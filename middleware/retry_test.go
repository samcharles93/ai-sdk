package middleware

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/object"
)

// countingChatProvider records the number of calls to Chat / ChatStream and
// returns the configured error (or success) for each.
type countingChatProvider struct {
	name        string
	chatCalls   atomic.Int64
	streamCalls atomic.Int64

	chatFn   func(ctx context.Context, req chat.Request) (chat.Response, error)
	streamFn func(ctx context.Context, req chat.Request) (chat.Stream, error)
}

func (p *countingChatProvider) Name() string { return p.name }

func (p *countingChatProvider) Chat(ctx context.Context, req chat.Request) (chat.Response, error) {
	p.chatCalls.Add(1)
	return p.chatFn(ctx, req)
}

func (p *countingChatProvider) ChatStream(ctx context.Context, req chat.Request) (chat.Stream, error) {
	p.streamCalls.Add(1)
	return p.streamFn(ctx, req)
}

// countingObjectProvider records the number of calls to GenerateObject /
// StreamObject and returns the configured error (or success) for each.
type countingObjectProvider struct {
	name          string
	generateCalls atomic.Int64
	streamCalls   atomic.Int64

	generateFn func(ctx context.Context, req object.Request) (object.ObjectResult, error)
	streamFn   func(ctx context.Context, req object.Request) (object.ObjectStream, error)
}

func (p *countingObjectProvider) Name() string { return p.name }

func (p *countingObjectProvider) GenerateObject(ctx context.Context, req object.Request) (object.ObjectResult, error) {
	p.generateCalls.Add(1)
	return p.generateFn(ctx, req)
}

func (p *countingObjectProvider) StreamObject(ctx context.Context, req object.Request) (object.ObjectStream, error) {
	p.streamCalls.Add(1)
	return p.streamFn(ctx, req)
}

// emptyStream is a no-op chat.Stream: no chunks, returns io.EOF immediately.
type emptyStream struct{}

func (emptyStream) Next(_ context.Context) (chat.Chunk, error) { return chat.Chunk{}, io.EOF }
func (emptyStream) Close() error                               { return nil }

// emptyObjectStream is a no-op object.ObjectStream.
type emptyObjectStream struct{}

func (emptyObjectStream) Next(_ context.Context) (object.ObjectChunk, error) {
	return object.ObjectChunk{}, io.EOF
}
func (emptyObjectStream) Close() error { return nil }

// errRetryable is an error that the default retry classifier treats as
// retryable (transient network failure keyword).
var errRetryable = errors.New("connection refused")

// errNonRetryable is an error that the default retry classifier treats as
// non-retryable (auth keyword).
var errNonRetryable = errors.New("401 unauthorised")

// zeroBackoff returns Backoff(attempt)=0 so tests don't have to wait.
type zeroBackoff struct{}

func (zeroBackoff) Backoff(_ int) time.Duration { return 0 }

// alwaysRetryable treats every non-nil error as retryable. Tests use this to
// isolate retry-loop arithmetic from the default classifier's behaviour.
func alwaysRetryable(err error) bool { return err != nil }

// neverRetryable treats every error as non-retryable. Used to assert that a
// single failure short-circuits the loop (no retries).
func neverRetryable(_ error) bool { return false }

func TestRetryChat_AllAttemptsFail_ExactCallCount(t *testing.T) {
	const maxAttempts = 3
	p := &countingChatProvider{
		name: "always-fail",
		chatFn: func(_ context.Context, _ chat.Request) (chat.Response, error) {
			return chat.Response{}, errRetryable
		},
	}

	mw := RetryChat(RetryConfig{MaxAttempts: maxAttempts}, zeroBackoff{}, alwaysRetryable)(p)

	_, err := mw.Chat(context.Background(), chat.Request{Model: "m"})
	if !errors.Is(err, errRetryable) {
		t.Fatalf("expected errRetryable, got %v", err)
	}

	if got := p.chatCalls.Load(); got != int64(maxAttempts) {
		t.Errorf("Chat call count = %d, want %d (off-by-one would be %d)", got, maxAttempts, maxAttempts+1)
	}
}

// Regression for ai-sdk-5jb.1: retry_chat.go:47 ChatStream used to perform
// MaxAttempts+1 calls instead of MaxAttempts.
func TestRetryChatStream_AllAttemptsFail_ExactCallCount(t *testing.T) {
	const maxAttempts = 3
	p := &countingChatProvider{
		name: "always-fail",
		streamFn: func(_ context.Context, _ chat.Request) (chat.Stream, error) {
			return nil, errRetryable
		},
	}

	mw := RetryChat(RetryConfig{MaxAttempts: maxAttempts}, zeroBackoff{}, alwaysRetryable)(p)

	_, err := mw.ChatStream(context.Background(), chat.Request{Model: "m"})
	if !errors.Is(err, errRetryable) {
		t.Fatalf("expected errRetryable, got %v", err)
	}

	if got := p.streamCalls.Load(); got != int64(maxAttempts) {
		t.Errorf("ChatStream call count = %d, want %d (off-by-one would be %d)", got, maxAttempts, maxAttempts+1)
	}
}

func TestRetryGenerateObject_AllAttemptsFail_ExactCallCount(t *testing.T) {
	const maxAttempts = 3
	p := &countingObjectProvider{
		name: "always-fail",
		generateFn: func(_ context.Context, _ object.Request) (object.ObjectResult, error) {
			return nil, errRetryable
		},
	}

	mw := RetryObject(RetryConfig{MaxAttempts: maxAttempts}, zeroBackoff{}, alwaysRetryable)(p)

	_, err := mw.GenerateObject(context.Background(), object.Request{Model: "m"})
	if !errors.Is(err, errRetryable) {
		t.Fatalf("expected errRetryable, got %v", err)
	}

	if got := p.generateCalls.Load(); got != int64(maxAttempts) {
		t.Errorf("GenerateObject call count = %d, want %d (off-by-one would be %d)", got, maxAttempts, maxAttempts+1)
	}
}

// Regression for ai-sdk-5jb.1: retry_object.go:41 StreamObject used to perform
// MaxAttempts+1 calls instead of MaxAttempts.
func TestRetryStreamObject_AllAttemptsFail_ExactCallCount(t *testing.T) {
	const maxAttempts = 3
	p := &countingObjectProvider{
		name: "always-fail",
		streamFn: func(_ context.Context, _ object.Request) (object.ObjectStream, error) {
			return nil, errRetryable
		},
	}

	mw := RetryObject(RetryConfig{MaxAttempts: maxAttempts}, zeroBackoff{}, alwaysRetryable)(p)

	_, err := mw.StreamObject(context.Background(), object.Request{Model: "m"})
	if !errors.Is(err, errRetryable) {
		t.Fatalf("expected errRetryable, got %v", err)
	}

	if got := p.streamCalls.Load(); got != int64(maxAttempts) {
		t.Errorf("StreamObject call count = %d, want %d (off-by-one would be %d)", got, maxAttempts, maxAttempts+1)
	}
}

// MaxAttempts=1 must mean "one call, no retries" — including for streaming,
// where the bug previously triggered the extra unscheduled call path.
func TestRetryChatStream_MaxAttemptsOne_NoRetries(t *testing.T) {
	p := &countingChatProvider{
		name: "always-fail",
		streamFn: func(_ context.Context, _ chat.Request) (chat.Stream, error) {
			return nil, errRetryable
		},
	}

	mw := RetryChat(RetryConfig{MaxAttempts: 1}, zeroBackoff{}, alwaysRetryable)(p)

	_, err := mw.ChatStream(context.Background(), chat.Request{Model: "m"})
	if !errors.Is(err, errRetryable) {
		t.Fatalf("expected errRetryable, got %v", err)
	}

	if got := p.streamCalls.Load(); got != 1 {
		t.Errorf("ChatStream call count = %d, want 1", got)
	}
}

func TestRetryStreamObject_MaxAttemptsOne_NoRetries(t *testing.T) {
	p := &countingObjectProvider{
		name: "always-fail",
		streamFn: func(_ context.Context, _ object.Request) (object.ObjectStream, error) {
			return nil, errRetryable
		},
	}

	mw := RetryObject(RetryConfig{MaxAttempts: 1}, zeroBackoff{}, alwaysRetryable)(p)

	_, err := mw.StreamObject(context.Background(), object.Request{Model: "m"})
	if !errors.Is(err, errRetryable) {
		t.Fatalf("expected errRetryable, got %v", err)
	}

	if got := p.streamCalls.Load(); got != 1 {
		t.Errorf("StreamObject call count = %d, want 1", got)
	}
}

// Success on the first attempt must short-circuit; no retries, no off-by-one.
func TestRetryChat_SuccessFirstAttempt_SingleCall(t *testing.T) {
	p := &countingChatProvider{
		name: "ok",
		chatFn: func(_ context.Context, _ chat.Request) (chat.Response, error) {
			return chat.Response{Content: "hi"}, nil
		},
	}

	mw := RetryChat(RetryConfig{MaxAttempts: 5}, zeroBackoff{}, alwaysRetryable)(p)

	if _, err := mw.Chat(context.Background(), chat.Request{Model: "m"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := p.chatCalls.Load(); got != 1 {
		t.Errorf("Chat call count = %d, want 1", got)
	}
}

func TestRetryChatStream_SuccessFirstAttempt_SingleCall(t *testing.T) {
	p := &countingChatProvider{
		name: "ok",
		streamFn: func(_ context.Context, _ chat.Request) (chat.Stream, error) {
			return emptyStream{}, nil
		},
	}

	mw := RetryChat(RetryConfig{MaxAttempts: 5}, zeroBackoff{}, alwaysRetryable)(p)

	stream, err := mw.ChatStream(context.Background(), chat.Request{Model: "m"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := stream.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF from empty stream, got %v", err)
	}
	_ = stream.Close()

	if got := p.streamCalls.Load(); got != 1 {
		t.Errorf("ChatStream call count = %d, want 1", got)
	}
}

func TestRetryStreamObject_SuccessFirstAttempt_SingleCall(t *testing.T) {
	p := &countingObjectProvider{
		name: "ok",
		streamFn: func(_ context.Context, _ object.Request) (object.ObjectStream, error) {
			return emptyObjectStream{}, nil
		},
	}

	mw := RetryObject(RetryConfig{MaxAttempts: 5}, zeroBackoff{}, alwaysRetryable)(p)

	stream, err := mw.StreamObject(context.Background(), object.Request{Model: "m"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := stream.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF from empty stream, got %v", err)
	}
	_ = stream.Close()

	if got := p.streamCalls.Load(); got != 1 {
		t.Errorf("StreamObject call count = %d, want 1", got)
	}
}

// A non-retryable error must short-circuit the loop: exactly one call.
func TestRetryChatStream_NonRetryableError_SingleCall(t *testing.T) {
	p := &countingChatProvider{
		name: "auth-fail",
		streamFn: func(_ context.Context, _ chat.Request) (chat.Stream, error) {
			return nil, errNonRetryable
		},
	}

	mw := RetryChat(RetryConfig{MaxAttempts: 5}, zeroBackoff{}, neverRetryable)(p)

	_, err := mw.ChatStream(context.Background(), chat.Request{Model: "m"})
	if !errors.Is(err, errNonRetryable) {
		t.Fatalf("expected errNonRetryable, got %v", err)
	}

	if got := p.streamCalls.Load(); got != 1 {
		t.Errorf("ChatStream call count = %d, want 1 (non-retryable must short-circuit)", got)
	}
}

func TestRetryStreamObject_NonRetryableError_SingleCall(t *testing.T) {
	p := &countingObjectProvider{
		name: "auth-fail",
		streamFn: func(_ context.Context, _ object.Request) (object.ObjectStream, error) {
			return nil, errNonRetryable
		},
	}

	mw := RetryObject(RetryConfig{MaxAttempts: 5}, zeroBackoff{}, neverRetryable)(p)

	_, err := mw.StreamObject(context.Background(), object.Request{Model: "m"})
	if !errors.Is(err, errNonRetryable) {
		t.Fatalf("expected errNonRetryable, got %v", err)
	}

	if got := p.streamCalls.Load(); got != 1 {
		t.Errorf("StreamObject call count = %d, want 1 (non-retryable must short-circuit)", got)
	}
}

// Success on a later attempt must stop retrying; the call count should equal
// the 1-based index of the successful attempt.
func TestRetryChat_SuccessAfterRetries_ExactCallCount(t *testing.T) {
	const succeedOnAttempt = 3
	var seen atomic.Int64
	p := &countingChatProvider{
		name: "transient",
		chatFn: func(_ context.Context, _ chat.Request) (chat.Response, error) {
			n := seen.Add(1)
			if n < int64(succeedOnAttempt) {
				return chat.Response{}, errRetryable
			}
			return chat.Response{Content: "ok"}, nil
		},
	}

	mw := RetryChat(RetryConfig{MaxAttempts: 5}, zeroBackoff{}, alwaysRetryable)(p)

	if _, err := mw.Chat(context.Background(), chat.Request{Model: "m"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := p.chatCalls.Load(); got != int64(succeedOnAttempt) {
		t.Errorf("Chat call count = %d, want %d", got, succeedOnAttempt)
	}
}

func TestRetryChatStream_SuccessAfterRetries_ExactCallCount(t *testing.T) {
	const succeedOnAttempt = 3
	var seen atomic.Int64
	p := &countingChatProvider{
		name: "transient",
		streamFn: func(_ context.Context, _ chat.Request) (chat.Stream, error) {
			n := seen.Add(1)
			if n < int64(succeedOnAttempt) {
				return nil, errRetryable
			}
			return emptyStream{}, nil
		},
	}

	mw := RetryChat(RetryConfig{MaxAttempts: 5}, zeroBackoff{}, alwaysRetryable)(p)

	stream, err := mw.ChatStream(context.Background(), chat.Request{Model: "m"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := stream.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF, got %v", err)
	}
	_ = stream.Close()

	if got := p.streamCalls.Load(); got != int64(succeedOnAttempt) {
		t.Errorf("ChatStream call count = %d, want %d", got, succeedOnAttempt)
	}
}

func TestRetryStreamObject_SuccessAfterRetries_ExactCallCount(t *testing.T) {
	const succeedOnAttempt = 3
	var seen atomic.Int64
	p := &countingObjectProvider{
		name: "transient",
		streamFn: func(_ context.Context, _ object.Request) (object.ObjectStream, error) {
			n := seen.Add(1)
			if n < int64(succeedOnAttempt) {
				return nil, errRetryable
			}
			return emptyObjectStream{}, nil
		},
	}

	mw := RetryObject(RetryConfig{MaxAttempts: 5}, zeroBackoff{}, alwaysRetryable)(p)

	stream, err := mw.StreamObject(context.Background(), object.Request{Model: "m"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := stream.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF, got %v", err)
	}
	_ = stream.Close()

	if got := p.streamCalls.Load(); got != int64(succeedOnAttempt) {
		t.Errorf("StreamObject call count = %d, want %d", got, succeedOnAttempt)
	}
}

// When the loop returns mid-backoff on context cancellation, no extra call is
// made after the loop either.
func TestRetryChatStream_ContextCancelled_NoExtraCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// smallBackoff returns a non-zero delay so the cancellation actually has
	// time to propagate through sleepContext before the timer fires.
	backoff := smallBackoff{}

	p := &countingChatProvider{
		name: "fail-then-cancel",
		streamFn: func(_ context.Context, _ chat.Request) (chat.Stream, error) {
			// Cancel on the first call so the upcoming backoff sleep
			// sees ctx.Done() and returns ctx.Err. The retry loop must
			// return immediately without issuing another call.
			cancel()
			return nil, errRetryable
		},
	}

	mw := RetryChat(RetryConfig{MaxAttempts: 5}, backoff, alwaysRetryable)(p)

	_, _ = mw.ChatStream(ctx, chat.Request{Model: "m"})

	// Call 1 fails; sleep sees ctx.Err; loop returns. No off-by-one tail.
	// We allow 1 or 2 (in case of scheduler quirks) but reject 3+ which would
	// indicate the old post-loop call was re-introduced.
	if got := p.streamCalls.Load(); got < 1 || got > 2 {
		t.Errorf("ChatStream call count = %d, expected 1 (≤2 acceptable); off-by-one tail would yield ≥3", got)
	}
}

// smallBackoff returns a small but non-zero backoff delay. Tests that rely on
// context cancellation propagating through sleepContext use this so the timer
// select actually yields to ctx.Done().
type smallBackoff struct{}

func (smallBackoff) Backoff(_ int) time.Duration { return 10 * time.Millisecond }
