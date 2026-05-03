// Package handlers provides HTTP handler implementations for AI SDK UI
// endpoints. These handlers define the service interfaces they depend on
// (following Go convention: consumers define interfaces).
//
// Handlers:
//
//   - chat.go: Chat API handler for sending messages and receiving streams
//
// Handlers use Datastar to stream SSE responses to the client for
// real-time text deltas, tool calls, and status updates.
package handlers
