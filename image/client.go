package image

import "context"

// Client is a thin, provider-agnostic facade over a Provider. It centralises
// concerns that are independent of the underlying backend and provides a
// single entry point that higher-level code can depend on.
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

// GenerateImage creates images from a prompt by delegating to the
// underlying Provider. If the Client or its Provider is nil, it returns
// ErrNoProvider.
func (c *Client) GenerateImage(ctx context.Context, req GenerateImageRequest) (GenerateImageResponse, error) {
	if c == nil || c.p == nil {
		return GenerateImageResponse{}, ErrNoProvider
	}
	if req.Prompt == "" {
		return GenerateImageResponse{}, ErrInvalidRequest
	}
	return c.p.GenerateImage(ctx, req)
}
