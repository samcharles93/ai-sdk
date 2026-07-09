package embed

import (
	"context"
	"errors"
	"testing"
)

type fakeProvider struct {
	lastReq Request
	resp    Response
	respErr error
	calls   int
}

func (p *fakeProvider) Name() string { return "fake" }

func (p *fakeProvider) Embed(ctx context.Context, req Request) (Response, error) {
	p.calls++
	p.lastReq = req
	return p.resp, p.respErr
}

func TestNewClient_NilProvider(t *testing.T) {
	c := NewClient(nil)
	if _, err := c.Embed(context.Background(), Request{Model: "m", Inputs: []string{"a"}}); !errors.Is(err, ErrNoProvider) {
		t.Fatalf("Embed with nil provider: got %v, want ErrNoProvider", err)
	}

	var nilC *Client
	if _, err := nilC.Embed(context.Background(), Request{Model: "m", Inputs: []string{"a"}}); !errors.Is(err, ErrNoProvider) {
		t.Fatalf("nil client Embed: got %v, want ErrNoProvider", err)
	}
}

func TestEmbed_NoInputs(t *testing.T) {
	p := &fakeProvider{}
	c := NewClient(p)
	if _, err := c.Embed(context.Background(), Request{Model: "m"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Embed with no inputs: got %v, want ErrInvalidRequest", err)
	}
	if p.calls != 0 {
		t.Fatalf("provider should not be called on invalid request, calls=%d", p.calls)
	}
}

func TestEmbed_Success(t *testing.T) {
	want := Response{
		Model: "m",
		Embeddings: []Embedding{
			{Index: 0, Vector: []float32{0.1, 0.2}},
			{Index: 1, Vector: []float32{0.3, 0.4}},
		},
		Usage: Usage{PromptTokens: 4, TotalTokens: 4},
	}
	p := &fakeProvider{resp: want}
	c := NewClient(p)

	got, err := c.Embed(context.Background(), Request{Model: "m", Inputs: []string{"hello", "world"}})
	if err != nil {
		t.Fatalf("Embed: unexpected error: %v", err)
	}
	if got.Model != want.Model || len(got.Embeddings) != 2 {
		t.Fatalf("Embed: got %+v, want %+v", got, want)
	}
	for i, e := range got.Embeddings {
		if e.Index != want.Embeddings[i].Index {
			t.Fatalf("Embedding[%d].Index = %d, want %d", i, e.Index, want.Embeddings[i].Index)
		}
		if len(e.Vector) != len(want.Embeddings[i].Vector) {
			t.Fatalf("Embedding[%d].Vector len = %d, want %d", i, len(e.Vector), len(want.Embeddings[i].Vector))
		}
		for j, v := range e.Vector {
			if v != want.Embeddings[i].Vector[j] {
				t.Fatalf("Embedding[%d].Vector[%d] = %v, want %v", i, j, v, want.Embeddings[i].Vector[j])
			}
		}
	}
	if got.Usage != want.Usage {
		t.Fatalf("Usage: got %+v, want %+v", got.Usage, want.Usage)
	}
	if p.lastReq.Model != "m" || len(p.lastReq.Inputs) != 2 {
		t.Fatalf("provider received unexpected request: %+v", p.lastReq)
	}
	if c.Provider() != p {
		t.Fatalf("Provider() did not return underlying provider")
	}
}

func TestEmbed_PropagatesError(t *testing.T) {
	sentinel := errors.New("boom")
	p := &fakeProvider{respErr: sentinel}
	c := NewClient(p)

	if _, err := c.Embed(context.Background(), Request{Model: "m", Inputs: []string{"a"}}); !errors.Is(err, sentinel) {
		t.Fatalf("Embed: got %v, want %v", err, sentinel)
	}
}
