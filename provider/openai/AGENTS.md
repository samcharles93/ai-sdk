# OpenAI

Speech uses `internal/tts` for `/audio/speech`, just as transcription uses
`internal/whisper`. Keep the provider a thin wrapper around shared transport.
Native voice listing uses the static set; compatible-server discovery queries
`/audio/voices` and normalises string/object response shapes. A missing endpoint
returns `ErrVoiceListingNotSupported`.

Speech streaming uses SSE where supported and raw audio for `tts-1` variants.
Read `../../speech/AGENTS.md` and `../../runtime/AGENTS.md` before changing the
stream contract or generic compatible-server defaults and limits.
