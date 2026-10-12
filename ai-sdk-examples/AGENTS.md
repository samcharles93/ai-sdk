# Examples

This is a separate Go module. Examples demonstrate SDK entry points; keep them
consistent with domain contracts and runtime resolution.

`openai-chat` is an interactive chat CLI; `anthropic-agent` demonstrates tool use
and streaming. Object, transcription, image and video examples live in
`object-generation`, `speech-to-text`, `image-generation`, and `video-generation`.
Run an example from this directory with `go run ./<example>/`. Live examples
need provider credentials; never put keys into source or documentation.
