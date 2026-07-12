// Package openai implements the chat.Provider interface for OpenAI's Chat
// Completions and Responses APIs.
//
// The provider selects the wire protocol from the canonical chat request.
// Chat Completions and Responses have independent request builders, response
// decoders, and streaming event parsers.
package openai
