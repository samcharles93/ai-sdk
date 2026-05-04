# Production Readiness & Missing Examples

> **Goal**: Close operational gaps (retry, circuit breaker, middleware for all domains) and add missing examples so the AI SDK is production-ready and self-documenting.

---

## Research Synthesis & Architecture Decisions

### ✅ Decision 1: Per-domain middleware types (confirmed by research)

Go-kit, go-micro, and gRPC all use **concrete per-domain middleware types**, not generic `Middleware[T]`. Go's type system can't constrain `T any` by method sets. The pattern:

```go
// Per domain (8 total)
type ChatMiddleware      func(chat.Provider) chat.Provider
type EmbedMiddleware     func(embed.Provider) embed.Provider
type ImageMiddleware     func(image.Provider) image.Provider
// ... etc

// Generic only for Chain (all share func(T) T shape)
func Chain[T any](mws ...Middleware[T]) Middleware[T]
```

### ✅ Decision 2: Struct wrapping pattern (not functional)

Each middleware is a **struct implementing the Provider interface** by embedding `next`, same as existing `TelemetryMiddleware`. This is the pattern from gRPC, go-kit, go-micro.

### ✅ Decision 3: BackoffStrategy interface + RetryableError predicate

From hashicorp/go-retryablehttp + gRPC: backoff is a strategy interface, retry decision is a predicate. Allows pluggable backoff and custom retry logic.

### ✅ Decision 4: gobreaker-compatible state machine

3-state (Closed→Open→HalfOpen→Closed), time-based expiry, counter-based trip threshold. Standard pattern from sony/gobreaker.

### ✅ Decision 5: No go.work needed

Existing `ai-sdk-examples/go.mod` uses `replace github.com/samcharles93/ai-sdk => ../`. No go.work required.

### ✅ Decision 6: Streaming retry — retry only ChatStream/StreamObject creation

Once a stream is established, mid-stream errors from Next() are terminal. Retry middleware only retries the initial stream creation call.

### Provider interface summary (from research)

| Domain | Methods | Has Stream? | Stream interface |
|--------|---------|-------------|------------------|
| chat | Name, Chat, ChatStream | Yes | chat.Stream (Next/Close) |
| object | Name, GenerateObject, StreamObject | Yes | object.ObjectStream (Next/Close) |
| embed | Name, Embed | No | — |
| image | Name, GenerateImage | No | — |
| speech | Name, GenerateSpeech | No | — |
| transcribe | Name, Transcribe | No | — |
| video | Name, GenerateVideo | No | — |
| rerank | Name, Rerank | No | — |

---

## Wave 1 — Generic Middleware Foundation

### 1.1 — Define domain-specific middleware types (7 new + 1 existing)

**Files**: `pkg/middleware/chat.go` (update), `embed.go`, `image.go`, `speech.go`, `transcribe.go`, `video.go`, `rerank.go`, `object.go`

For each of the 8 domains, define a concrete middleware type:
```go
// chat.go — update existing ChatMiddleware, add ChatHooks
type ChatMiddleware      func(chat.Provider) chat.Provider
// embed.go — new
type EmbedMiddleware     func(embed.Provider) embed.Provider
type ImageMiddleware     func(image.Provider) image.Provider
type SpeechMiddleware    func(speech.Provider) speech.Provider
type TranscribeMiddleware func(transcribe.Provider) transcribe.Provider
type VideoMiddleware     func(video.Provider) video.Provider
type RerankMiddleware    func(rerank.Provider) rerank.Provider
type ObjectMiddleware    func(object.Provider) object.Provider
```

### 1.2 — Generic Chain function

**File**: `pkg/middleware/chain.go`

```go
// Chain composes middlewares left-to-right (first = outermost).
func Chain[T any](middlewares ...func(T) T) func(T) T {
    return func(next T) T {
        for i := len(middlewares) - 1; i >= 0; i-- {
            next = middlewares[i](next)
        }
        return next
    }
}
```

Unit test: `TestChain_ComposesLeftToRight` — verifies order with mock providers for 2 domains.

### 1.3 — Per-domain Chain convenience functions
Each domain file includes `ChainXxx`:
```go
func ChainChat(ms ...ChatMiddleware) ChatMiddleware          { return Chain[chat.Provider](ms...) }
func ChainEmbed(ms ...EmbedMiddleware) EmbedMiddleware       { return Chain[embed.Provider](ms...) }
func ChainImage(ms ...ImageMiddleware) ImageMiddleware       { return Chain[image.Provider](ms...) }
func ChainSpeech(ms ...SpeechMiddleware) SpeechMiddleware    { return Chain[speech.Provider](ms...) }
func ChainTranscribe(ms ...TranscribeMiddleware) TranscribeMiddleware { return Chain[transcribe.Provider](ms...) }
func ChainVideo(ms ...VideoMiddleware) VideoMiddleware       { return Chain[video.Provider](ms...) }
func ChainRerank(ms ...RerankMiddleware) RerankMiddleware    { return Chain[rerank.Provider](ms...) }
func ChainObject(ms ...ObjectMiddleware) ObjectMiddleware    { return Chain[object.Provider](ms...) }
```

---

## Wave 2 — Production Middleware: Retry & Circuit Breaker

### 2.1 — Shared retry engine
**File**: `pkg/middleware/retry.go`

Define `RetryConfig` with: MaxAttempts (int).
Define `BackoffStrategy` interface for pluggable backoff:
```go
type BackoffStrategy interface {
    Backoff(attempt int) time.Duration
}

type RetryableError func(error) bool
```

Implement `ExponentialBackoff` strategy:
```go
type ExponentialBackoff struct {
    BaseDelay  time.Duration
    MaxDelay   time.Duration
    Multiplier float64
    Jitter     float64 // 0.0–1.0, applied as 1±Jitter*random
}
```

Core retry loop pattern (per-domain): struct wraps `next Provider`, implements domain interface, retries with context-aware sleep.

Unit tests:
- `TestRetry_RetriesOnTransientError` — retries 3 times, succeeds on 3rd
- `TestRetry_DoesNotRetryAuthError` — 401 returns immediately
- `TestRetry_AbortsOnContextCancel` — cancelled context stops retry loop
- `TestRetry_ExponentialBackoff` — verifies backoff grows by multiplier
- `TestRetry_Jitter` — verifies actual wait differs from exact backoff

### 2.2 — Domain-specific retry constructors
**Files**: `pkg/middleware/retry_chat.go`, `retry_embed.go`, `retry_image.go`, `retry_speech.go`, `retry_transcribe.go`, `retry_video.go`, `retry_rerank.go`, `retry_object.go`

Each file contains a `RetryXxx` constructor returning the domain's middleware type:
```go
func RetryChat(cfg RetryConfig, backoff BackoffStrategy, retryable RetryableError) ChatMiddleware
func RetryEmbed(cfg RetryConfig, backoff BackoffStrategy, retryable RetryableError) EmbedMiddleware
// ... etc
```

### 2.3 — Name-delegating pattern (shared)
Each retry wrapper struct embeds `next Provider` and delegates `Name()`:
```go
func (w *retryChatProvider) Name() string { return w.next.Name() }
```

### 2.4 — Circuit Breaker middleware
**File**: `pkg/middleware/circuitbreaker.go`

3-state machine: CLOSED → OPEN → HALF_OPEN → CLOSED (gobreaker-compatible).

State transitions:
- CLOSED → OPEN: consecutiveFailures >= FailureThreshold
- OPEN → HALF_OPEN: Timeout expires
- HALF_OPEN → CLOSED: consecutiveSuccesses >= SuccessThreshold  
- HALF_OPEN → OPEN: any single failure

```go
type CircuitBreakerConfig struct {
    FailureThreshold uint32
    SuccessThreshold uint32
    Timeout          time.Duration
}
```

Uses `sync.Mutex` for goroutine safety. Counts tracked per-circuit.

Unit tests:
- `TestCircuitBreaker_OpensAfterThreshold` — N consecutive failures → OPEN state
- `TestCircuitBreaker_HalfOpenProbe` — after timeout, one call allowed through
- `TestCircuitBreaker_ClosesAfterSuccess` — successThreshold reached in HALF_OPEN → CLOSED
- `TestCircuitBreaker_RejectsInOpenState` — returns ErrCircuitOpen immediately

### 2.5 — Domain-specific circuit breaker constructors
**Files**: `pkg/middleware/circuitbreaker_chat.go`, etc. (8 files)

Same per-domain pattern as retry. Each returns the domain's middleware type.

---

## Wave 3 — Telemetry Middleware for All Domains

### 3.1 — Generic telemetry middleware
**File**: `pkg/middleware/telemetry_generic.go`

Extends existing `telemetry.go` (Chat only) to all domains using the generic `Middleware[T]` pattern.

```go
func Telemetry[T any](tracer telemetry.Tracer, name string, call func(context.Context, T) (any, error)) Middleware[T]
```

Creates a span before calling the provider, records error on span if call fails, sets attributes (provider name, model if available).

### 3.2 — Domain-specific telemetry constructors
**Files**: `pkg/middleware/telemetry_embed.go`, etc.

---

## Wave 4 — Health Checks

### 4.1 — Provider health check interface
**File**: `pkg/middleware/health.go`

```go
type HealthChecker interface {
    HealthCheck(context.Context) error
}
```

### 4.2 — Health check middleware
```go
func HealthCheck[T HealthChecker](timeout time.Duration) Middleware[T] { ... }
```

Calls `HealthCheck` before each provider call. Returns `ErrProviderUnavailable` if check fails. Providers don't need to implement this — it's optional.

---

## Wave 5 — Missing Examples

### 5.1 — Embedding example
**File**: `ai-sdk-examples/embedding/main.go`

Demonstrates:
- OpenAIGenerateEmbedding with text inputs
- Computing cosine similarity between embeddings
- Vector index operations (add, search)

### 5.2 — Video generation example
**File**: `ai-sdk-examples/video-generation/main.go`

Demonstrates:
- Creating a video from text prompt
- Polling for completion
- Displaying result URL

### 5.3 — Streaming chat example
**File**: `ai-sdk-examples/streaming-chat/main.go`

Demonstrates:
- Streaming text response with real-time output to terminal
- Handling tool calls during stream
- Displaying token usage on finish

### 5.4 — Reranking example
**File**: `ai-sdk-examples/rerank/main.go`

Demonstrates:
- Reranking search results by relevance
- Displaying scores and order changes

### 5.5 — Multi-provider example
**File**: `ai-sdk-examples/multi-provider/main.go`

Demonstrates:
- Registering multiple providers via Registry
- Switching between providers based on model name
- Using different capabilities from different providers (chat from OpenAI, embed from Cohere)

### 5.6 — Production setup example
**File**: `ai-sdk-examples/production-setup/main.go`

Demonstrates:
- Full middleware chain: telemetry → retry → circuit breaker → provider
- Multiple providers with health checks
- Graceful shutdown
- Structured logging

### 5.7 — Examples README
**File**: `ai-sdk-examples/README.md`

Documents each example with: Purpose, Required env vars, Command to run, Expected output.

Note: **No go.work needed** — existing `ai-sdk-examples/go.mod` uses:
```
require github.com/samcharles93/ai-sdk v0.0.0
replace github.com/samcharles93/ai-sdk => ../
```
New examples must follow this convention — each gets its own directory with a single `main.go` file that can be run via `cd ai-sdk-examples && go run ./<example-name>/`.

---

## Wave 6 — Domain Tests

### 6.1 — Core wrapper tests for untested domains
**Files**: `pkg/core/image_impl_test.go`, `speech_impl_test.go`, `object_impl_test.go`, `video_impl_test.go`

Each test file covers:
- No provider → error
- Valid request → delegates to mock provider
- Provider error → propagated
- Context cancelled → aborted

### 6.2 — Middleware integration tests
**File**: `pkg/middleware/integration_test.go`

Tests middleware chain composition:
- Retry + Telemetry + Chat provider → spans created, retries logged
- CircuitBreaker + Retry → breaker tracks retry failures

---

## Wave 7 — Final Verification

### 7.1 — Build & Test
- `go build ./...` — all packages compile
- `go vet ./...` — no vet warnings
- `go test ./...` — all tests pass (existing + new)

### 7.2 — Example builds
- `cd ai-sdk-examples && go build ./...` — all examples compile

### 7.3 — Lint
- Run `gopls_go_diagnostics` on all changed packages

---

## Task Summary

| Wave | Tasks | New/Modified Files |
|------|-------|--------------------|
| Wave 1 — Middleware Types | 3 | 8 type files + 1 chain.go + 1 chain_test.go (10 files) |
| Wave 2 — Retry & CB | 5 | retry.go, retry_test.go, circuitbreaker.go, circuitbreaker_test.go + 16 domain constructors (20 files) |
| Wave 3 — Telemetry | 2 | telemetry_generic.go, telemetry_test.go extend + 7 domain constructors (9 files) |
| Wave 4 — Health | 2 | health.go, health_test.go (2 files) |
| Wave 5 — Examples | 7 | 6 example main.gos + 1 README (7 files) |
| Wave 6 — Tests | 2 | 4 core test files + 1 integration test (5 files) |
| Wave 7 — Verify | 3 | Build, test, lint |

**Total**: ~24 tasks, ~53 new/modified files, ~3,000 lines of code
