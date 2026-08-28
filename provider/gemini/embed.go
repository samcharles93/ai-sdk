package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/embed"
	errx "github.com/samcharles93/ai-sdk/error"
)

var _ embed.Provider = (*Provider)(nil)

type embedWirePart struct {
	Text string `json:"text"`
}

type embedWireContent struct {
	Parts []embedWirePart `json:"parts"`
}

type embedWireSubRequest struct {
	Model   string           `json:"model"`
	Content embedWireContent `json:"content"`
}

type embedWireRequest struct {
	Requests []embedWireSubRequest `json:"requests"`
}

type embedWireValues struct {
	Values []float32 `json:"values"`
}

type embedWireResponse struct {
	Embeddings []embedWireValues `json:"embeddings"`
}

// classifyEmbedHTTP maps a non-2xx HTTP response into a typed
// *errx.ProviderError, stripping the API key from any echoed URL in the
// snippet.
func classifyEmbedHTTP(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	snippet := chat.SanitizeErrorBody(body)
	snippet = scrubKey(snippet)

	var base error
	retryable := false
	switch {
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		base = embed.ErrAuthFailed
	case resp.StatusCode == 400 && strings.Contains(strings.ToLower(snippet), "api key"):
		base = embed.ErrAuthFailed
	case resp.StatusCode == 429:
		base = embed.ErrRateLimited
		retryable = true
	case resp.StatusCode >= 500:
		base = embed.ErrProviderUnavailable
		retryable = true
	default:
		base = embed.ErrProviderUnavailable
		retryable = true
	}
	return errx.NewProviderError("gemini", resp, base, snippet, retryable)
}

// Embed produces one embedding vector per entry in req.Inputs, in the same order.
func (p *Provider) Embed(ctx context.Context, req embed.Request) (embed.Response, error) {
	if strings.TrimSpace(req.Model) == "" {
		return embed.Response{}, fmt.Errorf("gemini: model is required: %w", embed.ErrInvalidRequest)
	}
	if len(req.Inputs) == 0 {
		return embed.Response{}, fmt.Errorf("gemini: at least one input is required: %w", embed.ErrInvalidRequest)
	}

	modelRef := "models/" + req.Model
	subs := make([]embedWireSubRequest, len(req.Inputs))
	for i, in := range req.Inputs {
		subs[i] = embedWireSubRequest{
			Model:   modelRef,
			Content: embedWireContent{Parts: []embedWirePart{{Text: in}}},
		}
	}
	body, err := json.Marshal(embedWireRequest{Requests: subs})
	if err != nil {
		return embed.Response{}, fmt.Errorf("gemini: marshal request: %w", err)
	}

	endpoint := p.baseURL +
		"/v1beta/models/" + url.PathEscape(req.Model) + ":batchEmbedContents" +
		"?key=" + url.QueryEscape(p.apiKey)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return embed.Response{}, fmt.Errorf("gemini: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return embed.Response{}, fmt.Errorf("gemini: do request: %w", errors.Join(err, embed.ErrProviderUnavailable))
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return embed.Response{}, classifyEmbedHTTP(resp)
	}

	var wr embedWireResponse
	if err := json.NewDecoder(resp.Body).Decode(&wr); err != nil {
		return embed.Response{}, fmt.Errorf("gemini: decode response: %w", err)
	}

	if len(wr.Embeddings) != len(req.Inputs) {
		return embed.Response{}, fmt.Errorf("gemini: embedding count mismatch: got %d want %d: %w",
			len(wr.Embeddings), len(req.Inputs), embed.ErrProviderUnavailable)
	}

	out := embed.Response{
		Model:      req.Model,
		Embeddings: make([]embed.Embedding, len(wr.Embeddings)),
	}
	for i, e := range wr.Embeddings {
		out.Embeddings[i] = embed.Embedding{Index: i, Vector: e.Values}
	}
	return out, nil
}
