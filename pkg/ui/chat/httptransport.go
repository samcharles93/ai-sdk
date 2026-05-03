package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"strings"

	"github.com/samcharles93/ai-sdk/pkg/uimessage"
)

// HTTPTransport is an HTTP-based [Transport] that POSTs UI messages to
// a remote endpoint and reads SSE events from the response body.
//
// The remote endpoint MUST respond with `text/event-stream` content
// encoding one [uimessage.Chunk] per `data: <json>\n\n` line.
type HTTPTransport struct {
	// BaseURL is the scheme + host + optional path prefix for the
	// remote AI SDK server (e.g. "http://localhost:8080/api").
	// SendMessages POSTs to BaseURL + "/chat/stream".
	BaseURL string

	// Client is the HTTP client used for requests. If nil,
	// [http.DefaultClient] is used.
	Client *http.Client
}

// NewHTTPTransport returns an [HTTPTransport] for the given base URL.
func NewHTTPTransport(baseURL string) *HTTPTransport {
	return &HTTPTransport{BaseURL: baseURL}
}

// SendMessages implements [Transport]. It marshals the arguments to
// JSON, POSTs to BaseURL+"/chat/stream", and returns a channel of
// [Chunk] events parsed from the SSE response.
//
// The returned channel is closed when the response body is exhausted or
// ctx is cancelled. Context cancellation aborts the HTTP request and
// closes the response body.
func (t *HTTPTransport) SendMessages(ctx context.Context, chatID string, messages []Message, opts SendOptions) (<-chan Chunk, error) {
	reqBody := map[string]any{
		"chatID":   chatID,
		"messages": messages,
	}
	if opts.Metadata != nil {
		reqBody["metadata"] = opts.Metadata
	}
	maps.Copy(reqBody, opts.Body)

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("httptransport: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.BaseURL+"/chat/stream", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("httptransport: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}

	client := t.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("httptransport: post: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("httptransport: unexpected status %d", resp.StatusCode)
	}

	ch := make(chan Chunk, 8)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		send := func(c Chunk) bool {
			select {
			case <-ctx.Done():
				return false
			case ch <- c:
				return true
			}
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			const prefix = "data: "
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			payload := line[len(prefix):]
			if payload == "" {
				continue
			}

			chunk, err := uimessage.UnmarshalChunk([]byte(payload))
			if err != nil {
				_ = send(uimessage.ErrorChunk{ErrorText: fmt.Sprintf("parse chunk: %v", err)})
				continue
			}
			if !send(chunk) {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			_ = send(uimessage.ErrorChunk{ErrorText: fmt.Sprintf("read response: %v", err)})
		}
	}()

	return ch, nil
}
