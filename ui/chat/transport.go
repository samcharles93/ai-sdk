package chat

import (
	"context"
	"errors"
	"fmt"

	domain "github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/core"
	"github.com/samcharles93/ai-sdk/uimessage"
	"github.com/samcharles93/ai-sdk/uimessage/sse"
)

// Transport abstracts how messages travel between a [Chat] and the
// model. Implementations return a channel of [uimessage.Chunk] events
// produced by the assistant's response. Transports must close the
// channel when the turn ends (success, error, or cancellation).
type Transport interface {
	SendMessages(ctx context.Context, chatID string, messages []Message, opts SendOptions) (<-chan Chunk, error)
}

// ---------------------------------------------------------------------------
// DefaultTransport — HTTP POST + SSE (network)
// ---------------------------------------------------------------------------

// DefaultTransport is a placeholder for the HTTP+SSE network transport.
// The full implementation lands in Phase D; this stub satisfies the
// interface so [Chat] can be wired up.
type DefaultTransport struct {
	API     string
	Headers map[string]string
	Body    map[string]any
}

// NewDefaultTransport returns a [DefaultTransport] for the given URL.
func NewDefaultTransport(api string) *DefaultTransport {
	return &DefaultTransport{
		API:     api,
		Headers: make(map[string]string),
		Body:    make(map[string]any),
	}
}

// ErrNotImplemented is returned by stub transports until the network
// path is filled in (Phase D).
var ErrNotImplemented = errors.New("chat transport: not implemented")

// SendMessages implements [Transport].
func (t *DefaultTransport) SendMessages(_ context.Context, _ string, _ []Message, _ SendOptions) (<-chan Chunk, error) {
	return nil, ErrNotImplemented
}

// ---------------------------------------------------------------------------
// DirectTransport — in-process via core.StreamText
// ---------------------------------------------------------------------------

// DirectTransport calls [core.StreamText] in a goroutine and bridges
// its [core.StreamPart] output to UI message [Chunk] events using the
// same translator as the SSE writer ([sse.FromTextStream]). It is the
// Go equivalent of the JS DirectChatTransport.
type DirectTransport struct {
	provider domain.Provider
	opts     core.GenerateOptions
}

// NewDirectTransport returns a [DirectTransport] backed by provider
// and options. The Messages field of opts is overridden per call.
func NewDirectTransport(provider domain.Provider, opts core.GenerateOptions) *DirectTransport {
	return &DirectTransport{provider: provider, opts: opts}
}

// SendMessages implements [Transport]. It converts the UI messages to
// chat domain messages via [uimessage.ToModelMessages], runs
// [core.StreamText], and pipes the resulting stream through
// [sse.FromTextStream].
func (t *DirectTransport) SendMessages(ctx context.Context, chatID string, messages []Message, _ SendOptions) (<-chan Chunk, error) {
	domainMsgs, err := uimessage.ToModelMessages(messages, uimessage.ToModelOptions{})
	if err != nil {
		return nil, fmt.Errorf("convert to model messages: %w", err)
	}
	opts := t.opts
	opts.Messages = domainMsgs
	opts.Prompt = ""

	result, err := core.StreamText(ctx, t.provider, opts)
	if err != nil {
		return nil, fmt.Errorf("stream text: %w", err)
	}
	return sse.FromTextStream(ctx, &result, chatID), nil
}
