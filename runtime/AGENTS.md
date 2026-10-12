# runtime

- A `ProviderClass` turns a `ProviderConfig` into a `ProviderSet` with one
  optional field per domain. A class must advertise a capability in `caps` and
  return a provider that implements it; otherwise the field stays nil.
- Each domain has `rt.<Domain>(ctx, ref, req)` and `rt.<Domain>Provider(ctx,
  ref)`. A new domain adds both, its `Capability` constant and its
  `ProviderSet` field.
- Resolution is metadata-gated where the catalog or server publishes
  modalities: a model whose metadata excludes the capability is refused with
  `ErrCapabilityNotSupported` before a request is sent.
- Self-hosted OpenAI-compatible servers (llama.cpp, llama-swap, vLLM, Kokoro)
  resolve through the `openai-compatible` class with a `BaseURL` and
  `auth.type: none`.
- MiniMax is the worked example of a non-catalog provider wired as a custom
  class with per-model config.

Catalog loading and overrides supply provider/model metadata. When building a
provider that implements several domains, use the combined builder and type
assertions so the domain fields share one instance.

## Speech configuration

For self-hosted OpenAI-compatible TTS, supply the server BaseURL and
`auth.type: none` for a keyless server. The generic class has no built-in voice:
set Voice on the request or `speech_default_voice` / `speech_default_format`
in provider options. Per-model extra overrides provider options.

Speech resolution refuses published modalities or explicit capabilities that
exclude speech output before sending a request. Unknown metadata is attempted
best-effort; `capabilities: ["speech"]` makes the intended capability explicit.

Runtime input-limit defaults are 4096 characters for OpenAI and 200 for Groq;
the generic class is uncapped. `max_input_chars` in options or model extra
overrides the limit. Oversized input returns `ErrInvalidRequest` locally.

MiniMax resolves from explicit provider configuration and model IDs when absent
from the catalog. Its native provider supplies image, speech, music and video;
chat uses the OpenAI-compatible transport. TogetherAI uses the same split for
chat versus native image/rerank. Register built-ins with
`RegisterBuiltinClasses`; custom `ProviderClass` factories are the escape hatch
for discovery, authentication exchange and header injection.
