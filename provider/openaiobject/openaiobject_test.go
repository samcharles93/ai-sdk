package openaiobject

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	errx "github.com/samcharles93/ai-sdk/error"
	"github.com/samcharles93/ai-sdk/object"
)

func TestGenerateObject_Success(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"chatcmpl-obj-1",
			"model":"gpt-4o",
			"choices":[{"index":0,"message":{"role":"assistant","content":"{\"city\":\"Tokyo\",\"temp\":21}"},"finish_reason":"stop"}]
		}`)
	}))
	defer srv.Close()

	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.GenerateObject(context.Background(), object.Request{Model: "gpt-4o", Prompt: "Weather in Tokyo?"})
	if err != nil {
		t.Fatalf("GenerateObject: %v", err)
	}

	obj, ok := res.(object.Object)
	if !ok {
		t.Fatalf("result = %T (%#v), want object.Object", res, res)
	}
	want := `{"city":"Tokyo","temp":21}`
	if obj.Content != want {
		t.Errorf("Content = %q, want %q", obj.Content, want)
	}
	if obj.Name != "object" {
		t.Errorf("Name = %q, want object", obj.Name)
	}
	if !strings.HasSuffix(gotPath, "/chat/completions") {
		t.Errorf("path = %q, want suffix /chat/completions", gotPath)
	}
	if gotBody["model"] != "gpt-4o" || gotBody["stream"] != false {
		t.Errorf("body = %#v", gotBody)
	}
	if gotBody["response_format"] != nil {
		rf := gotBody["response_format"].(map[string]any)
		if rf["type"] != "json_object" {
			t.Errorf("response_format.type = %v, want json_object (no schema provided)", rf["type"])
		}
	}
}

func TestGenerateObject_SchemaEnforcement(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`)

	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"chatcmpl-obj-2",
			"model":"gpt-4o",
			"choices":[{"index":0,"message":{"role":"assistant","content":"{\"city\":\"Tokyo\"}"},"finish_reason":"stop"}]
		}`)
	}))
	defer srv.Close()

	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})

	// json_schema (strict) when a schema is present.
	if _, err := p.GenerateObject(context.Background(), object.Request{
		Model: "gpt-4o", Prompt: "Give a city", Schema: schema,
	}); err != nil {
		t.Fatalf("GenerateObject with schema: %v", err)
	}
	rf, ok := gotBody["response_format"].(map[string]any)
	if !ok {
		t.Fatalf("response_format = %#v", gotBody["response_format"])
	}
	if rf["type"] != "json_schema" {
		t.Errorf("response_format.type = %v, want json_schema", rf["type"])
	}
	js, ok := rf["json_schema"].(map[string]any)
	if !ok {
		t.Fatalf("json_schema = %#v", rf["json_schema"])
	}
	if js["name"] != "object" || js["strict"] != true {
		t.Errorf("json_schema name/strict = %v/%v, want object/true", js["name"], js["strict"])
	}
	schBytes, _ := json.Marshal(js["schema"])
	var parsed map[string]any
	if err := json.Unmarshal(schBytes, &parsed); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	if parsed["type"] != "object" {
		t.Errorf("schema.type = %v, want object", parsed["type"])
	}

	// json_object fallback when no schema is provided.
	if _, err := p.GenerateObject(context.Background(), object.Request{
		Model: "gpt-4o", Prompt: "Give a city",
	}); err != nil {
		t.Fatalf("GenerateObject without schema: %v", err)
	}
	rf2, _ := gotBody["response_format"].(map[string]any)
	if rf2["type"] != "json_object" {
		t.Errorf("response_format.type (fallback) = %v, want json_object", rf2["type"])
	}
	if _, hasJSONSchema := rf2["json_schema"]; hasJSONSchema {
		t.Errorf("fallback should not include json_schema: %#v", rf2)
	}
}

func TestGenerateObject_ErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantTarget error
		wantRetry  bool
	}{
		{name: "auth", status: http.StatusUnauthorized, body: `{"error":"invalid api key"}`, wantTarget: object.ErrAuthFailed, wantRetry: false},
		{name: "rate limit", status: http.StatusTooManyRequests, body: `{"error":"slow down"}`, wantTarget: object.ErrRateLimited, wantRetry: true},
		{name: "bad request", status: http.StatusBadRequest, body: `{"error":"bad schema"}`, wantTarget: object.ErrInvalidRequest, wantRetry: false},
		{name: "server", status: http.StatusInternalServerError, body: `{"error":"boom"}`, wantTarget: object.ErrProviderUnavailable, wantRetry: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()

			p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
			_, err := p.GenerateObject(context.Background(), object.Request{Model: "gpt-4o", Prompt: "x"})
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, tt.wantTarget) {
				t.Errorf("errors.Is(%v, %v) = false, want true", err, tt.wantTarget)
			}
			var perr *errx.ProviderError
			if !errors.As(err, &perr) {
				t.Fatalf("errors.As to *errx.ProviderError failed: %T", err)
			}
			if perr.Provider != "openaiobject" {
				t.Errorf("Provider = %q, want openaiobject", perr.Provider)
			}
			if perr.StatusCode != tt.status {
				t.Errorf("StatusCode = %d, want %d", perr.StatusCode, tt.status)
			}
			if perr.Retryable != tt.wantRetry {
				t.Errorf("Retryable = %v, want %v", perr.Retryable, tt.wantRetry)
			}
		})
	}
}

func TestGenerateObject_EmptyModel(t *testing.T) {
	p, _ := New(Config{APIKey: "k", BaseURL: "http://example.invalid"})
	_, err := p.GenerateObject(context.Background(), object.Request{Prompt: "x"})
	if !errors.Is(err, object.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestGenerateObject_NoChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"x","model":"gpt-4o","choices":[]}`)
	}))
	defer srv.Close()

	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.GenerateObject(context.Background(), object.Request{Model: "gpt-4o", Prompt: "x"})
	if !errors.Is(err, object.ErrProviderUnavailable) {
		t.Fatalf("expected ErrProviderUnavailable, got %v", err)
	}
}

func TestStreamObject_ConcatAndClose(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		chunks := []string{
			sseLine(t, `{"city":`, ""),
			sseLine(t, `"Tokyo"`, ""),
			sseLine(t, `,"temp":21}`, ""),
			sseLine(t, "", "stop"),
			"data: [DONE]" + "\n\n",
		}
		for _, s := range chunks {
			_, _ = io.WriteString(w, s)
			if fl != nil {
				fl.Flush()
			}
		}
	}))
	defer srv.Close()

	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	st, err := p.StreamObject(context.Background(), object.Request{Model: "gpt-4o", Prompt: "Weather?"})
	if err != nil {
		t.Fatalf("StreamObject: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	var sb strings.Builder
	var doneChunk *object.ObjectChunk
	for {
		c, err := st.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if c.Done {
			cc := c
			doneChunk = &cc
			// After a Done chunk, the next Next must return io.EOF.
			if _, err := st.Next(ctx); !errors.Is(err, io.EOF) {
				t.Fatalf("expected EOF after Done, got %v", err)
			}
			break
		}
		sb.WriteString(c.Delta)
	}

	want := `{"city":"Tokyo","temp":21}`
	if sb.String() != want {
		t.Errorf("concat = %q, want %q", sb.String(), want)
	}
	if doneChunk == nil {
		t.Fatal("never saw a Done=true chunk")
	}
	if err := st.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestStreamObject_DoneEmittedOnEOFWithoutFinishReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		chunks := []string{
			sseLine(t, `{"a":1}`, ""),
			"data: [DONE]" + "\n\n",
		}
		for _, s := range chunks {
			_, _ = io.WriteString(w, s)
			if fl != nil {
				fl.Flush()
			}
		}
	}))
	defer srv.Close()

	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	st, err := p.StreamObject(context.Background(), object.Request{Model: "gpt-4o", Prompt: "x"})
	if err != nil {
		t.Fatalf("StreamObject: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	first, err := st.Next(ctx)
	if err != nil {
		t.Fatalf("first Next: %v", err)
	}
	if first.Delta != `{"a":1}` || first.Done {
		t.Fatalf("first chunk = %#v", first)
	}
	done, err := st.Next(ctx)
	if err != nil {
		t.Fatalf("second Next: %v", err)
	}
	if !done.Done {
		t.Fatalf("expected Done chunk, got %#v", done)
	}
	if _, err := st.Next(ctx); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF after Done, got %v", err)
	}
}

func TestStreamObject_RejectsInvalidRequest(t *testing.T) {
	p, _ := New(Config{APIKey: "k", BaseURL: "http://example.invalid"})
	_, err := p.StreamObject(context.Background(), object.Request{Prompt: "x"})
	if !errors.Is(err, object.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

// testChunk mirrors the OpenAI Chat Completions stream chunk shape so
// tests can build SSE lines whose content deltas are correctly JSON-escaped.
type testChunk struct {
	ID      string            `json:"id"`
	Model   string            `json:"model"`
	Choices []testChunkChoice `json:"choices"`
}

type testChunkChoice struct {
	Index        int            `json:"index"`
	Delta        testChunkDelta `json:"delta"`
	FinishReason string         `json:"finish_reason"`
}

type testChunkDelta struct {
	Content string `json:"content"`
	Role    string `json:"role"`
}

// sseLine marshals a single-choice OpenAI Chat Completions stream chunk
// and wraps it as an SSE data line. content and finishReason map to the
// delta's content and the choice's finish_reason respectively.
func sseLine(t *testing.T, content, finishReason string) string {
	t.Helper()
	chunk := testChunk{
		Choices: []testChunkChoice{{
			Index:        0,
			Delta:        testChunkDelta{Content: content},
			FinishReason: finishReason,
		}},
	}
	b, err := json.Marshal(chunk)
	if err != nil {
		t.Fatal(err)
	}
	return "data: " + string(b) + "\n\n"
}
