# UI

Use Templ components and Datastar signals, event handlers and SSE for reactive
updates. Edit `.templ` source and regenerate `*_templ.go` output.

`ui/chat` owns `Chat`, `Transport`, `ChatOptions`, `Send`, `AddToolOutput`, and
finish/tool callbacks. These are UI state APIs, distinct from the `chat` domain.
Message parts and stream processing live in `uimessage`; HTTP SSE adaptation
lives in `uimessage/sse` (see its AGENTS.md).

Handlers define the service interfaces they consume. Wire concrete providers
at the composition root, not in UI packages.
