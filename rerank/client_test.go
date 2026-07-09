package rerank

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// stubProvider implements Provider for testing the Client facade.
type stubProvider struct {
	rerankFn func(ctx context.Context, req Request) (Response, error)
}

func (s *stubProvider) Name() string { return "stub" }
func (s *stubProvider) Rerank(ctx context.Context, req Request) (Response, error) {
	return s.rerankFn(ctx, req)
}

func TestClient_IsNil(t *testing.T) {
	var c *Client
	_, err := c.Rerank(context.Background(), Request{Query: "q", Documents: []string{"d"}})
	if !errors.Is(err, ErrNoProvider) {
		t.Fatalf("nil client: got %v, want ErrNoProvider", err)
	}
	if c.Provider() != nil {
		t.Fatal("nil client.Provider must return nil")
	}
}

func TestClient_NoProvider(t *testing.T) {
	c := NewClient(nil)
	_, err := c.Rerank(context.Background(), Request{Query: "q", Documents: []string{"d"}})
	if !errors.Is(err, ErrNoProvider) {
		t.Fatalf("no provider: got %v, want ErrNoProvider", err)
	}
	if c.Provider() != nil {
		t.Fatal("nil-initialised client.Provider must return nil")
	}
}

func TestClient_ValidatesQuery(t *testing.T) {
	c := NewClient(&stubProvider{rerankFn: func(ctx context.Context, req Request) (Response, error) {
		t.Error("provider must not be called with empty query")
		return Response{}, nil
	}})
	_, err := c.Rerank(context.Background(), Request{Query: "", Documents: []string{"d"}})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty query: got %v, want ErrInvalidRequest", err)
	}
}

func TestClient_ValidatesDocuments(t *testing.T) {
	c := NewClient(&stubProvider{rerankFn: func(ctx context.Context, req Request) (Response, error) {
		t.Error("provider must not be called with empty documents")
		return Response{}, nil
	}})
	_, err := c.Rerank(context.Background(), Request{Query: "q", Documents: nil})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil documents: got %v, want ErrInvalidRequest", err)
	}
	_, err = c.Rerank(context.Background(), Request{Query: "q", Documents: []string{}})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty documents: got %v, want ErrInvalidRequest", err)
	}
}

func TestClient_DelegatesToProvider(t *testing.T) {
	wantReq := Request{
		Model:     "m",
		Query:     "q",
		Documents: []string{"a", "b"},
		TopN:      1,
	}
	wantResp := Response{Model: "m", Ranking: []RankingItem{{OriginalIndex: 0, Score: 0.99, Document: "a"}}}
	p := &stubProvider{rerankFn: func(ctx context.Context, req Request) (Response, error) {
		if req.Model != wantReq.Model || req.Query != wantReq.Query || len(req.Documents) != 2 {
			t.Fatalf("unexpected request: %+v", req)
		}
		return wantResp, nil
	}}
	c := NewClient(p)
	resp, err := c.Rerank(context.Background(), wantReq)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp.Model != wantResp.Model || len(resp.Ranking) != 1 {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestClient_ProviderPropagatesErrors(t *testing.T) {
	c := NewClient(&stubProvider{rerankFn: func(ctx context.Context, req Request) (Response, error) {
		return Response{}, ErrProviderUnavailable
	}})
	_, err := c.Rerank(context.Background(), Request{Model: "m", Query: "q", Documents: []string{"d"}})
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("got %v, want ErrProviderUnavailable", err)
	}
}

func TestRequest_ZeroValueTopN(t *testing.T) {
	req := Request{TopN: 0}
	if req.TopN != 0 {
		t.Fatal("zero TopN must remain zero")
	}
}

func TestResponse_EmptyRanking(t *testing.T) {
	resp := Response{Ranking: nil}
	if resp.Ranking != nil {
		t.Fatal("nil Ranking expected on zero-value Response")
	}
	resp2 := Response{Ranking: []RankingItem{}}
	if len(resp2.Ranking) != 0 {
		t.Fatal("empty slice Ranking must have len 0")
	}
}

func TestSentinelErrors_AreDistinct(t *testing.T) {
	sentinels := []error{
		ErrNoProvider,
		ErrInvalidRequest,
		ErrProviderUnavailable,
		ErrRateLimited,
		ErrAuthFailed,
		ErrUnsupported,
	}
	for i, a := range sentinels {
		for j, b := range sentinels {
			if i != j && errors.Is(a, b) {
				t.Fatalf("sentinel %v and %v must be distinct via errors.Is", a, b)
			}
		}
	}
}

func TestSentinelErrors_ContainText(t *testing.T) {
	wants := map[error]string{
		ErrNoProvider:          "rerank:",
		ErrInvalidRequest:      "rerank:",
		ErrProviderUnavailable: "rerank:",
		ErrRateLimited:         "rerank:",
		ErrAuthFailed:          "rerank:",
		ErrUnsupported:         "rerank:",
	}
	for sentinel, prefix := range wants {
		if !strings.HasPrefix(sentinel.Error(), prefix) {
			t.Fatalf("%v does not have prefix %q", sentinel, prefix)
		}
	}
}
