package core

import (
	"context"
	"errors"
	"testing"

	"github.com/samcharles93/ai-sdk/rerank"
)

// mockRerankProvider implements rerank.Provider for tests.
type mockRerankProvider struct {
	name string
	fn   func(ctx context.Context, req rerank.Request) (rerank.Response, error)
}

func (m *mockRerankProvider) Name() string { return m.name }
func (m *mockRerankProvider) Rerank(ctx context.Context, req rerank.Request) (rerank.Response, error) {
	return m.fn(ctx, req)
}

func TestRerank_NoProvider(t *testing.T) {
	_, err := Rerank(context.Background(), nil, rerank.Request{Query: "q", Documents: []string{"d"}})
	if !errors.Is(err, ErrNoProvider) {
		t.Errorf("expected ErrNoProvider, got %v", err)
	}
}

func TestRerank_ValidRequest(t *testing.T) {
	p := &mockRerankProvider{name: "test", fn: func(ctx context.Context, req rerank.Request) (rerank.Response, error) {
		return rerank.Response{
			Model:   "m",
			Ranking: []rerank.RankingItem{{OriginalIndex: 0, Score: 0.9, Document: "a"}},
		}, nil
	}}
	resp, err := Rerank(context.Background(), p, rerank.Request{Model: "m", Query: "q", Documents: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Ranking) != 1 {
		t.Errorf("expected 1 ranking item, got %d", len(resp.Ranking))
	}
}

func TestRerank_ProviderError(t *testing.T) {
	wantErr := errors.New("provider error")
	p := &mockRerankProvider{name: "test", fn: func(ctx context.Context, req rerank.Request) (rerank.Response, error) {
		return rerank.Response{}, wantErr
	}}
	_, err := Rerank(context.Background(), p, rerank.Request{Query: "q", Documents: []string{"d"}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped provider error, got %v", err)
	}
}

func TestRerank_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &mockRerankProvider{name: "test"}
	_, err := Rerank(ctx, p, rerank.Request{Query: "q", Documents: []string{"d"}})
	if !errors.Is(err, ErrAborted) {
		t.Errorf("expected ErrAborted, got %v", err)
	}
}
