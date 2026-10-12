package decide

import "context"

type Client struct{ p Provider }

func NewClient(p Provider) *Client { return &Client{p: p} }
func (c *Client) Provider() Provider {
	if c == nil {
		return nil
	}
	return c.p
}

func (c *Client) Decide(ctx context.Context, req Request) (Response, error) {
	if c == nil || c.p == nil {
		return Response{}, ErrNoProvider
	}
	if err := req.Validate(); err != nil {
		return Response{}, err
	}
	return c.p.Decide(ctx, req)
}
