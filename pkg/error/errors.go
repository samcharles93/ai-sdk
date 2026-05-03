package errx

import "errors"

var (
	// Common input/flow errors
	ErrInvalidInput   = errors.New("invalid input")
	ErrTimeout        = errors.New("timeout")
	ErrCancelled      = errors.New("cancelled")
	ErrNotImplemented = errors.New("not implemented")

	// Provider / model errors
	ErrProviderNotAvailable = errors.New("provider not available")
	ErrModelNotFound        = errors.New("model not found")
	ErrQuotaExceeded        = errors.New("quota exceeded")
)
