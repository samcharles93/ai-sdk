package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/image"
	"github.com/samcharles93/ai-sdk/object"
	"github.com/samcharles93/ai-sdk/speech"
	"github.com/samcharles93/ai-sdk/transcribe"
	"github.com/samcharles93/ai-sdk/video"
)

// abortSite names a core entry point that reports caller cancellation as
// ErrAborted, alongside the source location of the error literal it builds.
// Each site writes its own fmt.Errorf literal, so they drift independently —
// hence a case per site rather than one representative call.
type abortSite struct {
	name string
	site string
	call func(context.Context) error
}

// abortSites covers every entry point whose ctx.Err() guard runs before the
// provider is touched. The mocks are therefore never invoked and need no
// scripting.
func abortSites() []abortSite {
	return []abortSite{
		{"GenerateText", "generate.go:59", func(ctx context.Context) error {
			_, err := GenerateText(ctx, &fakeProvider{}, GenerateOptions{Model: "m", Prompt: "x"})
			return err
		}},
		{"GenerateObject", "object_impl.go:20", func(ctx context.Context) error {
			_, err := GenerateObject(ctx, &fakeObjectProvider{}, object.Request{Model: "m"})
			return err
		}},
		{"StreamObject", "object_impl.go:41", func(ctx context.Context) error {
			_, err := StreamObject(ctx, &fakeObjectProvider{}, object.Request{Model: "m"})
			return err
		}},
		{"GenerateImage", "image_impl.go:20", func(ctx context.Context) error {
			_, err := GenerateImage(ctx, &mockImageProvider{name: "test"}, image.GenerateImageRequest{Model: "m"})
			return err
		}},
		{"GenerateVideo", "video_impl.go:20", func(ctx context.Context) error {
			_, err := GenerateVideo(ctx, &mockVideoProvider{name: "test"}, video.GenerateVideoRequest{Model: "m"})
			return err
		}},
		{"GenerateSpeech", "speech_impl.go:20", func(ctx context.Context) error {
			_, err := GenerateSpeech(ctx, &mockSpeechProvider{name: "test"}, speech.GenerateSpeechRequest{Model: "m"})
			return err
		}},
		{"Transcribe", "transcribe_impl.go:19", func(ctx context.Context) error {
			_, err := Transcribe(ctx, &mockTranscribeProvider{name: "test"}, transcribe.TranscribeRequest{Model: "m"})
			return err
		}},
		{"StreamText", "stream_impl.go:106", func(ctx context.Context) error {
			p := &fakeProvider{streamScript: [][]chat.Chunk{{{Delta: "x", Done: true, FinishReason: "stop"}}}}
			r, err := StreamText(ctx, p, GenerateOptions{Model: "m", Prompt: "x"})
			if err != nil {
				return err
			}
			for range r.FullStream { //nolint:revive // drain to release the producer
			}
			_, ferr := r.FinishReason()
			return ferr
		}},
	}
}

// expiredContexts returns one already-dead context per cancellation cause.
func expiredContexts() []struct {
	name  string
	cause error
	ctx   func(t *testing.T) context.Context
} {
	return []struct {
		name  string
		cause error
		ctx   func(t *testing.T) context.Context
	}{
		{"Canceled", context.Canceled, func(t *testing.T) context.Context {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			t.Cleanup(cancel)
			return ctx
		}},
		{"DeadlineExceeded", context.DeadlineExceeded, func(t *testing.T) context.Context {
			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
			t.Cleanup(cancel)
			return ctx
		}},
	}
}

// TestAbortErrorWrapsCause pins the contract that an aborted call reports both
// ErrAborted and the underlying context error through errors.Is. Consumers
// distinguish a user-initiated cancellation from a genuine fault on the
// latter; formatting the cause with %v instead of %w silently breaks them.
func TestAbortErrorWrapsCause(t *testing.T) {
	for _, site := range abortSites() {
		for _, cc := range expiredContexts() {
			t.Run(site.name+"/"+cc.name, func(t *testing.T) {
				err := site.call(cc.ctx(t))
				if err == nil {
					t.Fatalf("%s: expected an error from an expired context", site.site)
				}
				if !errors.Is(err, ErrAborted) {
					t.Errorf("%s: errors.Is(err, ErrAborted) = false, want true (err: %v)", site.site, err)
				}
				if !errors.Is(err, cc.cause) {
					t.Errorf("%s: errors.Is(err, %v) = false, want true — the cause is formatted in, not wrapped (err: %v)",
						site.site, cc.cause, err)
				}
			})
		}
	}
}

// TestAbortErrorMessageUnchanged guards the rendered text. Moving from
// "%w: %v" to "%w: %w" must not shift the message, since consumers and
// existing tests match on it.
func TestAbortErrorMessageUnchanged(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := GenerateSpeech(ctx, &mockSpeechProvider{name: "test"}, speech.GenerateSpeechRequest{Model: "m"})
	if err == nil {
		t.Fatal("expected an error")
	}
	want := ErrAborted.Error() + ": " + context.Canceled.Error()
	if got := err.Error(); got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

// TestStreamTextAbortWrapsCauseMidStream covers the emit-failure sites in
// StreamText (stream_impl.go:113,161,170,181,215,254,268). Which of them fires
// depends on where the producer is when cancellation lands, so this asserts the
// invariant that holds at every one of them rather than pinning a single site.
func TestStreamTextAbortWrapsCauseMidStream(t *testing.T) {
	// More chunks than streamBufferSize so the producer blocks on emit
	// once the consumer stops draining.
	chunks := make([]chat.Chunk, 0, streamBufferSize*4)
	for range streamBufferSize * 4 {
		chunks = append(chunks, chat.Chunk{Delta: "tok"})
	}
	chunks = append(chunks, chat.Chunk{Done: true, FinishReason: "stop"})

	ctx, cancel := context.WithCancel(context.Background())
	p := &fakeProvider{streamScript: [][]chat.Chunk{chunks}}

	r, err := StreamText(ctx, p, GenerateOptions{Model: "m", Prompt: "x"})
	if err != nil {
		t.Fatalf("StreamText: %v", err)
	}

	// Cancel once the producer is underway, then stop draining so the
	// emit path blocks on a full buffer and observes ctx.Done().
	<-r.FullStream
	cancel()
	for range r.FullStream { //nolint:revive // drain to release the producer
	}

	// The producer may have completed the whole script before cancellation
	// landed; only assert the wrapping when it actually aborted.
	_, e := r.FinishReason()
	if e == nil {
		t.Skip("producer finished before cancellation landed; no abort to inspect")
	}
	if !errors.Is(e, ErrAborted) {
		t.Errorf("errors.Is(err, ErrAborted) = false, want true (err: %v)", e)
	}
	if !errors.Is(e, context.Canceled) {
		t.Errorf("errors.Is(err, context.Canceled) = false, want true — the cause is formatted in, not wrapped (err: %v)", e)
	}
}
