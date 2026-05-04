# AI SDK Examples

Example programs demonstrating the Go AI SDK.

## Examples

| Name | Description | Required Env Vars | Run Command |
|------|-------------|-------------------|-------------|
| openai-chat | Interactive chat with OpenAI models via `core.GenerateText` | `OPENAI_API_KEY` | `go run ./ai-sdk-examples/openai-chat/` |
| anthropic-agent | Agent with tool use, multi-step reasoning, and streaming via `agent.RunAgent` | `ANTHROPIC_API_KEY` | `go run ./ai-sdk-examples/anthropic-agent/ "prompt"` |
| embedding | Generate embeddings with OpenAI and compute cosine similarity between vectors | `OPENAI_API_KEY` | `go run ./ai-sdk-examples/embedding/` |
| image-generation | Image generation API pattern reference (Azure, TogetherAI providers) | — | `go run ./ai-sdk-examples/image-generation/` |
| multi-provider | Register multiple providers via registry, switch between them for chat, embedding, and reranking | `OPENAI_API_KEY`, `COHERE_API_KEY` | `go run ./ai-sdk-examples/multi-provider/` |
| object-generation | Structured JSON output API pattern reference via `core.GenerateObject` | — | `go run ./ai-sdk-examples/object-generation/` |
| production-setup | Production middleware pipeline: telemetry tracing, exponential-backoff retry, and circuit breaker | `OPENAI_API_KEY` | `go run ./ai-sdk-examples/production-setup/ "prompt"` |
| rerank | Document reranking with Cohere for RAG and search quality improvement | `COHERE_API_KEY` | `go run ./ai-sdk-examples/rerank/` |
| speech-to-text | Audio transcription API pattern reference | — | `go run ./ai-sdk-examples/speech-to-text/` |
| streaming-chat | Real-time streaming text generation via `core.StreamText` with token usage reporting | `OPENAI_API_KEY` | `go run ./ai-sdk-examples/streaming-chat/ "prompt"` |
| video-generation | Video generation API pattern reference (xAI grok-video provider) | — | `go run ./ai-sdk-examples/video-generation/` |

## Concepts Demonstrated

- **`core.GenerateText`** — Non-streaming text generation with optional tool calling (openai-chat, production-setup)
- **`core.StreamText`** — Real-time streaming with deltas, usage futures, and finish reasons (streaming-chat, anthropic-agent)
- **`core.GenerateVideo`** — Provider-agnostic video generation (video-generation)
- **`core.GenerateImage`** — Provider-agnostic image generation (image-generation)
- **`core.GenerateObject`** — Structured JSON output via schema (object-generation)
- **embedding** — Vector embeddings with cosine similarity, dot product, and vector norm (embedding, multi-provider)
- **reranking** — Document reranking by relevance score (rerank, multi-provider)
- **agent** — Tool-loop agent with event-driven streaming (anthropic-agent)
- **registry** — Multi-provider registration, retrieval, and capability switching (multi-provider)
- **middleware** — Retry with exponential backoff, circuit breaker, and telemetry tracing (production-setup)
- **informational examples** — API pattern documentation when providers or keys are unavailable (image-generation, object-generation, speech-to-text, video-generation)

## Provider Support

| Example | Provider(s) Used |
|---------|-----------------|
| openai-chat | OpenAI |
| anthropic-agent | Anthropic |
| embedding | OpenAI |
| multi-provider | OpenAI, Cohere |
| production-setup | OpenAI |
| rerank | Cohere |
| streaming-chat | OpenAI |
