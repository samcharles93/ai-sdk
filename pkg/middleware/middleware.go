package middleware

import (
	"context"

	"github.com/samcharles93/ai-sdk/pkg/chat"
)

// ChatMiddleware wraps a chat.Provider to intercept and potentially modify
// requests and responses. Middleware can be stacked to compose behaviour.
//
// Example (logging middleware):
//
//	func LoggingMiddleware(next chat.Provider) chat.Provider {
//	    return &loggingProvider{next: next}
//	}
type ChatMiddleware func(next chat.Provider) chat.Provider

// ChatRequestHook is called before a chat request is sent. It may modify
// the request or return an error to short-circuit.
type ChatRequestHook func(ctx context.Context, req *chat.Request) error

// ChatResponseHook is called after a chat response is received. It may
// modify the response or record metrics.
type ChatResponseHook func(ctx context.Context, req *chat.Request, resp *chat.Response) error

// ChainChat composes ChatMiddleware slices using the generic ChainGeneric.
func ChainChat(ms ...ChatMiddleware) ChatMiddleware {
	return ChainGeneric[chat.Provider](ms...)
}
