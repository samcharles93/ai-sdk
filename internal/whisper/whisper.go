// Package whisper provides a provider-agnostic implementation of the
// Whisper-compatible /audio/transcriptions endpoint (multipart form request,
// response decoding, and segment mapping) shared by the provider packages
// that target it (currently openai and groq).
//
// It lives outside the domain layers so those packages stay stdlib-only, and
// is parameterised by the provider-specific bits (options key, error
// classification) so each provider becomes a thin wrapper rather than a
// near-copy of the same request/decode logic.
package whisper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/transcribe"
	"github.com/samcharles93/ai-sdk/util"
)

const endpoint = "/audio/transcriptions"

// --- wire types ----------------------------------------------------------

// wireTranscriptionResponse mirrors the JSON returned by a Whisper-compatible
// /audio/transcriptions endpoint. It is the union of the fields the sharing
// providers actually use; provider-specific extras (for example Groq's task /
// x_groq block) are ignored by the decoders and omitted here.
type wireTranscriptionResponse struct {
	Text     string                     `json:"text"`
	Language string                     `json:"language,omitempty"`
	Duration float64                    `json:"duration,omitempty"`
	Segments []wireTranscriptionSegment `json:"segments,omitempty"`
	Words    []wireTranscriptionWord    `json:"words,omitempty"`
}

type wireTranscriptionSegment struct {
	ID               int     `json:"id"`
	Seek             int     `json:"seek"`
	Start            float64 `json:"start"`
	End              float64 `json:"end"`
	Text             string  `json:"text"`
	Tokens           []int   `json:"tokens,omitempty"`
	Temperature      float64 `json:"temperature,omitempty"`
	AvgLogprob       float64 `json:"avg_logprob,omitempty"`
	CompressionRatio float64 `json:"compression_ratio,omitempty"`
	NoSpeechProb     float64 `json:"no_speech_prob,omitempty"`
}

type wireTranscriptionWord struct {
	Word  string  `json:"word"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// --- config --------------------------------------------------------------

// Config parameterises the shared Whisper-compatible transcription helper for
// a specific provider backend.
type Config struct {
	// Provider is the provider name. It is used as the ProviderOptions bucket
	// key and in every error message that would otherwise be provider-specific
	// (for example "openai" or "groq").
	Provider string
	// BaseURL is the API root; /audio/transcriptions is appended.
	BaseURL string
	// APIKey is the bearer token used in the Authorization header.
	APIKey string
	// HTTPClient is the client used to perform the request.
	HTTPClient *http.Client
	// IncludeFields, when true, reads the provider's include[] option (OpenAI's
	// additional fields) and writes them into the form. When false the option
	// is ignored, as Groq does.
	IncludeFields bool
	// ClassifyError, when non-nil, builds the error returned for a non-2xx
	// response. It receives the HTTP response (for headers such as
	// Retry-After / X-Request-Id) and the sanitised body snippet. When nil a
	// default classification is used (a plain fmt.Errorf wrapping
	// [ErrorForStatus]).
	ClassifyError func(resp *http.Response, snippet string) error
}

// Transcribe performs a Whisper-compatible audio transcription call. It
// builds the multipart request, executes it against cfg.BaseURL +
// /audio/transcriptions, classifies non-2xx responses, decodes the standard
// Whisper response, and maps its segments into transcribe.TranscribeResponse.
func Transcribe(ctx context.Context, cfg Config, req transcribe.TranscribeRequest) (transcribe.TranscribeResponse, error) {
	if req.Model == "" {
		return transcribe.TranscribeResponse{}, fmt.Errorf("%s: model is required: %w", cfg.Provider, transcribe.ErrInvalidRequest)
	}
	if len(req.Audio) == 0 {
		return transcribe.TranscribeResponse{}, fmt.Errorf("%s: audio data is required: %w", cfg.Provider, transcribe.ErrInvalidRequest)
	}

	body, contentType, err := buildForm(cfg, req)
	if err != nil {
		return transcribe.TranscribeResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+endpoint, body)
	if err != nil {
		return transcribe.TranscribeResponse{}, fmt.Errorf("%s: build transcription request: %w", cfg.Provider, err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	httpReq.Header.Set("Content-Type", contentType)

	resp, err := cfg.HTTPClient.Do(httpReq)
	if err != nil {
		return transcribe.TranscribeResponse{}, fmt.Errorf("%s: http do: %w", cfg.Provider, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		snippet := chat.SanitizeErrorBody(respBody)
		if cfg.ClassifyError != nil {
			return transcribe.TranscribeResponse{}, cfg.ClassifyError(resp, snippet)
		}
		return transcribe.TranscribeResponse{}, defaultError(cfg.Provider, resp.StatusCode, snippet)
	}

	var wr wireTranscriptionResponse
	if err := json.NewDecoder(resp.Body).Decode(&wr); err != nil {
		return transcribe.TranscribeResponse{}, fmt.Errorf("%s: decode transcription response: %w", cfg.Provider, err)
	}

	out := transcribe.TranscribeResponse{
		Text:     wr.Text,
		Language: wr.Language,
		Duration: wr.Duration,
	}

	// Map segments.
	if len(wr.Segments) > 0 {
		out.Segments = make([]transcribe.TranscriptionSegment, len(wr.Segments))
		for i, seg := range wr.Segments {
			out.Segments[i] = transcribe.TranscriptionSegment{
				ID:               seg.ID,
				Seek:             seg.Seek,
				Start:            seg.Start,
				End:              seg.End,
				Text:             seg.Text,
				Tokens:           seg.Tokens,
				Temperature:      seg.Temperature,
				AvgLogprob:       seg.AvgLogprob,
				CompressionRatio: seg.CompressionRatio,
				NoSpeechProb:     seg.NoSpeechProb,
			}
		}
	}

	return out, nil
}

// buildForm constructs the multipart form data for a transcription request.
// It writes the standard Whisper fields (model, language, prompt, temperature,
// response_format, timestamp_granularities[]) plus the provider-specific
// include[] fields when cfg.IncludeFields is set.
func buildForm(cfg Config, req transcribe.TranscribeRequest) (*bytes.Buffer, string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Model field.
	_ = writer.WriteField("model", req.Model)

	// Optional fields.
	if req.Language != "" {
		_ = writer.WriteField("language", req.Language)
	}
	if req.Prompt != "" {
		_ = writer.WriteField("prompt", req.Prompt)
	}
	if req.Temperature != 0 {
		_ = writer.WriteField("temperature", fmt.Sprintf("%f", req.Temperature))
	}

	// Parse provider options.
	var responseFormat string
	var timestampGranularities []string
	var includeFields []string

	if opts, ok := req.ProviderOptions[cfg.Provider].(map[string]any); ok {
		if v, ok := opts["response_format"].(string); ok && v != "" {
			responseFormat = v
		}
		if tg, ok := opts["timestamp_granularities"].([]any); ok && len(tg) > 0 {
			for _, g := range tg {
				if s, ok := g.(string); ok {
					timestampGranularities = append(timestampGranularities, s)
				}
			}
		}
		if cfg.IncludeFields {
			if inc, ok := opts["include"].([]any); ok && len(inc) > 0 {
				for _, i := range inc {
					if s, ok := i.(string); ok {
						includeFields = append(includeFields, s)
					}
				}
			}
		}
	}

	// Response format: prefer verbose_json for newer models.
	if responseFormat == "" {
		responseFormat = "verbose_json"
	}
	_ = writer.WriteField("response_format", responseFormat)

	// Timestamp granularities: default to segment.
	if len(timestampGranularities) == 0 {
		timestampGranularities = []string{"segment"}
	}
	for _, g := range timestampGranularities {
		_ = writer.WriteField("timestamp_granularities[]", g)
	}

	// Include fields if the provider opts in.
	for _, inc := range includeFields {
		_ = writer.WriteField("include[]", inc)
	}

	// Determine file extension and content type from audio data.
	ext := util.DetectAudioFormat(req.Audio)
	if ext == "" {
		ext = "mp3"
	}

	// File field with audio data.
	part, err := writer.CreateFormFile("file", "audio."+ext)
	if err != nil {
		return nil, "", fmt.Errorf("%s: create form file: %w", cfg.Provider, err)
	}
	if _, err := part.Write(req.Audio); err != nil {
		return nil, "", fmt.Errorf("%s: write audio data: %w", cfg.Provider, err)
	}

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("%s: close writer: %w", cfg.Provider, err)
	}

	return body, writer.FormDataContentType(), nil
}

// ErrorForStatus maps a Whisper transcription HTTP status code to the
// corresponding transcribe sentinel error. It is shared by every provider so
// the classification switch lives in one place rather than being duplicated.
func ErrorForStatus(code int) error {
	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		return transcribe.ErrAuthFailed
	case http.StatusTooManyRequests:
		return transcribe.ErrRateLimited
	default:
		return transcribe.ErrProviderUnavailable
	}
}

// defaultError builds the plain wrapped error used when Config.ClassifyError
// is nil. It reproduces the "<provider>: status %d: <snippet>: <sentinel>"
// shape some providers (openai) return for non-2xx responses.
func defaultError(provider string, code int, snippet string) error {
	return fmt.Errorf("%s: status %d: %s: %w", provider, code, snippet, ErrorForStatus(code))
}
