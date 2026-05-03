package object

import "context"

// Client is a thin, provider-agnostic facade over a Provider. It centralises
// concerns that are independent of the underlying backend and nil-guards
// provider calls for callers.
type Client struct {
	p Provider
}

// NewClient returns a Client backed by the given Provider. The Provider may
// be nil; in that case the Client's methods will return ErrNoProvider.
func NewClient(p Provider) *Client { return &Client{p: p} }

// Provider returns the underlying Provider, which may be nil.
func (c *Client) Provider() Provider {
	if c == nil {
		return nil
	}
	return c.p
}

// GenerateObject performs a non-streaming object generation request. If the
// Client or its Provider is nil, ErrNoProvider is returned.
func (c *Client) GenerateObject(ctx context.Context, req Request) (ObjectResult, error) {
	if c == nil || c.p == nil {
		return nil, ErrNoProvider
	}
	return c.p.GenerateObject(ctx, req)
}

// StreamObject performs a streaming object generation request. If the Client
// or its Provider is nil, ErrNoProvider is returned. The caller must Close the
// returned ObjectStream when finished.
func (c *Client) StreamObject(ctx context.Context, req Request) (ObjectStream, error) {
	if c == nil || c.p == nil {
		return nil, ErrNoProvider
	}
	return c.p.StreamObject(ctx, req)
}
