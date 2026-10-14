# Providers

Read the provider's current docs or official SDK types before changing wire
structs. Keep shared wire helpers in `internal/`, never import another provider.
For domain contracts, read the corresponding domain's AGENTS.md where present.

Runtime capabilities depend on both class registration and implemented
interfaces; see `../runtime/AGENTS.md`. TogetherAI and MiniMax chat use the
OpenAI-compatible transport, while their native packages implement other domains.
Azure shares one provider for chat/embed/image; xAI shares chat/image/video.

`openaiobject` is a standalone structured-object backend using Chat Completions
with `response_format.json_schema`. Keep typed-object orchestration in `core`;
provider integration checks must not introduce a provider-to-core dependency.

Anthropic thinking options travel through `chat.Request.ProviderOptions` as
`reasoning_effort` or `thinking_budget_tokens`; model-specific validation lives
in its adapter.

Gemini chat has two transports: `gemini` (generateContent) and
`gemini-interactions` (Interactions API, steps schema via `Api-Revision`).
Interactions requests send `store: false`; long decode (`MaxTokens` > 65536)
must use `background` + `store`, so the provider deletes those interactions
after reading or cancelling them. Keep that invariant when changing it.
