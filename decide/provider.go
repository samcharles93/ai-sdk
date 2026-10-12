package decide

import "context"

type Provider interface {
	Name() string
	Decide(context.Context, Request) (Response, error)
}
