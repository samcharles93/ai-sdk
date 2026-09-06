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

	"github.com/samcharles93/ai-sdk/speech"
)

func TestGenerateSpeech_Success(t *testing.T) {
	wantAudio := []byte("fake-mp3-bytes-1234")
	wantHex := hex.EncodeToString(wantAudio)

	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"data":{"audio":"`+wantHex+`","status":2},"trace_id":"t1"}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "test-key", BaseURL: srv.URL})

	resp, err := p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{
		Model: "speech-2.8-hd",
		Text:  "hello world",
		Voice: "voice-a",
		Speed: 1.2,
	})
	if err != nil {
		t.Fatalf("GenerateSpeech: %v", err)
	}
	if string(resp.Audio) != string(wantAudio) {
		t.Errorf("audio = %q, want %q", resp.Audio, wantAudio)
	}
	if resp.Format != "mp3" {
		t.Errorf("format = %q, want mp3", resp.Format)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("auth = %q, want Bearer test-key", gotAuth)
	}
	vs, _ := gotBody["voice_setting"].(map[string]any)
	if vs["voice_id"] != "voice-a" || vs["speed"] != 1.2 {
		t.Errorf("voice_setting = %v, want voice_id voice-a and speed 1.2", vs)
	}
}

func TestGenerateSpeech_MissingText(t *testing.T) {
	p := newTestClient(t, &fakeAPI{t: t})
	_, err := p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{})
	if !errors.Is(err, speech.ErrInvalidRequest) {
		t.Errorf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestGenerateSpeech_NoAudio(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"data":{"status":2}}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{Text: "hi"})
	if err == nil || !strings.Contains(err.Error(), "no audio") {
		t.Errorf("expected 'no audio' error, got %v", err)
	}
}

func TestGenerateSpeech_PassesNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"type":"rate_limit","message":"too many requests"}}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{Text: "hi"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, speech.ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
	if !strings.Contains(err.Error(), "too many requests") {
		t.Errorf("expected platform message, got %v", err)
	}
}
