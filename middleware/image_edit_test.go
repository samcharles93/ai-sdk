package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/samcharles93/ai-sdk/image"
	"github.com/samcharles93/ai-sdk/telemetry"
)

// stubEditor is a configurable image.Editor for middleware tests.
type stubEditor struct {
	name     string
	failures int
	err      error
	calls    int
}

func (s *stubEditor) Name() string { return s.name }
func (s *stubEditor) EditImage(ctx context.Context, req image.EditImageRequest) (image.EditImageResponse, error) {
	s.calls++
	if s.failures > 0 {
		s.failures--
		return image.EditImageResponse{}, s.err
	}
	return image.EditImageResponse{Images: []image.GeneratedImage{{URL: "https://example.com/out.png"}}}, nil
}

func TestRetryImageEdit_RetriesThenSucceeds(t *testing.T) {
	ed := &stubEditor{name: "xai", failures: 2, err: image.ErrProviderUnavailable}
	wrapped := RetryImageEdit(
		RetryConfig{MaxAttempts: 3},
		ExponentialBackoff{BaseDelay: time.Nanosecond},
		DefaultRetryableError,
	)(ed)

	resp, err := wrapped.EditImage(context.Background(), image.EditImageRequest{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if ed.calls != 3 {
		t.Errorf("calls = %d, want 3", ed.calls)
	}
	if len(resp.Images) != 1 {
		t.Errorf("expected 1 image, got %d", len(resp.Images))
	}
	if wrapped.Name() != "xai" {
		t.Errorf("Name() = %q, want xai", wrapped.Name())
	}
}

func TestRetryImageEdit_NonRetryableStops(t *testing.T) {
	ed := &stubEditor{name: "xai", failures: 1, err: image.ErrAuthFailed}
	wrapped := RetryImageEdit(
		RetryConfig{MaxAttempts: 3},
		ExponentialBackoff{BaseDelay: time.Nanosecond},
		DefaultRetryableError,
	)(ed)

	_, err := wrapped.EditImage(context.Background(), image.EditImageRequest{Model: "m"})
	if err == nil {
		t.Fatal("expected error")
	}
	if ed.calls != 1 {
		t.Errorf("calls = %d, want 1 (auth is not retryable)", ed.calls)
	}
}

func TestChainImageEdit_Composes(t *testing.T) {
	ed := &stubEditor{name: "xai"}
	wrapped := ChainImageEdit(
		func(next image.Editor) image.Editor {
			return NewTelemetryImageEditMiddleware(next, telemetry.NoopTracer{})
		},
		RetryImageEdit(RetryConfig{MaxAttempts: 1}, ExponentialBackoff{BaseDelay: time.Nanosecond}, DefaultRetryableError),
	)(ed)

	if got := wrapped.Name(); got != "xai" {
		t.Errorf("Name() = %q, want xai", got)
	}
	resp, err := wrapped.EditImage(context.Background(), image.EditImageRequest{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Images) != 1 {
		t.Errorf("expected 1 image, got %d", len(resp.Images))
	}
}

func TestCircuitBreakerImageEdit_OpenCircuit(t *testing.T) {
	ed := &stubEditor{name: "xai", failures: 1, err: image.ErrProviderUnavailable}
	wrapped := CircuitBreakerImageEdit(CircuitBreakerConfig{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		OpenTimeout:      time.Minute,
	})(ed)

	// First call fails, tripping the breaker to OPEN.
	if _, err := wrapped.EditImage(context.Background(), image.EditImageRequest{Model: "m"}); err == nil {
		t.Fatal("expected error on first call")
	}
	// Second call is rejected while the breaker is open.
	if _, err := wrapped.EditImage(context.Background(), image.EditImageRequest{Model: "m"}); err == nil {
		t.Fatal("expected circuit-open error on second call")
	} else if err != ErrCircuitOpen {
		t.Errorf("err = %v, want ErrCircuitOpen", err)
	}
}
