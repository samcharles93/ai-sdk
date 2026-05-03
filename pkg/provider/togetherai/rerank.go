package togetherai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/samcharles93/ai-sdk/pkg/rerank"
)

// Rerank implements [rerank.Provider] against the Together AI
// /v1/rerank endpoint.
func (p *Provider) Rerank(ctx context.Context, req rerank.Request) (rerank.Response, error) {
	if req.Query == "" || len(req.Documents) == 0 || req.Model == "" {
		return rerank.Response{}, rerank.ErrInvalidRequest
	}

	body := map[string]any{
		"model":            req.Model,
		"query":            req.Query,
		"documents":        req.Documents,
		"return_documents": false,
	}
	if req.TopN > 0 {
		body["top_n"] = req.TopN
	}

	opts, err := rerank.ProviderOptionsFor[Options](req.ProviderOptions, providerName)
	if err != nil {
		return rerank.Response{}, fmt.Errorf("togetherai: provider options: %w", err)
	}
	if len(opts.RankFields) > 0 {
		body["rank_fields"] = opts.RankFields
	}
	for k, v := range opts.Extra {
		if _, exists := body[k]; !exists {
			body[k] = v
		}
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return rerank.Response{}, fmt.Errorf("togetherai: marshal: %w", err)
	}

	url := p.baseURL + "/rerank"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return rerank.Response{}, fmt.Errorf("togetherai: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.http.Do(httpReq)
	if err != nil {
		return rerank.Response{}, fmt.Errorf("togetherai: %w: %v", rerank.ErrProviderUnavailable, err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return rerank.Response{}, fmt.Errorf("togetherai: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return rerank.Response{}, mapRerankHTTPError(resp.StatusCode, bodyBytes)
	}

	var decoded togetherRerankResponse
	if err := json.Unmarshal(bodyBytes, &decoded); err != nil {
		return rerank.Response{}, fmt.Errorf("togetherai: decode: %w", err)
	}

	out := rerank.Response{
		Model:   decoded.Model,
		Ranking: make([]rerank.RankingItem, 0, len(decoded.Results)),
	}
	for _, r := range decoded.Results {
		out.Ranking = append(out.Ranking, rerank.RankingItem{
			OriginalIndex: r.Index,
			Score:         r.RelevanceScore,
		})
	}
	return out, nil
}

type togetherRerankResult struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

type togetherRerankResponse struct {
	ID      string                 `json:"id"`
	Model   string                 `json:"model"`
	Usage   togetherRerankUsage    `json:"usage"`
	Results []togetherRerankResult `json:"results"`
}

type togetherRerankUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func mapRerankHTTPError(status int, body []byte) error {
	msg := string(bytes.TrimSpace(body))
	var env togetherErrorEnvelope
	if json.Unmarshal(body, &env) == nil && env.Error.Message != "" {
		msg = env.Error.Message
	}

	var sentinel error
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		sentinel = rerank.ErrAuthFailed
	case status == http.StatusTooManyRequests:
		sentinel = rerank.ErrRateLimited
	case status == http.StatusBadRequest:
		sentinel = rerank.ErrInvalidRequest
	case status >= 500:
		sentinel = rerank.ErrProviderUnavailable
	default:
		sentinel = rerank.ErrProviderUnavailable
	}
	return fmt.Errorf("togetherai: HTTP %d: %s: %w", status, msg, sentinel)
}

// Compile-time assertion that *Provider satisfies rerank.Provider.
var _ rerank.Provider = (*Provider)(nil)
