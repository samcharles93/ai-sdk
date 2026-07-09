// Package openai provides access to OpenAI's audio generation (TTS) API
// via the speech.Provider interface.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/samcharles93/ai-sdk/speech"
)

const (
	speechAPIPath = "/audio/speech"
)

// Model IDs supported by OpenAI's TTS API.
const (
	SpeechModelTTS1        = "tts-1"
	SpeechModelTTS1HD      = "tts-1-hd"
	SpeechModelTTS11106    = "tts-1-1106"
	SpeechModelTTS1HD1106  = "tts-1-hd-1106"
	SpeechModelGPT4MiniTTS = "gpt-4o-mini-tts"
)

// --- wire types ----------------------------------------------------------

type wireSpeechRequest struct {
	Model          string  `json:"model"`
	Input          string  `json:"input"`
	Voice          string  `json:"voice"`
	ResponseFormat string  `json:"response_format,omitempty"`
	Speed          float64 `json:"speed,omitempty"`
	Instructions   string  `json:"instructions,omitempty"`
}

// Valid output formats for the speech API.
var validSpeechFormats = map[string]bool{
	"mp3":  true,
	"opus": true,
	"aac":  true,
	"flac": true,
	"wav":  true,
	"pcm":  true,
}

// --- Speech (TTS) --------------------------------------------------------

// GenerateSpeech generates speech audio from the given text using OpenAI's
// TTS API. It satisfies speech.Provider.
func (p *Provider) GenerateSpeech(ctx context.Context, req speech.GenerateSpeechRequest) (speech.GenerateSpeechResponse, error) {
	if req.Model == "" {
		return speech.GenerateSpeechResponse{}, fmt.Errorf("openai: model is required: %w", speech.ErrInvalidRequest)
	}
	if req.Text == "" {
		return speech.GenerateSpeechResponse{}, fmt.Errorf("openai: text is required: %w", speech.ErrInvalidRequest)
	}

	voice := req.Voice
	if voice == "" {
		voice = "alloy"
	}

	format := req.Format
	if format == "" {
		format = "mp3"
	}
	if !validSpeechFormats[format] {
		return speech.GenerateSpeechResponse{}, fmt.Errorf("openai: invalid output format %q: %w", format, speech.ErrInvalidRequest)
	}

	body := wireSpeechRequest{
		Model:          req.Model,
		Input:          req.Text,
		Voice:          voice,
		ResponseFormat: format,
		Speed:          req.Speed,
	}

	// Parse provider-specific options for OpenAI.
	if opts, ok := req.ProviderOptions["openai"].(map[string]any); ok {
		if v, ok := opts["instructions"].(string); ok && v != "" {
			body.Instructions = v
		}
		if v, ok := opts["speed"].(float64); ok && v != 0 {
			// Only override if explicitly set and not already from req.
			if req.Speed == 0 {
				body.Speed = v
			}
		}
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return speech.GenerateSpeechResponse{}, fmt.Errorf("openai: marshal speech request: %w", err)
	}

	url := p.baseURL + speechAPIPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return speech.GenerateSpeechResponse{}, fmt.Errorf("openai: build speech request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "audio/*")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return speech.GenerateSpeechResponse{}, fmt.Errorf("openai: http do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		snippet := strings.TrimSpace(string(body))
		return speech.GenerateSpeechResponse{}, classifySpeechHTTPError(resp.StatusCode, snippet)
	}

	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		return speech.GenerateSpeechResponse{}, fmt.Errorf("openai: read audio response: %w", err)
	}

	return speech.GenerateSpeechResponse{
		Audio:  audio,
		Format: format,
	}, nil
}

func classifySpeechHTTPError(code int, body string) error {
	var base error
	switch {
	case code == 401 || code == 403:
		base = speech.ErrAuthFailed
	case code == 429:
		base = speech.ErrRateLimited
	case code >= 500:
		base = speech.ErrProviderUnavailable
	default:
		base = speech.ErrProviderUnavailable
	}
	return fmt.Errorf("openai: status %d: %s: %w", code, body, base)
}

// Compile-time assertion that *Provider satisfies speech.Provider.
var _ speech.Provider = (*Provider)(nil)
