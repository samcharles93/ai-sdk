package tts

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samcharles93/ai-sdk/speech"
)

func voicesConfig(srv *httptest.Server) Config {
	return Config{
		Provider:   "testprov",
		BaseURL:    srv.URL,
		APIKey:     "test-key",
		HTTPClient: srv.Client(),
	}
}

func TestListVoices_NormalisesStringAndObjectForms(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/voices" {
			t.Errorf("path = %q, want /audio/voices", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("auth = %q, want Bearer test-key", r.Header.Get("Authorization"))
		}
		_, _ = io.WriteString(w, `{"voices":[
			{"id":"af_heart","name":"Heart","language":"en-us","gender":"female"},
			"am_michael",
			{"name":"named-only"}
		]}`)
	}))
	defer srv.Close()

	voices, err := ListVoices(context.Background(), voicesConfig(srv), "kokoro")
	if err != nil {
		t.Fatalf("ListVoices: %v", err)
	}
	if len(voices) != 3 {
		t.Fatalf("len(voices) = %d, want 3", len(voices))
	}
	if voices[0].ID != "af_heart" || voices[0].Name != "Heart" || voices[0].Language != "en-us" || voices[0].Gender != "female" {
		t.Errorf("object voice = %+v, want populated fields", voices[0])
	}
	if voices[1].ID != "am_michael" || voices[1].Name != "am_michael" {
		t.Errorf("string voice = %+v, want id and name am_michael", voices[1])
	}
	if voices[2].ID != "named-only" {
		t.Errorf("name-only voice = %+v, want id named-only", voices[2])
	}
	for _, v := range voices {
		if v.Model != "kokoro" {
			t.Errorf("voice %q model = %q, want kokoro", v.ID, v.Model)
		}
	}
}

func TestListVoices_UnsupportedEndpoint(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusMethodNotAllowed} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()

			_, err := ListVoices(context.Background(), voicesConfig(srv), "")
			if !errors.Is(err, speech.ErrVoiceListingNotSupported) {
				t.Fatalf("error = %v, want ErrVoiceListingNotSupported", err)
			}
		})
	}
}

func TestListVoices_ErrorClassification(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":"slow down"}`)
	}))
	defer srv.Close()

	_, err := ListVoices(context.Background(), voicesConfig(srv), "")
	if !errors.Is(err, speech.ErrRateLimited) {
		t.Fatalf("error = %v, want ErrRateLimited", err)
	}
}
