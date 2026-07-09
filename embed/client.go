package embed

import "context"

// Client is a thin, provider-agnostic facade over a Provider. It centralises
// concerns that are independent of the underlying backend and provides a
// single entry point that higher-level code can depend on.
type Client struct {
	provider Provider
}

// NewClient returns a Client backed by the given Provider. The Provider may
// be nil; in that case the Client's methods will return ErrNoProvider.
func NewClient(p Provider) *Client {
	return &Client{provider: p}
}

// Provider returns the underlying Provider, which may be nil.
func (c *Client) Provider() Provider {
	if c == nil {
		return nil
	}
	return c.provider
}

// Embed produces embeddings for req.Inputs by delegating to the underlying
// Provider. If the Client or its Provider is nil, Embed returns
// ErrNoProvider. If req.Inputs is empty, Embed returns ErrInvalidRequest.
func (c *Client) Embed(ctx context.Context, req Request) (Response, error) {
	if c == nil || c.provider == nil {
		return Response{}, ErrNoProvider
	}
	if len(req.Inputs) == 0 {
		return Response{}, ErrInvalidRequest
	}
	return c.provider.Embed(ctx, req)
}

// EmbedOne is a convenience wrapper around Embed for the single-input case.
// It returns the produced embedding, the request's usage, and any error.
//
// EmbedOne returns ErrNoProvider when the Client has no provider, and
// ErrInvalidRequest when value is empty or the provider returns no
// embeddings.
func (c *Client) EmbedOne(ctx context.Context, model, value string) (Embedding, Usage, error) {
	if c == nil || c.provider == nil {
		return Embedding{}, Usage{}, ErrNoProvider
	}
	if value == "" {
		return Embedding{}, Usage{}, ErrInvalidRequest
	}
	resp, err := c.provider.Embed(ctx, Request{Model: model, Inputs: []string{value}})
	if err != nil {
		return Embedding{}, Usage{}, err
	}
	if len(resp.Embeddings) == 0 {
		return Embedding{}, resp.Usage, ErrInvalidRequest
	}
	return resp.Embeddings[0], resp.Usage, nil
}
