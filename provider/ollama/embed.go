package ollama

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

type ollamaEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type ollamaEmbedResponse struct {
	Model           string      `json:"model"`
	Embeddings      [][]float32 `json:"embeddings"`
	PromptEvalCount int         `json:"prompt_eval_count"`
}

func classifyEmbedStatus(code int) error {
	switch {
	case code == 401 || code == 403:
		return embed.ErrAuthFailed
	case code == 429:
		return embed.ErrRateLimited
	case code >= 500:
		return embed.ErrProviderUnavailable
	default:
		return embed.ErrProviderUnavailable
	}
}

// Embed produces one embedding vector per entry in req.Inputs by calling
// Ollama's /api/embed endpoint.
func (p *Provider) Embed(ctx context.Context, req embed.Request) (embed.Response, error) {
	if req.Model == "" {
		return embed.Response{}, fmt.Errorf("ollama: model is required: %w", embed.ErrInvalidRequest)
	}
	if len(req.Inputs) == 0 {
		return embed.Response{}, fmt.Errorf("ollama: at least one input is required: %w", embed.ErrInvalidRequest)
	}

	body := ollamaEmbedRequest{Model: req.Model, Input: req.Inputs}
	buf, err := json.Marshal(body)
	if err != nil {
		return embed.Response{}, fmt.Errorf("ollama: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/embed", bytes.NewReader(buf))
	if err != nil {
		return embed.Response{}, fmt.Errorf("ollama: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := p.http.Do(httpReq)
	if err != nil {
		return embed.Response{}, fmt.Errorf("ollama: http call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		base := classifyEmbedStatus(resp.StatusCode)
		return embed.Response{}, fmt.Errorf("ollama: http %d: %s: %w", resp.StatusCode, strings.TrimSpace(string(snippet)), base)
	}

	var oer ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&oer); err != nil {
		return embed.Response{}, fmt.Errorf("ollama: decode response: %w", err)
	}

	if len(oer.Embeddings) != len(req.Inputs) {
		return embed.Response{}, fmt.Errorf("ollama: expected %d embeddings, got %d: %w", len(req.Inputs), len(oer.Embeddings), embed.ErrProviderUnavailable)
	}

	embeddings := make([]embed.Embedding, len(oer.Embeddings))
	for i, v := range oer.Embeddings {
		embeddings[i] = embed.Embedding{Index: i, Vector: v}
	}

	model := oer.Model
	if model == "" {
		model = req.Model
	}

	return embed.Response{
		Model:      model,
		Embeddings: embeddings,
		Usage: embed.Usage{
			PromptTokens: oer.PromptEvalCount,
			TotalTokens:  oer.PromptEvalCount,
		},
	}, nil
}

var _ embed.Provider = (*Provider)(nil)
