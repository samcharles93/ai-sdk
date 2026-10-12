# telemetry

```go
type Span interface {
    End()
    SetAttribute(key, value string)
    RecordError(err error)
}
type Tracer interface {
    Start(ctx context.Context, name string) (context.Context, Span)
}
```

`NoopSpan` / `NoopTracer` are no-ops; `DefaultTracer` is the package fallback.
