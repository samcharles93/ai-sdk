# AI SDK (Go)

A provider-agnostic AI SDK for Go. Chat,
embeddings, image generation, speech, transcription, structured object
generation, video, and reranking — all through a unified, type-safe,
interface-driven API.

```
go get github.com/samcharles93/ai-sdk
```

---

## Overview

This SDK provides a clean, composable way to work with AI providers in Go.
Instead of vendor-specific clients scattered through your codebase, you program
against domain interfaces in `chat`, `embed`, `image`, etc.
Providers are injected at the composition root — your business logic never
imports a provider directly.

### Features

- **Unified interface** across 9 domains: chat, embedding, image generation,
  speech synthesis, transcription, object generation, video generation,
  reranking, and music generation
- **Pluggable providers** — swap implementations at the wiring layer
- **Tool use and streaming** built into the chat domain
- **Agent loops** built on top of `StreamText` — tool-calling agent with
  streaming events
- **Lifecycle hooks and panic containment** around model and tool execution
- **Caller-controlled cancellation** — the SDK does not impose hidden request
  deadlines when no timeout is configured
- **Middleware** — compose logging, telemetry, and circuit-breaker layers around
  providers
- **Runtime layer** — resolve `provider/model` references dynamically from a
  models.dev catalog
- **UI layer** — Templ + Datastar components for real-time reactive chat UIs
- **Strict onion architecture** — domain packages import nothing outside stdlib

### Supported Providers

| Provider      | Package                     | Chat | Embed | Image | Speech | Transcribe | Object | Rerank | Video | Music |
| ------------- | --------------------------- | ---- | ----- | ----- | ------ | ---------- | ------ | ------ | ----- | ----- |
| OpenAI        | `provider/openai`           | ✅   | —     | —     | ✅     | ✅         | —      | —      | —     | —     |
| OpenAIObject  | `provider/openaiobject`     | —    | —     | —     | —      | —          | ✅     | —      | —     | —     |
| Anthropic     | `provider/anthropic`        | ✅   | —     | —     | —      | —          | —      | —      | —     | —     |
| Azure         | `provider/azure`            | ✅   | ✅    | ✅    | —      | —          | —      | —      | —     | —     |
| Cohere        | `provider/cohere`           | ✅   | ✅    | —     | —      | —          | —      | ✅     | —     | —     |
| DeepSeek      | `provider/deepseek`         | ✅   | —     | —     | —      | —          | —      | —      | —     | —     |
| Gemini        | `provider/gemini`           | ✅   | ✅    | —     | —      | —          | —      | —      | —     | —     |
| Groq          | `provider/groq`             | ✅   | —     | —     | ✅     | ✅         | —      | —      | —     | —     |
| MiniMax       | `provider/minimax`          | ✅   | —     | ✅    | ✅     | —          | —      | —      | ✅     | ✅    |
| Mistral       | `provider/mistral`          | ✅   | ✅    | —     | —      | —          | —      | —      | —     | —     |
| Ollama        | `provider/ollama`           | ✅   | ✅    | —     | —      | —          | —      | —      | —     | —     |
| Perplexity    | `provider/perplexity`       | ✅   | —     | —     | —      | —          | —      | —      | —     | —     |
| TogetherAI    | `provider/togetherai`       | ✅   | —     | ✅    | —      | —          | —      | ✅     | —     | —     |
| xAI           | `provider/xai`              | ✅   | —     | ✅    | —      | —          | —      | —      | ✅     | —     |

Notes:
- TogetherAI chat routes through the OpenAI-compatible path; its native provider implements image + rerank.
- Azure chat/embed/image come from a single `provider/azure` provider.
- xAI implements chat + image + video from a single `*Provider`.
- `openaiobject` is a standalone object-generation backend (OpenAI Chat Completions with `response_format.json_schema`).
- MiniMax is a reference provider not published in the models.dev catalog, so it resolves through the runtime only when an operator supplies a provider config (class `minimax`) with model IDs and an API key — the config-driven custom-provider pattern it demonstrates. `runtime.RegisterClass` is the escape hatch for a provider not shipped as a built-in class.
- MiniMax chat routes through the OpenAI-compatible path (its text API is OpenAI-compatible); its native provider implements image + speech + music + video across four domain interfaces.
- Groq implements speech synthesis over its OpenAI-compatible `/openai/v1/audio/speech` endpoint. The current Canopy Labs Orpheus v1 models (English and Arabic) are wav-only and require a voice, so the SDK defaults `Format` to `wav`, rejects other formats before the call, and rejects a voice-less request rather than guessing. Orpheus also needs a one-time org-level terms acceptance in the Groq console.
- Any self-hosted OpenAI-compatible TTS server (Kokoro-FastAPI, speaches, openedai-speech, LocalAI) resolves through the generic `openai-compatible` class with `base_url` set to the server and `auth.type: none` (for keyless servers). Switching TTS backends is then config-only, not a code change. The generic path has no default voice: set `Voice` on each request, or configure `speech_default_voice` / `speech_default_format` in the provider `options` (per-model `extra` wins over provider `options`).
- Speech resolution is metadata-gated: a model whose published modalities (or an explicit per-model `capabilities` list in the runtime config) exclude speech output is refused with `ErrCapabilityNotSupported` before any request is sent. Models with no published metadata are attempted best-effort, so a chat-only endpoint can still surface a provider-side error; declare `capabilities: ["speech"]` to make the intent explicit.
- Speech requests carry typed `Instructions` and `SampleRate` fields plus `ProviderOptions` (bucket keyed by provider name, e.g. `{"openai": {"instructions": "...", "speed": 1.2}}`). Request fields win over provider options, and a provider that does not support a field rejects the request with `ErrInvalidRequest` instead of silently dropping it. OpenAI's headline TTS model is `gpt-4o-mini-tts`, which supports `instructions`; OpenAI recommends the `marin` or `cedar` voices, and `tts-1`/`tts-1-hd` support neither instructions nor SSE streaming.

---

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/samcharles93/ai-sdk/chat"
    "github.com/samcharles93/ai-sdk/provider/openai"
)

func main() {
    provider, err := openai.New(openai.Config{
        APIKey: os.Getenv("OPENAI_API_KEY"),
    })
    if err != nil {
        log.Fatal(err)
    }

    resp, err := provider.Chat(context.Background(), chat.Request{
        Model:    "gpt-5.4",
        Messages: []chat.Message{
            {Role: chat.RoleUser, Content: "Hello!"},
        },
    })
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp.Content)
}
```

### Streaming

```go
stream, err := provider.ChatStream(ctx, chat.Request{
    Model:    "gpt-5.4",
    Messages: []chat.Message{{Role: chat.RoleUser, Content: "Tell me a story"}},
})
defer stream.Close()

for {
    chunk, err := stream.Next(ctx)
    if err == io.EOF {
        break
    }
    if err != nil {
        log.Fatal(err)
    }
    fmt.Print(chunk.Delta)
}
```

### With Tool Use

```go
resp, err := provider.Chat(ctx, chat.Request{
    Model:    "gpt-5.4",
    Messages: []chat.Message{{Role: chat.RoleUser, Content: "What's the weather in London?"}},
    Tools: []chat.Tool{{
        Name:        "get_weather",
        Description: "Get current weather for a location",
        Parameters:  json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}`),
    }},
    ToolChoice: &chat.ToolChoice{Type: chat.ToolChoiceAuto},
})
```

### Runtime Resolution

The runtime resolves `provider/model` references and exposes the same entry
points for chat, speech, and transcription. With no explicit provider timeout,
the caller's context owns the lifetime of the operation.

```go
runtime.RegisterBuiltinClasses()
rt := runtime.NewRuntime(runtime.Config{
    Providers: map[string]runtime.ProviderConfig{
        "openai": {
            ID: "openai", Class: "openai",
            Auth: runtime.AuthConfig{Type: runtime.AuthTypeAPIKey, APIKey: os.Getenv("OPENAI_API_KEY")},
        },
    },
})

audio, err := rt.Speech(ctx, "openai/gpt-4o-mini-tts", speech.GenerateSpeechRequest{
    Text: "Hello from Go",
})
transcript, err := rt.Transcribe(ctx, "openai/whisper-1", transcribe.TranscribeRequest{
    Audio: audio.Audio,
})
fmt.Println(transcript.Text)
```

`core.GenerateOptions` also accepts `ToolHooks` for policy and output handling,
and `ModelHooks` for balanced lifecycle telemetry around every provider call.

---

## Architecture

The SDK follows a strict **onion architecture** — dependencies flow inward.
Outer layers depend on inner layers, never the reverse.

```
┌──────────────────────────────────────────┐
│  UI Layer        ui/                 │  Templ + Datastar
│  Runtime         runtime/            │  Provider resolution
│  Agent           agent/              │  Tool-loop agent
│  Core/Services   core/               │  Orchestration facades
│  Middleware      middleware/          │  Provider wrappers
│  Infrastructure  registry/, schema/, │
│                  util/, upload/, error/,  │
│                  logger/, telemetry/,     │
│                  prompt/                  │
│  Domain          chat/, embed/,       │  Interfaces + types (stdlib only)
│  Providers       provider/*/          │  Wire implementations
└──────────────────────────────────────────┘
```

### Key rules

- **Domain packages** (`chat`, `embed`, etc.) import only stdlib
- **Provider packages** implement domain interfaces; import only domain
  packages + stdlib + HTTP
- **Core/Services** orchestrate providers through interfaces — no provider
  import
- **Runtime** resolves `provider/model` strings into working provider instances
- No global state, no `init()` wiring, no package-level singletons

---

## Examples

Run examples from the repo root:

```bash
# Chat
OPENAI_API_KEY=sk-... go run ./ai-sdk-examples/openai-chat/

# Agent with tool use
ANTHROPIC_API_KEY=sk-ant-... go run ./ai-sdk-examples/anthropic-agent/ "What's the weather in London?"

# Object generation
go run ./ai-sdk-examples/object-generation/

# Image generation
AZURE_API_KEY=... go run ./ai-sdk-examples/image-generation/

# Transcription
go run ./ai-sdk-examples/speech-to-text/
```

Full example list at `ai-sdk-examples/README.md`.

---

## Development

### Prerequisites

- Go 1.26+
- [golangci-lint](https://golangci-lint.run/) (optional, for linting)

### Commands

```bash
task test       # run all tests
task test:race  # run the suite with the race detector
task check      # format, vet, staticcheck, lint, dead-code check, and tests
```

The project uses `gofumpt` for formatting and `golangci-lint` with `govet`,
`staticcheck`, `unused`, `nilerr`, and `misspell` enabled.

---

## License

Apache-2.0 — see [LICENSE](LICENSE).
