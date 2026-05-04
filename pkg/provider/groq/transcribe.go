// Package groq provides access to Groq's Whisper transcription API
// via the transcribe.Provider interface.
package groq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/samcharles93/ai-sdk/pkg/transcribe"
	"github.com/samcharles93/ai-sdk/pkg/util"
)

const (
	transcriptionAPIPath = "/audio/transcriptions"
)

// --- wire types ----------------------------------------------------------

type wireTranscriptionResponse struct {
	Text     string                     `json:"text"`
	Language string                     `json:"language,omitempty"`
	Duration float64                    `json:"duration,omitempty"`
	Task     string                     `json:"task,omitempty"`
	XGroq    *wireXGroq                 `json:"x_groq,omitempty"`
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

type wireXGroq struct {
	ID string `json:"id"`
}

// --- Transcription (Whisper) -----------------------------------------------

// Transcribe converts audio to text using Groq's Whisper API.
// It satisfies transcribe.Provider.
func (p *Provider) Transcribe(ctx context.Context, req transcribe.TranscribeRequest) (transcribe.TranscribeResponse, error) {
	if req.Model == "" {
		return transcribe.TranscribeResponse{}, fmt.Errorf("groq: model is required: %w", transcribe.ErrInvalidRequest)
	}
	if len(req.Audio) == 0 {
		return transcribe.TranscribeResponse{}, fmt.Errorf("groq: audio data is required: %w", transcribe.ErrInvalidRequest)
	}

	// Build multipart form data.
	body, contentType, err := buildTranscriptionForm(req)
	if err != nil {
		return transcribe.TranscribeResponse{}, fmt.Errorf("groq: build form: %w", err)
	}

	url := p.baseURL + transcriptionAPIPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return transcribe.TranscribeResponse{}, fmt.Errorf("groq: build transcription request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", contentType)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return transcribe.TranscribeResponse{}, fmt.Errorf("groq: http do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		snippet := strings.TrimSpace(string(respBody))
		return transcribe.TranscribeResponse{}, classifyTranscriptionHTTPError(resp.StatusCode, snippet)
	}

	var wr wireTranscriptionResponse
	if err := json.NewDecoder(resp.Body).Decode(&wr); err != nil {
		return transcribe.TranscribeResponse{}, fmt.Errorf("groq: decode transcription response: %w", err)
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

	// Parse provider options.
	var responseFormat string
	var timestampGranularities []string

	if opts, ok := req.ProviderOptions["groq"].(map[string]any); ok {
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
	}

	// Response format: default to verbose_json.
	if responseFormat == "" {
		responseFormat = "verbose_json"
	}
	_ = writer.WriteField("response_format", responseFormat)

	// Timestamp granularities.
	if len(timestampGranularities) == 0 {
		timestampGranularities = []string{"segment"}
	}
	for _, g := range timestampGranularities {
		_ = writer.WriteField("timestamp_granularities[]", g)
	}

	// Detect audio format for file extension.
	ext := util.DetectAudioFormat(req.Audio)
	if ext == "" {
		ext = "mp3"
	}

	// File field with audio data.
	part, err := writer.CreateFormFile("file", "audio."+ext)
	if err != nil {
		return nil, "", fmt.Errorf("groq: create form file: %w", err)
	}
	if _, err := part.Write(req.Audio); err != nil {
		return nil, "", fmt.Errorf("groq: write audio data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("groq: close writer: %w", err)
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
	return fmt.Errorf("groq: status %d: %s: %w", code, body, base)
}

// detectAudioFormat attempts to detect the audio format from magic bytes.
func detectAudioFormat(data []byte) string {
	if len(data) < 12 {
		return ""
	}
	// MP3: ID3 tag or sync word
	if len(data) >= 3 && data[0] == 'I' && data[1] == 'D' && data[2] == '3' {
		return "mp3"
	}
	if data[0] == 0xFF && (data[1]&0xE0) == 0xE0 {
		return "mp3"
	}
	// WAV: RIFF header
	if len(data) >= 4 && string(data[:4]) == "RIFF" {
		return "wav"
	}
	// OGG: OggS header
	if len(data) >= 4 && string(data[:4]) == "OggS" {
		return "ogg"
	}
	// WebM
	if len(data) >= 4 && data[0] == 0x1a && data[1] == 0x45 && data[2] == 0xdf && data[3] == 0xa3 {
		return "webm"
	}
	// FLAC
	if len(data) >= 4 && string(data[:4]) == "fLaC" {
		return "flac"
	}
	return ""
}

// Compile-time assertion that *Provider satisfies transcribe.Provider.
var _ transcribe.Provider = (*Provider)(nil)
