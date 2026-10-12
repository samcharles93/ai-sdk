# logger

- `Logger` interface — `Info(msg, attrs...), Error(msg, attrs...), Debug(msg, attrs...)`
- `NewSlogLogger(l *slog.Logger) Logger` — adapts stdlib slog
- `NoopLogger` — no-op implementation for tests
