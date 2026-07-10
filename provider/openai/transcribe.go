// Package openai provides access to OpenAI's Whisper transcription API
// via the transcribe.Provider interface.
package openai

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

const (
	transcriptionAPIPath = "/audio/transcriptions"
)

// --- wire types ----------------------------------------------------------

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

// --- Transcription (Whisper) -----------------------------------------------

// Transcribe converts audio to text using OpenAI's Whisper API.
// It satisfies transcribe.Provider.
func (p *Provider) Transcribe(ctx context.Context, req transcribe.TranscribeRequest) (transcribe.TranscribeResponse, error) {
	if req.Model == "" {
		return transcribe.TranscribeResponse{}, fmt.Errorf("openai: model is required: %w", transcribe.ErrInvalidRequest)
	}
	if len(req.Audio) == 0 {
		return transcribe.TranscribeResponse{}, fmt.Errorf("openai: audio data is required: %w", transcribe.ErrInvalidRequest)
	}

	// Build multipart form data.
	body, contentType, err := buildTranscriptionForm(req)
	if err != nil {
		return transcribe.TranscribeResponse{}, fmt.Errorf("openai: build form: %w", err)
	}

	url := p.baseURL + transcriptionAPIPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return transcribe.TranscribeResponse{}, fmt.Errorf("openai: build transcription request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", contentType)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return transcribe.TranscribeResponse{}, fmt.Errorf("openai: http do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		snippet := chat.SanitizeErrorBody(respBody)
		return transcribe.TranscribeResponse{}, classifyTranscriptionHTTPError(resp.StatusCode, snippet)
	}

	var wr wireTranscriptionResponse
	if err := json.NewDecoder(resp.Body).Decode(&wr); err != nil {
		return transcribe.TranscribeResponse{}, fmt.Errorf("openai: decode transcription response: %w", err)
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

// buildTranscriptionForm constructs the multipart form data for a
// transcription request.
func buildTranscriptionForm(req transcribe.TranscribeRequest) (*bytes.Buffer, string, error) {
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

	// Parse provider options for OpenAI-specific settings.
	var responseFormat string
	var includeFields []string

	if opts, ok := req.ProviderOptions["openai"].(map[string]any); ok {
		if v, ok := opts["response_format"].(string); ok && v != "" {
			responseFormat = v
		}
		if inc, ok := opts["include"].([]any); ok && len(inc) > 0 {
			for _, i := range inc {
				if s, ok := i.(string); ok {
					includeFields = append(includeFields, s)
				}
			}
		}
	}

	// Response format: prefer verbose_json for newer models.
	if responseFormat == "" {
		responseFormat = "verbose_json"
	}
	_ = writer.WriteField("response_format", responseFormat)

	// Timestamp granularities.
	_ = writer.WriteField("timestamp_granularities[]", "segment")

	// Include fields if specified.
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
		return nil, "", fmt.Errorf("openai: create form file: %w", err)
	}
	if _, err := part.Write(req.Audio); err != nil {
		return nil, "", fmt.Errorf("openai: write audio data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("openai: close writer: %w", err)
	}

	return body, writer.FormDataContentType(), nil
}

func classifyTranscriptionHTTPError(code int, body string) error {
	var base error
	switch {
	case code == 401 || code == 403:
		base = transcribe.ErrAuthFailed
	case code == 429:
		base = transcribe.ErrRateLimited
	case code >= 500:
		base = transcribe.ErrProviderUnavailable
	default:
		base = transcribe.ErrProviderUnavailable
	}
	return fmt.Errorf("openai: status %d: %s: %w", code, body, base)
}

// Compile-time assertion that *Provider satisfies transcribe.Provider.
var _ transcribe.Provider = (*Provider)(nil)
