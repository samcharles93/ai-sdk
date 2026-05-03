// Package telemetry defines minimal tracing interfaces used across the
// ai-sdk codebase. The package is intentionally small and dependency-free —
// concrete bridging implementations (for example OpenTelemetry) live in the
// middleware/ or provider-specific packages.
package telemetry

// This package contains only type definitions and noop implementations so
// consumers can accept a Tracer without taking a hard dependency on any
// instrumentation library.
