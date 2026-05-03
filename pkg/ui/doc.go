// Package ui provides the AI SDK UI layer — a Go re-interpretation of the
// AI SDK UI libraries (React, Svelte, Vue, Angular) using server-side
// Templ components and Datastar for streaming reactivity.
//
// The UI layer is the outermost layer in the onion model. It depends on
// domain interfaces (pkg/chat) and services (pkg/core) but no provider
// implementations.
//
// Sub-packages:
//
//   - chat: Chat state management equivalent to the JS useChat() hook
//   - components: Templ component files (.templ) for rendering chat UIs
//   - handlers: HTTP handler implementations for chat endpoints
package ui
