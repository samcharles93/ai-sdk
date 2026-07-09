// Package components provides Templ component files (.templ) for
// rendering AI SDK UI components. These are server-side equivalents of
// the JS AI SDK component libraries.
//
// Components:
//
//   - chat.templ: Main chat container with message list and input
//   - message.templ: Individual message rendering (user, assistant, system)
//   - input.templ: Chat input form with send button
//
// All components use Datastar attributes (data-signals, data-on-*) for
// client-side reactivity and SSE streaming updates.
//
// Run "templ generate" in this directory to generate Go code from .templ files.
package components
