package logger

import (
	"log/slog"
)

// Logger is a minimal structured logging abstraction used across the
// project. Implementations should keep behaviour lightweight so tests
// and libraries can substitute no-op or test loggers.
type Logger interface {
	Info(msg string, attrs ...any)
	Error(msg string, attrs ...any)
	Debug(msg string, attrs ...any)
}

// slogLogger adapts stdlib slog.Logger to our Logger interface.
type slogLogger struct{ l *slog.Logger }

func (s *slogLogger) Info(msg string, attrs ...any)  { s.l.Info(msg, attrs...) }
func (s *slogLogger) Error(msg string, attrs ...any) { s.l.Error(msg, attrs...) }
func (s *slogLogger) Debug(msg string, attrs ...any) { s.l.Debug(msg, attrs...) }

// NewSlogLogger returns a Logger backed by the provided *slog.Logger.
func NewSlogLogger(l *slog.Logger) Logger {
	if l == nil {
		return &noopLogger{}
	}
	return &slogLogger{l: l}
}

// NoopLogger returns a logger that performs no operations. Useful for
// tests where logging side-effects are undesirable.
type noopLogger struct{}

func (n *noopLogger) Info(msg string, attrs ...any)  {}
func (n *noopLogger) Error(msg string, attrs ...any) {}
func (n *noopLogger) Debug(msg string, attrs ...any) {}

// For convenience expose a singleton no-op value
var NoopLogger Logger = &noopLogger{}
