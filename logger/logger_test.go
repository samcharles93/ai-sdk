package logger

import (
	"log/slog"
	"testing"
)

func TestNoopLogger(t *testing.T) {
	NoopLogger.Info("test")
	NoopLogger.Debug("test")
	NoopLogger.Error("test")
}

func TestSlogAdapter(t *testing.T) {
	l := slog.New(slog.NewTextHandler(&testWriter{}, nil))
	lg := NewSlogLogger(l)
	lg.Info("info")
	lg.Debug("debug")
	lg.Error("err")
}

type testWriter struct{}

func (t *testWriter) Write(p []byte) (int, error) { return len(p), nil }
