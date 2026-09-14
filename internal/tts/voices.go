package tts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/speech"
)

const voicesEndpoint = "/audio/voices"

// wireVoice is the object shape OpenAI-compatible servers return per voice.
// Unknown fields (Kokoro voice grades, wrapper keys) are ignored.
type wireVoice struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Language string `json:"language"`
	Gender   string `json:"gender"`
}

// ListVoices fetches the OpenAI-compatible /audio/voices listing from
// cfg.BaseURL, normalising both the bare-string form (legacy Kokoro) and the
// object form (Kokoro >= 0.3, speaches) into speech.Voice values. A 404 or 405
// from the backend is reported as speech.ErrVoiceListingNotSupported; other
// non-2xx responses use the configured error classification.
func ListVoices(ctx context.Context, cfg Config, model string) ([]speech.Voice, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BaseURL+voicesEndpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: build voices request: %w", cfg.Provider, err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	httpReq.Header.Set("Accept", "application/json")

	resp, err := cfg.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%s: http do: %w", cfg.Provider, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return nil, speech.ErrVoiceListingNotSupported
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		snippet := chat.SanitizeErrorBody(respBody)
		if cfg.ClassifyError != nil {
			return nil, cfg.ClassifyError(resp, snippet)
		}
		return nil, defaultError(cfg.Provider, resp.StatusCode, snippet)
	}

	var payload struct {
		Voices []json.RawMessage `json:"voices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("%s: decode voices response: %w", cfg.Provider, err)
	}

	voices := make([]speech.Voice, 0, len(payload.Voices))
	for _, raw := range payload.Voices {
		voice, err := decodeVoice(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: decode voice entry: %w", cfg.Provider, err)
		}
		if voice.Model == "" {
			voice.Model = model
		}
		voices = append(voices, voice)
	}
	return voices, nil
}

// decodeVoice accepts either a bare string (legacy Kokoro) or an object.
func decodeVoice(raw json.RawMessage) (speech.Voice, error) {
	var id string
	if err := json.Unmarshal(raw, &id); err == nil {
		return speech.Voice{ID: id, Name: id}, nil
	}
	var wv wireVoice
	if err := json.Unmarshal(raw, &wv); err != nil {
		return speech.Voice{}, err
	}
	if wv.ID == "" {
		wv.ID = wv.Name
	}
	if wv.Name == "" {
		wv.Name = wv.ID
	}
	return speech.Voice{ID: wv.ID, Name: wv.Name, Language: wv.Language, Gender: wv.Gender}, nil
}
