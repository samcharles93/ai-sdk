# Groq

Speech uses `internal/tts` for the OpenAI-compatible `/audio/speech` endpoint.
Orpheus voices are model-scoped, so require a voice rather than guessing one.
The wrapper defaults to wav and permits only wav. Voice listing returns
`ErrVoiceListingNotSupported`. Check model-specific terms acceptance when a
live request is rejected. Orpheus delivery directions belong in the input text.
