// Package sse implements the SSE wire format for the AI SDK UI message
// stream protocol. A [Writer] streams [uimessage.Chunk] values as
// `data: <json>\n\n` events to an [http.ResponseWriter], and
// [FromTextStream] adapts a core text-stream into a chunk channel for
// use with [Pipe].
package sse

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/samcharles93/ai-sdk/pkg/uimessage"
)

// Headers are the canonical response headers for an AI SDK UI message
// stream. They mirror the TypeScript reference (`UI_MESSAGE_STREAM_HEADERS`).
var Headers = map[string]string{
	"Content-Type":                  "text/event-stream",
	"Cache-Control":                 "no-cache",
	"Connection":                    "keep-alive",
	"X-Vercel-Ai-Ui-Message-Stream": "v1",
	"X-Accel-Buffering":             "no",
}

// ApplyHeaders writes the canonical SSE headers to h.
func ApplyHeaders(h http.Header) {
	for k, v := range Headers {
		h.Set(k, v)
	}
}

// Writer streams [uimessage.Chunk] values as SSE events.
//
// It is safe for concurrent use; calls to [Writer.WriteChunk] are
// serialised by an internal mutex.
type Writer struct {
	w  io.Writer
	f  http.Flusher
	mu sync.Mutex
}

// NewWriter wraps rw as an SSE [Writer]. If rw also implements
// [http.Flusher], events are flushed after every write. Headers are
// applied automatically.
func NewWriter(rw http.ResponseWriter) *Writer {
	ApplyHeaders(rw.Header())
	rw.WriteHeader(http.StatusOK)
	w := &Writer{w: rw}
	if f, ok := rw.(http.Flusher); ok {
		w.f = f
		f.Flush()
	}
	return w
}

// NewWriterTo wraps an arbitrary [io.Writer]. It does not write
// headers and does not flush. Useful for tests.
func NewWriterTo(w io.Writer) *Writer {
	return &Writer{w: w}
}

// WriteChunk serialises c and writes it as a single SSE `data:` event.
func (w *Writer) WriteChunk(c uimessage.Chunk) error {
	raw, err := uimessage.MarshalChunk(c)
	if err != nil {
		return fmt.Errorf("marshal chunk: %w", err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := fmt.Fprintf(w.w, "data: %s\n\n", raw); err != nil {
		return fmt.Errorf("write event: %w", err)
	}
	if w.f != nil {
		w.f.Flush()
	}
	return nil
}

// WriteRaw writes a pre-encoded JSON payload as an SSE event.
func (w *Writer) WriteRaw(payload json.RawMessage) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := fmt.Fprintf(w.w, "data: %s\n\n", payload); err != nil {
		return fmt.Errorf("write raw event: %w", err)
	}
	if w.f != nil {
		w.f.Flush()
	}
	return nil
}

// Pipe drains src into w until src is closed or ctx is cancelled.
//
// Pipe returns the first non-nil error from a write; cancellation is
// signalled via [context.Cause].
func Pipe(ctx context.Context, src <-chan uimessage.Chunk, w *Writer) error {
	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case c, ok := <-src:
			if !ok {
				return nil
			}
			if err := w.WriteChunk(c); err != nil {
				return err
			}
		}
	}
}

// SSEEventLines splits a UI message stream payload (`"data: ...\n\n"`)
// into the JSON bodies of each event. It is intended for tests.
func SSEEventLines(s string) []string {
	var out []string
	for ev := range strings.SplitSeq(s, "\n\n") {
		ev = strings.TrimSpace(ev)
		if ev == "" {
			continue
		}
		ev = strings.TrimPrefix(ev, "data:")
		out = append(out, strings.TrimSpace(ev))
	}
	return out
}
