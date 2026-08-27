package toolkit

import (
	"testing"
	"time"
)

func TestShellTimeoutUsesOnlyExplicitUncappedValue(t *testing.T) {
	if timeout, ok := shellTimeout(0); ok || timeout != 0 {
		t.Fatalf("unset timeout = %s, %t; want 0, false", timeout, ok)
	}

	const seconds = 60 * 60
	if timeout, ok := shellTimeout(seconds); !ok || timeout != time.Hour {
		t.Fatalf("explicit timeout = %s, %t; want 1h, true", timeout, ok)
	}
}
