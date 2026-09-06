// Package core provides the high-level AI SDK orchestration functions:
// GenerateText, StreamText, and supporting types for tools, structured
// output, and stop conditions.
//
// This package is the Go re-interpretation of the AI SDK Core layer
// (generateText, streamText). It builds on the lower-level chat.Provider
// interface and adds tool-calling loops, multi-step reasoning, output
// parsing, and streaming control.
//
// The primary entry points are:
//
//   - [GenerateText]: non-streaming text generation with optional tool
//     calling and structured output.
//   - [StreamText]: streaming text generation with the same capabilities.
//
// Facade coverage across the eight domains:
//
// Every domain group except embedding has a services-layer facade here that
// validates the provider, respects cancellation, and wraps provider errors
// with core context: chat ([GenerateText]/[StreamText]), image
// ([GenerateImage]), video ([GenerateVideo]), object ([GenerateObject]),
// speech ([GenerateSpeech]), transcribe ([Transcribe]), and rerank
// ([Rerank]).
//
// Embedding is intentionally client-only: the [embed.Client] facade in the
// embed domain package centralises its provider/existence/validation concerns
// and is used directly by higher layers, so no core facade is added. This is
// a deliberate asymmetry, kept implicit only to mirror the domain's own
// client-only design rather than bolt on a thin wrapper whose behaviour would
// duplicate [embed.Client].
package core
