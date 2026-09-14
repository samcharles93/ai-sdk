package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samcharles93/ai-sdk/speech"
)

func TestStreamSpeech_SSE(t *testing.T) {
	want := []byte("streamed-audio")
	var gotBody map[string]any
	var gotAccept string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"speech.audio.delta\",\"audio\":%q}\n\n", base64.StdEncoding.EncodeToString(want))
		_, _ = io.WriteString(w, "data: {\"type\":\"speech.audio.done\"}\n\n")
	}))
	defer srv.Close()

	p, err := New(Config{APIKey: "k", BaseURL: srv.URL, Streaming: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	stream, err := p.StreamSpeech(context.Background(), speech.GenerateSpeechRequest{
		Model: "gpt-4o-mini-tts",
		Text:  "hi",
	})
	if err != nil {
		t.Fatalf("StreamSpeech: %v", err)
	}
	defer stream.Close()

	if gotBody["stream_format"] != "sse" {
		t.Fatalf("stream_format = %v, want sse", gotBody["stream_format"])
	}
	if gotAccept != "text/event-stream" {
		t.Fatalf("Accept = %q, want text/event-stream", gotAccept)
	}

	chunk, err := stream.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if string(chunk.Data) != string(want) || chunk.Format != "mp3" {
		t.Fatalf("chunk = %+v, want data %q in mp3", chunk, want)
	}

	done, err := stream.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if !done.Done {
		t.Fatalf("chunk = %+v, want Done", done)
	}
	if _, err := stream.Next(context.Background()); err != io.EOF {
		t.Fatalf("Next after Done = %v, want io.EOF", err)
	}
}

func TestStreamSpeech_ModelFormatSelection(t *testing.T) {
	tests := []struct {
		model      string
		wantMode   string
		wantAccept string
	}{
		{model: "gpt-4o-mini-tts", wantMode: "sse", wantAccept: "text/event-stream"},
		{model: "tts-1", wantMode: "audio", wantAccept: "audio/*"},
		{model: "tts-1-hd", wantMode: "audio", wantAccept: "audio/*"},
		{model: "tts-1-hd-1106", wantMode: "audio", wantAccept: "audio/*"},
	}
	for _, tc := range tests {
		t.Run(tc.model, func(t *testing.T) {
			var gotBody map[string]any
			var gotAccept string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAccept = r.Header.Get("Accept")
				if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
					t.Fatalf("decode request body: %v", err)
				}
				_, _ = io.WriteString(w, "audio")
			}))
			defer srv.Close()

			p, err := New(Config{APIKey: "k", BaseURL: srv.URL, Streaming: true})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			stream, err := p.StreamSpeech(context.Background(), speech.GenerateSpeechRequest{Model: tc.model, Text: "hi"})
			if err != nil {
				t.Fatalf("StreamSpeech: %v", err)
			}
			defer stream.Close()

			if gotBody["stream_format"] != tc.wantMode {
				t.Fatalf("stream_format = %v, want %s", gotBody["stream_format"], tc.wantMode)
			}
			if gotAccept != tc.wantAccept {
				t.Fatalf("Accept = %q, want %q", gotAccept, tc.wantAccept)
			}
		})
	}
}

func TestStreamSpeech_NotEnabled(t *testing.T) {
	p, err := New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = p.StreamSpeech(context.Background(), speech.GenerateSpeechRequest{Model: "gpt-4o-mini-tts", Text: "hi"})
	if !errors.Is(err, speech.ErrStreamNotSupported) {
		t.Fatalf("error = %v, want ErrStreamNotSupported", err)
	}
}

func TestStreamSpeech_EstablishmentError(t *testing.T) {
	srv := newSpeechServer(t, http.StatusTooManyRequests, `{"error":"slow down"}`, nil)
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL, Streaming: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = p.StreamSpeech(context.Background(), speech.GenerateSpeechRequest{Model: "gpt-4o-mini-tts", Text: "hi"})
	if !errors.Is(err, speech.ErrRateLimited) {
		t.Fatalf("error = %v, want ErrRateLimited", err)
	}
}
