package xai

import (
	"context"
	"testing"
	"time"
)

func TestPollContextUsesOnlyExplicitTimeout(t *testing.T) {
	t.Run("unset preserves caller context", func(t *testing.T) {
		ctx, cancel := pollContext(context.Background(), 0)
		defer cancel()

		if deadline, ok := ctx.Deadline(); ok {
			t.Fatalf("unexpected implicit polling deadline: %s", deadline)
		}
	})

	t.Run("positive milliseconds opt in", func(t *testing.T) {
		const timeout = 1500
		start := time.Now()
		ctx, cancel := pollContext(context.Background(), timeout)
		defer cancel()

		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("explicit polling timeout did not set a deadline")
		}
		if got := deadline.Sub(start); got < 1400*time.Millisecond || got > 1600*time.Millisecond {
			t.Fatalf("polling timeout = %s, want about 1.5s", got)
		}
	})
}
