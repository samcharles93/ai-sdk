package chat

import "context"

// Client is a thin, provider-agnostic facade over a Provider. It centralises
// concerns that are independent of the underlying backend (such as toggling
// the streaming flag on the request) and provides a single entry point that
// higher-level code can depend on.
type Client struct {
	p Provider
}

// NewClient returns a Client backed by the given Provider. The Provider may
// be nil; in that case the Client's methods will return ErrNoProvider.
func NewClient(p Provider) *Client {
	return &Client{p: p}
}

// Provider returns the underlying Provider, which may be nil.
func (c *Client) Provider() Provider {
	if c == nil {
		return nil
	}
	return c.p
}

// Chat performs a non-streaming chat completion. It forces req.Stream to
// false before delegating to the underlying Provider. If the Client or its
// Provider is nil, Chat returns ErrNoProvider.
func (c *Client) Chat(ctx context.Context, req Request) (Response, error) {
	if c == nil || c.p == nil {
		return Response{}, ErrNoProvider
	}
	req.Stream = false
	return c.p.Chat(ctx, req)
}

// ChatStream performs a streaming chat completion. It forces req.Stream to
// true before delegating to the underlying Provider. If the Client or its
// Provider is nil, ChatStream returns ErrNoProvider.
func (c *Client) ChatStream(ctx context.Context, req Request) (Stream, error) {
	if c == nil || c.p == nil {
		return nil, ErrNoProvider
	}
	req.Stream = true
	return c.p.ChatStream(ctx, req)
}
