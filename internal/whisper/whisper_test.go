package whisper

import (
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/samcharles93/ai-sdk/transcribe"
)

// newTestServer returns an httptest server plus a capture func for the parsed
// multipart form values. The handler writes respBody with the given status.
func newTestServer(t *testing.T, status int, respBody string, capture func(url.Values, *multipart.FileHeader)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		if r.MultipartForm.File["file"] != nil {
			f := r.MultipartForm.File["file"][0]
			if capture != nil {
				capture(r.PostForm, f)
			}
		} else if capture != nil {
			capture(r.PostForm, nil)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestTranscribe_Success(t *testing.T) {
	resp := `{"text":"hello world","language":"en","duration":2.5,"segments":[{"id":0,"seek":0,"start":0.0,"end":1.5,"text":"hello","tokens":[1,2],"temperature":0.0,"avg_logprob":-0.2,"compression_ratio":1.4,"no_speech_prob":0.01}]}`
	srv := newTestServer(t, http.StatusOK, resp, nil)

	out, err := Transcribe(context.Background(), Config{
		Provider:   "openai",
		BaseURL:    srv.URL,
		APIKey:     "k",
		HTTPClient: srv.Client(),
	}, transcribe.TranscribeRequest{
		Model: "whisper-1",
		Audio: []byte("fake audio"),
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if out.Text != "hello world" || out.Language != "en" || out.Duration != 2.5 {
		t.Fatalf("unexpected response: %+v", out)
	}
	if len(out.Segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(out.Segments))
	}
	seg := out.Segments[0]
	if seg.ID != 0 || seg.Text != "hello" || seg.Start != 0 || seg.End != 1.5 || seg.Temperature != 0 || seg.AvgLogprob != -0.2 || seg.CompressionRatio != 1.4 || seg.NoSpeechProb != 0.01 {
		t.Fatalf("unexpected segment: %+v", seg)
	}
}

func TestTranscribe_DefaultError_RateLimited(t *testing.T) {
	srv := newTestServer(t, http.StatusTooManyRequests, `{"error":"slow down"}`, nil)

	_, err := Transcribe(context.Background(), Config{
		Provider:   "openai",
		BaseURL:    srv.URL,
		APIKey:     "k",
		HTTPClient: srv.Client(),
	}, transcribe.TranscribeRequest{Model: "whisper-1", Audio: []byte("a")})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, transcribe.ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
	if !strings.HasPrefix(err.Error(), "openai: status 429:") {
		t.Errorf("unexpected error string: %q", err.Error())
	}
}

func TestTranscribe_DefaultError_Forbidden(t *testing.T) {
	srv := newTestServer(t, http.StatusForbidden, `{"error":"denied"}`, nil)

	_, err := Transcribe(context.Background(), Config{
		Provider:   "openai",
		BaseURL:    srv.URL,
		APIKey:     "k",
		HTTPClient: srv.Client(),
	}, transcribe.TranscribeRequest{Model: "whisper-1", Audio: []byte("a")})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, transcribe.ErrAuthFailed) {
		t.Errorf("expected ErrAuthFailed, got %v", err)
	}
}

func TestTranscribe_DefaultError_Server(t *testing.T) {
	srv := newTestServer(t, http.StatusInternalServerError, `{"error":"boom"}`, nil)

	_, err := Transcribe(context.Background(), Config{
		Provider:   "openai",
		BaseURL:    srv.URL,
		APIKey:     "k",
		HTTPClient: srv.Client(),
	}, transcribe.TranscribeRequest{Model: "whisper-1", Audio: []byte("a")})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, transcribe.ErrProviderUnavailable) {
		t.Errorf("expected ErrProviderUnavailable, got %v", err)
	}
}

func TestTranscribe_ClassifyError_Override(t *testing.T) {
	srv := newTestServer(t, http.StatusTooManyRequests, `{"error":"slow down"}`, nil)

	gotCode := 0
	gotSnippet := ""
	cfg := Config{
		Provider:   "groq",
		BaseURL:    srv.URL,
		APIKey:     "k",
		HTTPClient: srv.Client(),
		ClassifyError: func(resp *http.Response, snippet string) error {
			gotCode = resp.StatusCode
			gotSnippet = snippet
			return errors.New("custom")
		},
	}
	_, err := Transcribe(context.Background(), cfg, transcribe.TranscribeRequest{Model: "whisper-1", Audio: []byte("a")})
	if err == nil || err.Error() != "custom" {
		t.Fatalf("expected custom error, got %v", err)
	}
	if gotCode != http.StatusTooManyRequests {
		t.Errorf("classify got code %d, want %d", gotCode, http.StatusTooManyRequests)
	}
	if !strings.Contains(gotSnippet, "slow down") {
		t.Errorf("snippet = %q, want to contain 'slow down'", gotSnippet)
	}
}

// TestTranscribe_FormFields verifies the multipart form is built with the
// expected fields, and that include[] is only written when IncludeFields.
func TestTranscribe_FormFields(t *testing.T) {
	cases := []struct {
		name          string
		includeFields bool
		wantInclude   bool
		wantTS        string
	}{
		{name: "with include", includeFields: true, wantInclude: true, wantTS: "segment"},
		{name: "without include", includeFields: false, wantInclude: false, wantTS: "segment"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotInclude []string
			var gotTS []string
			var gotModel, gotResponseFormat, gotFileName string

			srv := newTestServer(t, http.StatusOK, `{"text":"ok"}`, func(form url.Values, file *multipart.FileHeader) {
				gotInclude = form["include[]"]
				gotTS = form["timestamp_granularities[]"]
				gotModel = form.Get("model")
				gotResponseFormat = form.Get("response_format")
				if file != nil {
					gotFileName = file.Filename
				}
			})

			po := map[string]any{
				"openai": map[string]any{
					"response_format": "json",
					"include":         []any{"index", "logprob"},
				},
			}
			req := transcribe.TranscribeRequest{
				Model:           "whisper-1",
				Audio:           []byte("fake audio"),
				ProviderOptions: po,
			}

			_, err := Transcribe(context.Background(), Config{
				Provider:      "openai",
				BaseURL:       srv.URL,
				APIKey:        "k",
				HTTPClient:    srv.Client(),
				IncludeFields: tc.includeFields,
			}, req)
			if err != nil {
				t.Fatalf("Transcribe: %v", err)
			}

			if gotModel != "whisper-1" {
				t.Errorf("model = %q, want whisper-1", gotModel)
			}
			if gotResponseFormat != "json" {
				t.Errorf("response_format = %q, want json", gotResponseFormat)
			}
			if len(gotTS) == 0 || gotTS[0] != tc.wantTS {
				t.Errorf("timestamp_granularities[] = %v, want [%s]", gotTS, tc.wantTS)
			}
			if tc.wantInclude {
				if len(gotInclude) != 2 || gotInclude[0] != "index" || gotInclude[1] != "logprob" {
					t.Errorf("include[] = %v, want [index logprob]", gotInclude)
				}
			} else if len(gotInclude) != 0 {
				t.Errorf("include[] = %v, want none", gotInclude)
			}
			if !strings.HasPrefix(gotFileName, "audio.") {
				t.Errorf("file name = %q, want audio.<ext>", gotFileName)
			}
		})
	}
}

func TestTranscribe_MissingModel(t *testing.T) {
	_, err := Transcribe(context.Background(), Config{Provider: "openai"}, transcribe.TranscribeRequest{Audio: []byte("a")})
	if !errors.Is(err, transcribe.ErrInvalidRequest) {
		t.Errorf("expected ErrInvalidRequest, got %v", err)
	}
	if !strings.Contains(err.Error(), "model is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTranscribe_MissingAudio(t *testing.T) {
	_, err := Transcribe(context.Background(), Config{Provider: "openai"}, transcribe.TranscribeRequest{Model: "whisper-1"})
	if !errors.Is(err, transcribe.ErrInvalidRequest) {
		t.Errorf("expected ErrInvalidRequest, got %v", err)
	}
	if !strings.Contains(err.Error(), "audio data is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestErrorForStatus(t *testing.T) {
	cases := map[int]error{
		http.StatusUnauthorized:        transcribe.ErrAuthFailed,
		http.StatusForbidden:           transcribe.ErrAuthFailed,
		http.StatusTooManyRequests:     transcribe.ErrRateLimited,
		http.StatusInternalServerError: transcribe.ErrProviderUnavailable,
		http.StatusBadRequest:          transcribe.ErrProviderUnavailable,
	}
	for code, want := range cases {
		if got := ErrorForStatus(code); !errors.Is(got, want) {
			t.Errorf("ErrorForStatus(%d) = %v, want %v", code, got, want)
		}
	}
}
