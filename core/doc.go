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
package core
