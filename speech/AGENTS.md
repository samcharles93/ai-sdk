# Speech synthesis

Keep voice discovery (`VoiceLister`) and streaming (`Streamer`) optional.
Client facades return the domain's not-supported error when unavailable.
Streams yield audio chunks, a Done chunk with optional usage, then `io.EOF`.
Establishment errors use buffered-path sentinels; later errors come from `Next`.

Runtime configuration and gating are described in `../runtime/AGENTS.md`.
Shared OpenAI-compatible transport lives in `../internal/tts`; OpenAI and Groq
remain thin wrappers. Streaming middleware is described in
`../middleware/AGENTS.md`.
