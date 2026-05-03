package errx

import "testing"

func TestSentinelErrors(t *testing.T) {
	if ErrInvalidInput == nil || ErrTimeout == nil || ErrProviderNotAvailable == nil {
		t.Fatal("expected sentinel errors to be non-nil")
	}
}
