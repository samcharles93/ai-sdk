// Package openai provides access to OpenAI's embedding API
// via the embed.Provider interface.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/samcharles93/ai-sdk/embed"
)

const (
	embeddingsAPIPath = "/embeddings"
)

// --- wire types ----------------------------------------------------------

type wireEmbedRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	EncodingFormat string   `json:"encoding_format,omitempty"`
	Dimensions     int      `json:"dimensions,omitempty"`
	User           string   `json:"user,omitempty"`
}

type wireEmbedDatum struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

type wireEmbedUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type wireEmbedResponse struct {
	Object string           `json:"object"`
	Data   []wireEmbedDatum `json:"data"`
	Model  string           `json:"model"`
	Usage  wireEmbedUsage   `json:"usage"`
}

// --- Embed ----------------------------------------------------------------

// Embed produces embedding vectors for the given inputs using OpenAI's
// embeddings API. It satisfies embed.Provider.
func (p *Provider) Embed(ctx context.Context, req embed.Request) (embed.Response, error) {
	if req.Model == "" {
		return embed.Response{}, fmt.Errorf("openai: model is required: %w", embed.ErrInvalidRequest)
	}
	if len(req.Inputs) == 0 {
		return embed.Response{}, fmt.Errorf("openai: at least one input is required: %w", embed.ErrInvalidRequest)
	}

	body := wireEmbedRequest{
		Model:          req.Model,
		Input:          req.Inputs,
		EncodingFormat: "float",
	}

	// Parse provider options for OpenAI-specific settings.
	if opts, ok := req.ProviderOptions["openai"].(map[string]any); ok {
		if dims, ok := opts["dimensions"].(float64); ok && dims > 0 {
			body.Dimensions = int(dims)
		}
		if user, ok := opts["user"].(string); ok && user != "" {
			body.User = user
		}
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return embed.Response{}, fmt.Errorf("openai: marshal embed request: %w", err)
	}

	url := p.baseURL + embeddingsAPIPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return embed.Response{}, fmt.Errorf("openai: build embed request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return embed.Response{}, fmt.Errorf("openai: http do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		snippet := strings.TrimSpace(string(respBody))
		return embed.Response{}, classifyEmbedHTTPError(resp.StatusCode, snippet)
	}

	var wr wireEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&wr); err != nil {
		return embed.Response{}, fmt.Errorf("openai: decode embed response: %w", err)
	}

	out := embed.Response{
		Model:      wr.Model,
		Embeddings: make([]embed.Embedding, len(wr.Data)),
	}
	for i, d := range wr.Data {
		embedding := embed.Embedding{
			Index:  d.Index,
			Vector: d.Embedding,
		}
		// OpenAI always returns data in order with contiguous indices,
		// but we use positional assignment to avoid panics if the API
		// ever returns out-of-order or sparse indices.
		if i < len(out.Embeddings) {
			out.Embeddings[i] = embedding
		} else {
			out.Embeddings = append(out.Embeddings, embedding)
		}
	}
	out.Usage = embed.Usage{
		PromptTokens: wr.Usage.PromptTokens,
		TotalTokens:  wr.Usage.TotalTokens,
	}

	return out, nil
}

func classifyEmbedHTTPError(code int, body string) error {
	var base error
	switch {
	case code == 401 || code == 403:
		base = embed.ErrAuthFailed
	case code == 429:
		base = embed.ErrRateLimited
	case code >= 500:
		base = embed.ErrProviderUnavailable
	default:
		base = embed.ErrProviderUnavailable
	}
	return fmt.Errorf("openai: status %d: %s: %w", code, body, base)
}

// Compile-time assertion that *Provider satisfies embed.Provider.
var _ embed.Provider = (*Provider)(nil)
