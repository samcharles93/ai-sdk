package minimax

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/samcharles93/ai-sdk/music"
)

func TestGenerateMusic_Success(t *testing.T) {
	wantAudio := []byte("fake-track-bytes-5678")
	wantHex := hex.EncodeToString(wantAudio)

	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"base_resp":{"status_code":0,"status_msg":"success"},"data":{"audio":"`+wantHex+`","status":2}}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "test-key", BaseURL: srv.URL})

	resp, err := p.GenerateMusic(context.Background(), music.GenerateMusicRequest{
		Model:  "music-3.0",
		Prompt: "Pop, melancholic, perfect for a rainy night",
		Lyrics: "[Verse]\nhello\n[Chorus]\nworld",
		Format: "wav",
	})
	if err != nil {
		t.Fatalf("GenerateMusic: %v", err)
	}
	if string(resp.Audio) != string(wantAudio) {
		t.Errorf("audio = %q, want %q", resp.Audio, wantAudio)
	}
	if resp.Format != "wav" {
		t.Errorf("format = %q, want wav", resp.Format)
	}
	if gotBody["model"] != "music-3.0" {
		t.Errorf("model = %v, want music-3.0", gotBody["model"])
	}
	if gotBody["lyrics"] == nil {
		t.Error("lyrics missing from body")
	}
}

func TestGenerateMusic_MissingPrompt(t *testing.T) {
	p := newTestClient(t, &fakeAPI{t: t})
	_, err := p.GenerateMusic(context.Background(), music.GenerateMusicRequest{})
	if !errors.Is(err, music.ErrInvalidRequest) {
		t.Errorf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestGenerateMusic_BaseRespError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"base_resp":{"status_code":1002,"status_msg":"bad prompt"}}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.GenerateMusic(context.Background(), music.GenerateMusicRequest{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "bad prompt") {
		t.Errorf("expected 'bad prompt' error, got %v", err)
	}
}

func TestGenerateMusic_NoAudio(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"base_resp":{"status_code":0},"data":{"status":1}}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.GenerateMusic(context.Background(), music.GenerateMusicRequest{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "no audio") {
		t.Errorf("expected 'no audio' error, got %v", err)
	}
}

func TestGenerateMusic_MapsRequestFields(t *testing.T) {
	tests := []struct {
		name                string
		req                 music.GenerateMusicRequest
		wantPrompt          bool
		wantInstrumental    bool
		wantLyricsOptimizer bool
		wantFormat          string
		wantSampleRate      bool
	}{
		{
			name:                "instrumental with auto-lyrics and options",
			req:                 music.GenerateMusicRequest{Prompt: "p", Instrumental: true, LyricsOptimizer: true, Format: "wav", ProviderOptions: map[string]any{"minimax": map[string]any{"sample_rate": 44100}}},
			wantPrompt:          true,
			wantInstrumental:    true,
			wantLyricsOptimizer: true,
			wantFormat:          "wav",
			wantSampleRate:      true,
		},
		{
			name:           "defaults",
			req:            music.GenerateMusicRequest{Prompt: "p"},
			wantPrompt:     true,
			wantFormat:     "mp3",
			wantSampleRate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotBody map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, `{"base_resp":{"status_code":0},"data":{"audio":"6161","status":2}}`)
			}))
			defer srv.Close()
			p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})

			_, err := p.GenerateMusic(context.Background(), tt.req)
			if err != nil {
				t.Fatalf("GenerateMusic: %v", err)
			}
			if (gotBody["prompt"] != nil) != tt.wantPrompt {
				t.Errorf("prompt present = %v, want %v", gotBody["prompt"] != nil, tt.wantPrompt)
			}
			if (gotBody["is_instrumental"] != nil) != tt.wantInstrumental {
				t.Errorf("is_instrumental present = %v, want %v", gotBody["is_instrumental"] != nil, tt.wantInstrumental)
			}
			if (gotBody["lyrics_optimizer"] != nil) != tt.wantLyricsOptimizer {
				t.Errorf("lyrics_optimizer present = %v, want %v", gotBody["lyrics_optimizer"] != nil, tt.wantLyricsOptimizer)
			}
			audioSetting, _ := gotBody["audio_setting"].(map[string]any)
			if audioSetting["format"] != tt.wantFormat {
				t.Errorf("audio_setting.format = %v, want %q", audioSetting["format"], tt.wantFormat)
			}
			if (audioSetting["sample_rate"] != nil) != tt.wantSampleRate {
				t.Errorf("sample_rate present = %v, want %v", audioSetting["sample_rate"] != nil, tt.wantSampleRate)
			}
		})
	}
}

func TestGenerateMusic_InvalidSampleRate(t *testing.T) {
	p := newTestClient(t, &fakeAPI{t: t})
	_, err := p.GenerateMusic(context.Background(), music.GenerateMusicRequest{
		Prompt:          "p",
		ProviderOptions: map[string]any{"minimax": map[string]any{"sample_rate": 99999}},
	})
	if !errors.Is(err, music.ErrInvalidRequest) {
		t.Errorf("expected ErrInvalidRequest for invalid sample_rate, got %v", err)
	}
}

func TestGenerateMusic_PassesNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"type":"rate_limit","message":"too many requests"}}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.GenerateMusic(context.Background(), music.GenerateMusicRequest{Prompt: "x"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, music.ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
	if !strings.Contains(err.Error(), "too many requests") {
		t.Errorf("expected platform message, got %v", err)
	}
}
