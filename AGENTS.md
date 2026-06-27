# AGENTS.md — AI SDK (Go re-interpretation)

---

## CORE | STRICT — Onion Model Architecture

This project follows the **onion model** (also known as hexagonal / ports & adapters).  
Each layer is responsible for a specific concern and **MUST NOT** know about any layer above it.

Dependency direction: **inward only** — outer layers depend on inner layers, never the reverse.

```
┌──────────────────────────────────────────────────┐
│  UI Layer        pkg/ui/                         │  Templ + Datastar components & handlers
│  ─────────────────────────────────────────────── │  Knows: services, domain interfaces
│                  pkg/uimessage/                   │  UI message protocol (SSE, chunks)
│                  pkg/uimessage/sse/               │  SSE writer, stream processor
├──────────────────────────────────────────────────┤
│  Runtime         pkg/runtime/                     │  Provider-agnostic model resolution:
│  ─────────────────────────────────────────────── │    models.dev catalog, pluggable
│                                                   │    ProviderClass registry, Chat/Embed
│                                                   │    entry points.
│                                                   │  Knows: catalog, classes, core, domains
├──────────────────────────────────────────────────┤
│  Agent           pkg/agent/                       │  Tool-loop agent over StreamText
│  ─────────────────────────────────────────────── │  Knows: core, domain interfaces
├──────────────────────────────────────────────────┤
│  Services        pkg/core/                       │  Orchestration: GenerateText, StreamText,
│  ─────────────────────────────────────────────── │    GenerateObject, GenerateImage,
│                  pkg/chat/client.go               │    GenerateSpeech
│                  pkg/embed/client.go              │  Knows: domain interfaces (Provider)
│                  pkg/image/client.go              │  Thin facades over providers
│                  pkg/speech/client.go             │
│                  pkg/transcribe/client.go         │
│                  pkg/object/client.go             │
│                  pkg/video/client.go              │
│                  pkg/rerank/client.go             │
├──────────────────────────────────────────────────┤
│  Middleware      pkg/middleware/                  │  Wraps domain interfaces
│  ─────────────────────────────────────────────── │  Knows: domain, telemetry interfaces
├──────────────────────────────────────────────────┤
│  Infrastructure  pkg/registry/                    │  Provider registry
│  ─────────────────────────────────────────────── │  Knows: domain interfaces
│                  pkg/schema/                      │  JSON Schema builder (standalone)
│                  pkg/util/                        │  Prompt helpers, tokenizer (stdlib only)
│                  pkg/upload/                      │  Multipart form parsing
│                  pkg/error/                       │  Sentinel errors (stdlib only)
│                  pkg/logger/                      │  Structured logging abstraction
│                  pkg/telemetry/                   │  OTel-compatible tracing interfaces
│                  pkg/prompt/                      │  Prompt manager (standalone)
├──────────────────────────────────────────────────┤
│  Domain          pkg/chat/                        │  Chat types + Provider interface
│  Interfaces      pkg/embed/                       │  Embedding types + Provider
│  ─────────────── pkg/image/                       │  Image gen types + Provider
│  (INNERMOST)     pkg/speech/                      │  Speech synthesis types + Provider
│                  pkg/transcribe/                   │  Transcription types + Provider
│                  pkg/object/                      │  Object gen types + Provider
│                  pkg/video/                       │  Video gen types + Provider
│                  pkg/rerank/                      │  Reranking types + Provider
│                                                  │  Knows: NOTHING (stdlib only)
├──────────────────────────────────────────────────┤
│  Providers       pkg/provider/anthropic/          │  Implements domain interfaces
│  ─────────────── pkg/provider/azure/              │  Knows: domain interfaces + HTTP APIs
│                  pkg/provider/cohere/              │
│                  pkg/provider/deepseek/            │
│                  pkg/provider/gemini/              │
│                  pkg/provider/groq/                │
│                  pkg/provider/mistral/             │
│                  pkg/provider/ollama/              │
│                  pkg/provider/openai/              │
│                  pkg/provider/perplexity/          │
│                  pkg/provider/togetherai/          │
│                  pkg/provider/xai/                 │
└──────────────────────────────────────────────────┘
```

### Dependency Rules (NON-NEGOTIABLE)

1. **Domain packages (`pkg/chat`, `pkg/embed`, etc.)** MUST NOT import any other `pkg/` package. Only `context`, `encoding/json`, `errors`, and other stdlib packages are allowed.

2. **Provider packages (`pkg/provider/*`)** MAY import domain packages (`pkg/chat`, `pkg/embed`) to implement their interfaces. They MUST NOT import `pkg/core`, `pkg/ui`, `pkg/registry`, or `pkg/middleware`.

3. **Core/Services (`pkg/core/`)** MAY import domain packages and their interfaces. It MUST NOT import provider implementations or UI packages. It works strictly against interfaces.

4. **Runtime (`pkg/runtime/`)** MAY import domain packages, provider implementations, and core. It is the provider-resolution and model-discovery layer. It MUST NOT be imported by providers, domain packages, or core.

5. **Middleware (`pkg/middleware/`)** MAY import domain packages. It MUST NOT import core, providers, UI, or runtime.

6. **Infrastructure (`pkg/registry/`, `pkg/schema/`, `pkg/util/`)**:
   - `registry` — MAY import all domain interface packages. MUST NOT import providers, core, or UI.
   - `schema` — standalone, no pkg/ imports.
   - `util` — standalone, stdlib only.

7. **UI (`pkg/ui/`)** is the outermost layer. It MAY import core, domain interfaces, registry, and runtime. It MUST NOT import provider implementations directly. It contains:
   - State management structs (Go equivalents of React hooks like `useChat`)
   - Templ components (`.templ` files)
   - HTTP handlers
   - All UI depends on Datastar for streaming reactivity.

8. **`cmd/`** is the composition root. It wires everything together via dependency injection. It MAY import all packages.

### Package Conventions

Every domain package MUST contain:
- `doc.go` — Package-level documentation
- `types.go` — Request/Response types
- `provider.go` — Provider interface
- `client.go` — Thin Client facade with nil-guard
- `errors.go` — Sentinel errors

### Interface Ownership

Following [Go's interface conventions](https://go.dev/wiki/CodeReviewComments#interfaces):
- **Consumers define interfaces they need**, not producers.
- Domain packages define the `Provider` interface because they are consumed by higher layers.
- HTTP handlers define service interfaces, not the other way around.

### Dependency Injection

- Every struct has a `New` constructor accepting its dependencies as interfaces.
- No global state. No package-level singletons. No `init()` for wiring.
- Changing a `New` signature produces compile-time errors showing all affected call sites.

---

## UI Layer — Templ + Datastar

The AI SDK UI layer ports the concepts from the JS AI SDK UI libraries (`useChat`, `Chat`, etc.)  
to server-side Go using [Templ](https://templ.guide) for HTML templating and [Datastar](https://data-star.dev)  
for real-time streaming reactivity via SSE.

### Key Concepts (ported from JS)

| JS Concept            | Go Equivalent                            |
|-----------------------|------------------------------------------|
| `useChat()` hook      | `chat.Chat` struct with methods          |
| `UIMessage`           | `chat.UIMessage` struct                  |
| `ChatTransport`       | `chat.Transport` interface               |
| `sendMessage()`       | `Chat.Send(ctx, msg)` method             |
| `status` (reactive)   | Datastar signals on the DOM              |
| `onToolCall`          | Callback registered on `ChatOptions`     |
| `addToolOutput()`     | `Chat.AddToolOutput(ctx, opts)` method   |
| `onFinish`            | Callback registered on `ChatOptions`     |

### Component Strategy

Templ components from the JS component libraries are ported as `.templ` files  
using Datastar attributes for reactivity:
- `data-signals` for local state
- `data-on-*` for event handling
- SSE streaming for real-time text deltas from `streamText`

---

## File Organization

```
ai-sdk-examples/            # Example programs demonstrating SDK usage
  openai-chat/              #   Simple chat CLI with OpenAI
  anthropic-agent/          #   Agent with tool-use and streaming
  object-generation/        #   Structured object generation
  speech-to-text/           #   Audio transcription example
  image-generation/         #   Image generation example
cmd/ai-sdk/                 # Entrypoint — wires dependencies, starts server
pkg/
  chat/                     # Domain: chat types & interface
  embed/                    # Domain: embedding types & interface
  image/                    # Domain: image generation types & interface
  speech/                   # Domain: speech synthesis types & interface
  transcribe/               # Domain: transcription types & interface
  object/                   # Domain: structured object generation types & interface
  video/                    # Domain: video generation types & interface
  rerank/                   # Domain: reranking types & interface
  core/                     # Services: GenerateText, StreamText orchestration
  agent/                    # Agent: tool-loop agent over StreamText
  middleware/               # Middleware: wraps domain interfaces (logging, telemetry)
  registry/                 # Infrastructure: provider registry
  schema/                   # Infrastructure: JSON Schema builder
  util/                     # Infrastructure: prompt helpers, tokeniser
  upload/                   # Infrastructure: multipart form parsing
  error/                    # Infrastructure: sentinel errors
  logger/                   # Infrastructure: structured logging abstraction
  telemetry/                # Infrastructure: OTel-compatible tracing interfaces
  prompt/                   # Infrastructure: prompt manager
  uimessage/                # UI: message protocol (chunks, SSE encoding)
    sse/                    #   SSE writer and stream processing
  provider/                 # Providers: concrete implementations
    anthropic/
    azure/
    cohere/
    deepseek/
    gemini/
    groq/
    mistral/
    ollama/
    openai/
    perplexity/
    togetherai/
    xai/
  ui/                       # UI: Templ components & HTTP handlers
    chat/                   #   Chat state management
    components/             #   Templ component files (.templ)
    handlers/               #   HTTP handler implementations
```

## Provider Ecosystem

| Provider      | Package                        | Chat | Embed | Image | Speech | Transcribe | Object | Rerank | Video |
|---------------|--------------------------------|------|-------|-------|--------|------------|--------|--------|-------|
| OpenAI        | `pkg/provider/openai`          | ✅   | —     | —     | —      | —          | —      | —      | —     |
| Anthropic     | `pkg/provider/anthropic`       | ✅   | —     | —     | —      | —          | —      | —      | —     |
| Azure         | `pkg/provider/azure`           | ✅   | ✅    | ✅    | —      | —          | —      | —      | —     |
| Cohere        | `pkg/provider/cohere`          | ✅   | ✅    | —     | —      | —          | —      | ✅     | —     |
| DeepSeek      | `pkg/provider/deepseek`        | ✅   | —     | —     | —      | —          | —      | —      | —     |
| Gemini        | `pkg/provider/gemini`          | ✅   | ✅    | —     | —      | —          | —      | —      | —     |
| Groq          | `pkg/provider/groq`            | ✅   | —     | —     | —      | —          | —      | —      | —     |
| Mistral       | `pkg/provider/mistral`         | ✅   | ✅    | —     | —      | —          | —      | —      | —     |
| Ollama        | `pkg/provider/ollama`          | ✅   | ✅    | —     | —      | —          | —      | —      | —     |
| Perplexity    | `pkg/provider/perplexity`      | ✅   | —     | —     | —      | —          | —      | —      | —     |
| TogetherAI    | `pkg/provider/togetherai`      | ✅   | ✅    | ✅    | —      | —          | —      | —      | —     |
| xAI           | `pkg/provider/xai`             | ✅   | —     | —     | —      | —          | —      | —      | —     |

## New Package Documentation

### `pkg/runtime/` — AI Provider Runtime

The runtime layer resolves model references like `openai/gpt-4o` into working
provider instances. It is designed for applications (such as `tau`) that
want to consume AI providers without hardcoding every implementation.

```
pkg/runtime/
  doc.go            Package-level documentation
  provider_class.go ProviderClass interface + class registry
  catalog.go        models.dev catalog loader + merge/overrides
  config.go         Declarative runtime configuration
  runtime.go        Runtime: Chat, ChatStream, provider resolution
  builtin.go        Built-in classes (openai-compatible, openai, anthropic, ...)
```

**Key abstractions:**

- `ProviderClass` — a factory that turns a `ProviderConfig` into a
  `ProviderSet` of domain providers. Built-in classes include
  `openai-compatible` (any OpenAI-compatible endpoint) and the known
  models.dev npm mappings (`openai`, `anthropic`, `groq`, ...).
- `Catalog` — loads `https://models.dev/api.json`, merges overrides,
  and exposes provider/model metadata.
- `Runtime` — public entry point: `Chat(ctx, "provider/model", opts)` and
  `ChatStream(ctx, "provider/model", opts)`.

**Extensibility:**

```go
runtime.RegisterClass(myCustomClass{})
```

Custom classes can perform arbitrary setup (discovery, auth exchange,
header injection) before returning domain providers. This is the
escape hatch for providers like OpenShift MaaS that are not directly
covered by the built-in classes.

### `pkg/object/` — Object Generation Domain

The object generation domain provides types and interfaces for structured
JSON output from language models. It mirrors the AI SDK's `generateObject`
function.

```
pkg/object/
  client.go           Thin Client facade with nil-guard
  doc.go              Package-level documentation
  errors.go           Sentinel errors (ErrNoProvider, ErrInvalidRequest)
  provider.go         Provider interface (GenerateObject method)
  provider_options.go Provider-specific options helpers
  types.go            Request, Response, Object, ObjectResult types
```

**Key types:**
- `Provider` interface — `GenerateObject(ctx, req) (ObjectResult, error)`
- `Request` — Model, Prompt, MaxTokens, ProviderOptions
- `Response` — ID, Model, Object, Warnings
- `ObjectResult` — type alias for `any`; providers return concrete types

**Usage via core:**
```go
result, err := core.GenerateObject(ctx, provider, objRequest)
```

### `pkg/video/` — Video Generation Domain

Types and interfaces for video generation from text prompts.

```
pkg/video/
  client.go           Thin Client facade with nil-guard
  doc.go              Package-level documentation
  errors.go           Sentinel errors
  provider.go         Provider interface (GenerateVideo method)
  types.go            GenerateVideoRequest, GenerateVideoResponse, VideoResult
```

**Key types:**
- `GenerateVideoRequest` — Model, Prompt, Duration, Resolution, FrameRate
- `GenerateVideoResponse` — Videos ([]VideoResult), Warnings
- `VideoResult` — Data, URL, MediaType

### `pkg/agent/` — Agent Orchestration

The agent package provides a tool-loop agent that orchestrates multi-step
reasoning and tool execution over `core.StreamText`.

```
pkg/agent/
  agent.go            Agent struct, StreamEvent types, translate()
  agent_impl.go       RunAgent function (convenience API)
  doc.go              Package documentation with usage examples
```

**Key concepts:**
- `Agent` struct — Provider, Model, System, Tools, MaxSteps, Temperature, MaxTokens
- `Agent.Run(ctx, prompt)` — returns `<-chan StreamEvent`
- `RunAgent(ctx, provider, prompt, tools, maxSteps)` — convenience function
- `StreamEvent` — Type-based event dispatch (TextDelta, ToolCall, ToolResult, etc.)

**Event system:**
```go
switch ev.Type {
case agent.EventTextDelta:   // streaming text
case agent.EventToolCall:    // tool invocation requested
case agent.EventToolResult:  // tool execution complete
case agent.EventFinish:      // generation complete
case agent.EventError:       // stream error
case agent.EventAbort:       // context cancelled
}
```

The agent does NOT execute tools itself — `core.StreamText` handles the
full tool loop internally. The agent concentrates on event translation
and lifecycle management.

### `pkg/upload/` — File Upload Utilities

Parses multipart form data and provides file type detection.

```
pkg/upload/
  doc.go              Package-level documentation
  skill.go            Skill-specific upload helpers
  upload.go           ParseMultipartForm, DetectMediaType, ToBase64
  upload_test.go      Tests
```

**Key functions:**

- `ParseMultipartForm(r *http.Request, maxMemory int64) ([]File, error)`
- `DetectMediaType(data []byte) string` — PNG, JPEG, GIF, PDF detection
- `ToBase64(f File) string`

**File type:**

```go
type File struct {
    Name      string
    Data      []byte
    MediaType string
    Size      int64
}
```

### `pkg/util/` — Prompt Helpers and Token Counting

Shared utilities for prompt construction and token estimation.

```tree
pkg/util/
  doc.go              Package-level documentation
  id.go               ID generation
  prompt.go           FormatMessages, SystemPrompt, UserPrompt, etc.
  prompt_test.go      Tests
  stream.go           Stream utilities
  tokenizer.go        Token counting helpers
  tokenizer_test.go   Tests
```

**Prompt construction:**
```go
util.SystemPrompt("You are a helpful assistant.")
util.UserPrompt("What is the weather?")
util.AssistantPrompt("Let me check that for you.")
util.ToolResultMessage(callID, result)
util.FormatMessages(messages) // human-readable formatting
```

### `pkg/error/` — Sentinel Errors

Package-level sentinel error values for use across the project.

```
pkg/error/
  errors.go           Sentinel error variables
  errors_test.go      Tests
```

**Sentinel errors:**
```go
ErrInvalidInput      = errors.New("invalid input")
ErrTimeout           = errors.New("timeout")
ErrCancelled         = errors.New("cancelled")
ErrNotImplemented    = errors.New("not implemented")
ErrProviderNotAvailable = errors.New("provider not available")
ErrModelNotFound     = errors.New("model not found")
ErrQuotaExceeded     = errors.New("quota exceeded")
```

### `pkg/logger/` — Structured Logging

Minimal structured logging abstraction. Adaptable to `log/slog`.

```
pkg/logger/
  logger.go           Logger interface, slogLogger adapter, NoopLogger
  logger_test.go      Tests
```

**Key types:**
- `Logger` interface — `Info(msg, attrs...), Error(msg, attrs...), Debug(msg, attrs...)`
- `NewSlogLogger(l *slog.Logger) Logger` — adapts stdlib slog
- `NoopLogger` — no-op implementation for tests

### `pkg/telemetry/` — OpenTelemetry-Compatible Tracing

Minimal tracing interfaces compatible with OpenTelemetry conventions.

```
pkg/telemetry/
  doc.go              Package-level documentation
  telemetry.go        Span, Tracer interfaces, NoopSpan, NoopTracer
```

**Key interfaces:**
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

- `NoopSpan` / `NoopTracer` — zero-cost no-op implementations
- `DefaultTracer` — package-level `NoopTracer{}` fallback

### `pkg/middleware/` — Provider Middleware

Middleware layer wrapping domain Provider interfaces. Supports composition
via `Chain()`.

```
pkg/middleware/
  doc.go              Package-level documentation
  middleware.go       ChatMiddleware type, ChatRequestHook, ChatResponseHook, Chain()
  telemetry.go        TelemetryMiddleware (spans Chat/ChatStream calls)
  telemetry_test.go   Tests
```

**Key patterns:**
- `ChatMiddleware func(next chat.Provider) chat.Provider`
- `Chain(middlewares ...ChatMiddleware) ChatMiddleware` — composes left-to-right
- `TelemetryMiddleware` — wraps provider with OTel spans for Chat and ChatStream
- `ChatRequestHook` / `ChatResponseHook` — interception points

### `pkg/uimessage/sse/` — SSE Streaming

Server-Sent Events wire format for the AI SDK UI message stream protocol.

```
pkg/uimessage/sse/
  sse_test.go         Tests
  transform.go        Core text-stream to chunk channel adaptation
  writer.go           SSE Writer, Headers, Pipe
```

**Key components:**
- `Writer` — streams `uimessage.Chunk` values as SSE `data:` events
- `NewWriter(rw http.ResponseWriter)` — applies headers, flushes automatically
- `Headers` — canonical AI SDK UI stream headers (`X-Vercel-Ai-Ui-Message-Stream: v1`)
- `Pipe(ctx, src, w)` — drains chunk channel into SSE writer
- `FromTextStream` — adapts core text stream into UI message chunks

## Examples

Example programs demonstrating SDK usage live in `ai-sdk-examples/`:

| Example                | Description                                        |
|------------------------|----------------------------------------------------|
| `openai-chat/`         | Interactive chat CLI using OpenAI provider         |
| `anthropic-agent/`     | Agent with mock weather tool and streaming output  |
| `object-generation/`   | Structured object generation API pattern           |
| `speech-to-text/`      | Audio transcription API pattern                    |
| `image-generation/`    | Image generation API pattern (Azure, TogetherAI)   |

Run examples from the workspace root:
```bash
# Openai chat
OPENAI_API_KEY=sk-... go run ./ai-sdk-examples/openai-chat/

# Anthropic agent with tool use
ANTHROPIC_API_KEY=sk-ant-... go run ./ai-sdk-examples/anthropic-agent/ "What is the weather in London?"

# Informational examples (no API key needed)
go run ./ai-sdk-examples/object-generation/
go run ./ai-sdk-examples/speech-to-text/
go run ./ai-sdk-examples/image-generation/
```

## References

- Project structure: https://templ.guide/project-structure/project-structure
- AI SDK Core: https://ai-sdk.dev/docs/reference/ai-sdk-core
- AI SDK UI useChat: https://ai-sdk.dev/docs/reference/ai-sdk-core/ui-message
- Datastar: https://data-star.dev
