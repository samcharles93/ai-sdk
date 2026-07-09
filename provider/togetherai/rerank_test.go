package togetherai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/samcharles93/ai-sdk/rerank"
)

func TestRerank_Success(t *testing.T) {
	wantScore := 0.95
	var capturedBody map[string]any
	var capturedAuth string

	_, p := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: %s", r.Method)
		}
		if r.URL.Path != "/rerank" {
			t.Errorf("path: %s", r.URL.Path)
		}
		capturedAuth = r.Header.Get("Authorization")
		capturedBody = decodeBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "call-1",
			"model": "Salesforce/Llama-Rank-v1",
			"usage": map[string]any{
				"prompt_tokens": 50, "completion_tokens": 0, "total_tokens": 50,
			},
			"results": []map[string]any{
				{"index": float64(1), "relevance_score": wantScore},
				{"index": float64(0), "relevance_score": 0.42},
			},
		})
	})

	resp, err := p.Rerank(context.Background(), rerank.Request{
		Model:     "Salesforce/Llama-Rank-v1",
		Query:     "What animals can I find near Peru?",
		Documents: []string{"The giant panda", "The llama"},
		TopN:      3,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if capturedAuth != "Bearer sk-test" {
		t.Fatalf("auth: %q", capturedAuth)
	}
	if got := capturedBody["model"]; got != "Salesforce/Llama-Rank-v1" {
		t.Fatalf("model: %v", got)
	}
	if got := capturedBody["query"]; got != "What animals can I find near Peru?" {
		t.Fatalf("query: %v", got)
	}
	if got := capturedBody["return_documents"]; got != false {
		t.Fatalf("return_documents: %v (want false)", got)
	}
	if docs, ok := capturedBody["documents"].([]any); !ok || len(docs) != 2 {
		t.Fatalf("documents: %+v", capturedBody["documents"])
	}
	if topN, ok := capturedBody["top_n"].(float64); !ok || int(topN) != 3 {
		t.Fatalf("top_n: %+v", capturedBody["top_n"])
	}
	if len(resp.Ranking) != 2 {
		t.Fatalf("ranking len: %d", len(resp.Ranking))
	}
	if resp.Model != "Salesforce/Llama-Rank-v1" {
		t.Fatalf("model: %s", resp.Model)
	}
	if resp.Ranking[0].OriginalIndex != 1 || resp.Ranking[0].Score != wantScore {
		t.Fatalf("item 0: %+v", resp.Ranking[0])
	}
	if resp.Ranking[1].OriginalIndex != 0 || resp.Ranking[1].Score != 0.42 {
		t.Fatalf("item 1: %+v", resp.Ranking[1])
	}
}

func TestRerank_NoTopN(t *testing.T) {
	var capturedBody map[string]any
	_, p := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedBody = decodeBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "m",
			"usage": map[string]any{"total_tokens": 10},
			"results": []map[string]any{
				{"index": float64(0), "relevance_score": 1.0},
			},
		})
	})
	_, _ = p.Rerank(context.Background(), rerank.Request{Model: "m", Query: "q", Documents: []string{"x"}})
	if _, ok := capturedBody["top_n"]; ok {
		t.Fatal("top_n must be omitted when 0")
	}
}

func TestRerank_ProviderOptionsRankFields(t *testing.T) {
	var capturedBody map[string]any
	_, p := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedBody = decodeBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "m",
			"usage":   map[string]any{"total_tokens": 5},
			"results": []map[string]any{},
		})
	})
	_, _ = p.Rerank(context.Background(), rerank.Request{
		Model:     "m",
		Query:     "q",
		Documents: []string{"d"},
		ProviderOptions: map[string]any{
			"togetherai": Options{RankFields: []string{"title", "content"}},
		},
	})
	rf, ok := capturedBody["rank_fields"].([]any)
	if !ok || len(rf) != 2 {
		t.Fatalf("rank_fields: %+v", capturedBody["rank_fields"])
	}
}

func TestRerank_MissingFields(t *testing.T) {
	_, p := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("server must not be called")
	})
	if _, err := p.Rerank(context.Background(), rerank.Request{Query: "q", Documents: []string{"d"}}); !errors.Is(err, rerank.ErrInvalidRequest) {
		t.Fatalf("missing model: %v", err)
	}
	if _, err := p.Rerank(context.Background(), rerank.Request{Model: "m", Documents: []string{"d"}}); !errors.Is(err, rerank.ErrInvalidRequest) {
		t.Fatalf("missing query: %v", err)
	}
	if _, err := p.Rerank(context.Background(), rerank.Request{Model: "m", Query: "q"}); !errors.Is(err, rerank.ErrInvalidRequest) {
		t.Fatalf("missing documents: %v", err)
	}
}

func TestRerank_HTTPErrorMapping(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{"unauth", http.StatusUnauthorized, `{"error":{"message":"bad key"}}`, rerank.ErrAuthFailed},
		{"forbidden", http.StatusForbidden, `{"error":{"message":"nope"}}`, rerank.ErrAuthFailed},
		{"rate", http.StatusTooManyRequests, `{"error":{"message":"slow down"}}`, rerank.ErrRateLimited},
		{"server", http.StatusBadGateway, `{"error":{"message":"oops"}}`, rerank.ErrProviderUnavailable},
		{"badreq", http.StatusBadRequest, `{"error":{"message":"unknown model"}}`, rerank.ErrInvalidRequest},
		{"unparseable", http.StatusInternalServerError, `not json`, rerank.ErrProviderUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, p := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			_, err := p.Rerank(context.Background(), rerank.Request{Model: "m", Query: "q", Documents: []string{"d"}})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestRerank_ImplementsProvider(t *testing.T) {
	p, err := New(Config{APIKey: "k"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	var _ rerank.Provider = p
	if p.Name() != "togetherai" {
		t.Fatalf("name: %s", p.Name())
	}
}
