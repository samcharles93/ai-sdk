package tts

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

func TestStream_SSE(t *testing.T) {
	want1 := []byte("first-chunk")
	want2 := []byte("second-chunk")
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range [][]byte{want1, want2} {
			_, _ = fmt.Fprintf(w, "data: {\"type\":\"speech.audio.delta\",\"audio\":%q}\n\n", base64.StdEncoding.EncodeToString(chunk))
		}
		_, _ = io.WriteString(w, "data: {\"type\":\"speech.audio.done\",\"usage\":{\"input_tokens\":1,\"output_tokens\":2,\"total_tokens\":3}}\n\n")
	}))
	defer srv.Close()

	stream, format, err := Stream(context.Background(), testConfig(srv), speechRequest(), StreamFormatSSE)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer stream.Close()

	if format != "mp3" {
		t.Fatalf("format = %q, want mp3", format)
	}
	if gotBody["stream_format"] != "sse" {
		t.Fatalf("stream_format = %v, want sse", gotBody["stream_format"])
	}

	got1, err := stream.Next(context.Background())
	if err != nil {
		t.Fatalf("Next(1): %v", err)
	}
	if string(got1.Data) != string(want1) || got1.Done {
		t.Fatalf("chunk 1 = %+v, want data %q", got1, want1)
	}
	got2, err := stream.Next(context.Background())
	if err != nil {
		t.Fatalf("Next(2): %v", err)
	}
	if string(got2.Data) != string(want2) {
		t.Fatalf("chunk 2 = %+v, want data %q", got2, want2)
	}
	done, err := stream.Next(context.Background())
	if err != nil {
		t.Fatalf("Next(3): %v", err)
	}
	if !done.Done {
		t.Fatalf("chunk 3 = %+v, want Done", done)
	}
	if done.Usage == nil || done.Usage.TotalTokens != 3 {
		t.Fatalf("usage = %+v, want total_tokens 3", done.Usage)
	}
	if _, err := stream.Next(context.Background()); err != io.EOF {
		t.Fatalf("Next after Done = %v, want io.EOF", err)
	}
}

func TestStream_RawAudio(t *testing.T) {
	const body = "raw-audio-bytes"
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	stream, format, err := Stream(context.Background(), testConfig(srv), speechRequest(), StreamFormatAudio)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer stream.Close()

	if format != "mp3" {
		t.Fatalf("format = %q, want mp3", format)
	}
	if gotBody["stream_format"] != "audio" {
		t.Fatalf("stream_format = %v, want audio", gotBody["stream_format"])
	}

	var got []byte
	for {
		chunk, err := stream.Next(context.Background())
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if chunk.Done {
			break
		}
		got = append(got, chunk.Data...)
	}
	if string(got) != body {
		t.Fatalf("audio = %q, want %q", got, body)
	}
}

func TestStream_EstablishmentErrors(t *testing.T) {
	t.Run("non-2xx is classified", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer srv.Close()

		_, _, err := Stream(context.Background(), testConfig(srv), speechRequest(), StreamFormatSSE)
		if !errors.Is(err, speech.ErrRateLimited) {
			t.Fatalf("error = %v, want ErrRateLimited", err)
		}
	})

	t.Run("validation errors surface before the request", func(t *testing.T) {
		srv := newSpeechServer(t, http.StatusOK, "", nil)
		_, _, err := Stream(context.Background(), testConfig(srv), speech.GenerateSpeechRequest{}, StreamFormatSSE)
		if !errors.Is(err, speech.ErrInvalidRequest) {
			t.Fatalf("error = %v, want ErrInvalidRequest", err)
		}
	})
}

func TestStream_MalformedEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "data: not-json\n\n")
	}))
	defer srv.Close()

	stream, _, err := Stream(context.Background(), testConfig(srv), speechRequest(), StreamFormatSSE)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer stream.Close()

	if _, err := stream.Next(context.Background()); err == nil {
		t.Fatal("expected a decode error from Next")
	}
}

func speechRequest() speech.GenerateSpeechRequest {
	return speech.GenerateSpeechRequest{Model: "m", Text: "hi"}
}
