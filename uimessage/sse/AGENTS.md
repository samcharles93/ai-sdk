# uimessage/sse

- `Writer` — streams `uimessage.Chunk` values as SSE `data:` events
- `NewWriter(rw http.ResponseWriter)` — applies headers, flushes automatically
- `Headers` — canonical AI SDK UI stream headers
  (`X-Vercel-Ai-Ui-Message-Stream: v1`)
- `Pipe(ctx, src, w)` — drains chunk channel into SSE writer
- `FromTextStream` — adapts core text stream into UI message chunks
