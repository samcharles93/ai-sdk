# AGENTS.md — AI SDK (Go re-interpretation)

---

## CORE | STRICT — Onion Model Architecture

This project follows the **onion model** (also known as hexagonal / ports & adapters).  
Each layer is responsible for a specific concern and **MUST NOT** know about any layer above it.

Dependency direction: **inward only** — outer layers depend on inner layers, never the reverse.

```
┌──────────────────────────────────────────────┐
│  UI Layer        pkg/ui/                     │  Templ + Datastar components & handlers
│  ─────────────────────────────────────────── │  Knows: services, domain interfaces
│                                              │  Reason: renders AI responses into HTML
├──────────────────────────────────────────────┤
│  Services        pkg/core/                   │  Orchestration: GenerateText, StreamText
│  ─────────────────────────────────────────── │  Knows: domain interfaces (chat.Provider)
│                  pkg/chat/client.go           │  Thin facades over providers
│                  pkg/embed/client.go          │
│                  pkg/image/client.go          │
│                  pkg/speech/client.go         │
│                  pkg/transcribe/client.go     │
├──────────────────────────────────────────────┤
│  Middleware      pkg/middleware/              │  Wraps domain interfaces
│  ─────────────────────────────────────────── │  Knows: domain interfaces
├──────────────────────────────────────────────┤
│  Infrastructure  pkg/registry/                │  Provider registry
│  ─────────────────────────────────────────── │  Knows: domain interfaces
│                  pkg/schema/                  │  JSON Schema builder (standalone)
│                  pkg/util/                    │  Shared utilities (standalone, stdlib only)
├──────────────────────────────────────────────┤
│  Domain          pkg/chat/                    │  Types + Provider interface
│  Interfaces      pkg/embed/                   │  Types + Provider interface
│  ─────────────── pkg/image/                   │  Types + Provider interface
│  (INNERMOST)     pkg/speech/                  │  Types + Provider interface
│                  pkg/transcribe/               │  Types + Provider interface
│                                              │  Knows: NOTHING (stdlib only)
├──────────────────────────────────────────────┤
│  Providers       pkg/provider/deepseek/       │  Implements domain interfaces
│  ─────────────── pkg/provider/gemini/         │  Knows: domain interfaces + HTTP APIs
│                  pkg/provider/ollama/         │
└──────────────────────────────────────────────┘
```

### Dependency Rules (NON-NEGOTIABLE)

1. **Domain packages (`pkg/chat`, `pkg/embed`, etc.)** MUST NOT import any other `pkg/` package. Only `context`, `encoding/json`, `errors`, and other stdlib packages are allowed.

2. **Provider packages (`pkg/provider/*`)** MAY import domain packages (`pkg/chat`, `pkg/embed`) to implement their interfaces. They MUST NOT import `pkg/core`, `pkg/ui`, `pkg/registry`, or `pkg/middleware`.

3. **Core/Services (`pkg/core/`)** MAY import domain packages and their interfaces. It MUST NOT import provider implementations or UI packages. It works strictly against interfaces.

4. **Middleware (`pkg/middleware/`)** MAY import domain packages. It MUST NOT import core, providers, or UI.

5. **Infrastructure (`pkg/registry/`, `pkg/schema/`, `pkg/util/`)**:
   - `registry` — MAY import all domain interface packages. MUST NOT import providers, core, or UI.
   - `schema` — standalone, no pkg/ imports.
   - `util` — standalone, stdlib only.

6. **UI (`pkg/ui/`)** is the outermost layer. It MAY import core, domain interfaces, and registry. It MUST NOT import provider implementations directly. It contains:
   - State management structs (Go equivalents of React hooks like `useChat`)
   - Templ components (`.templ` files)
   - HTTP handlers
   - All UI depends on Datastar for streaming reactivity.

7. **`cmd/`** is the composition root. It wires everything together via dependency injection. It MAY import all packages.

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
cmd/ai-sdk/           # Entrypoint — wires dependencies, starts server
pkg/
  chat/               # Domain: chat types & interface
  embed/              # Domain: embedding types & interface
  image/              # Domain: image generation types & interface
  speech/             # Domain: speech synthesis types & interface
  transcribe/         # Domain: transcription types & interface
  core/               # Services: generateText, streamText orchestration
  middleware/         # Services: provider middleware
  registry/           # Infrastructure: provider registry
  schema/             # Infrastructure: JSON Schema builder
  util/               # Infrastructure: shared utilities
  provider/           # Providers: concrete implementations
    deepseek/
    gemini/
    ollama/
  ui/                 # UI: Templ components & HTTP handlers
    chat/             #   Chat state management
    components/       #   Templ component files (.templ)
    handlers/         #   HTTP handler implementations
```

## References

- Project structure: https://templ.guide/project-structure/project-structure
- AI SDK Core: https://ai-sdk.dev/docs/reference/ai-sdk-core
- AI SDK UI useChat: https://ai-sdk.dev/docs/reference/ai-sdk-core/ui-message
- Datastar: https://data-star.dev
