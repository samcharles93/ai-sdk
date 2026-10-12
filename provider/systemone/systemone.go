// Package systemone implements llama.cpp's System One decision API.
package systemone

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/samcharles93/ai-sdk/decide"
)

type (
	Config struct {
		BaseURL    string
		APIKey     string
		HTTPClient *http.Client
	}
	Provider struct {
		baseURL string
		apiKey  string
		client  *http.Client
	}
)

func New(cfg Config) (*Provider, error) {
	u, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("systemone: base URL required: %w", decide.ErrInvalidRequest)
	}
	if u.Path == "" {
		u.Path = "/v1"
	}
	c := cfg.HTTPClient
	if c == nil {
		c = &http.Client{}
	}
	return &Provider{baseURL: u.String(), apiKey: cfg.APIKey, client: c}, nil
}
func (p *Provider) Name() string { return "systemone" }

var _ decide.Provider = (*Provider)(nil)

func (p *Provider) request(ctx context.Context, method, path string, body []byte, out any) error {
	r, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if p.apiKey != "" {
		r.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.client.Do(r)
	if err != nil {
		return fmt.Errorf("systemone: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		base := decide.ErrProviderUnavailable
		switch resp.StatusCode {
		case 400, 422:
			base = decide.ErrInvalidRequest
		case 401, 403:
			base = decide.ErrAuthFailed
		case 429:
			base = decide.ErrRateLimited
		case 501:
			base = decide.ErrUnsupported
		}
		return fmt.Errorf("systemone: HTTP %d: %w", resp.StatusCode, base)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("systemone: decode: %w", err)
	}
	return nil
}

type Model struct {
	ID           string `json:"id"`
	Architecture struct {
		OutputModalities []string `json:"output_modalities"`
	} `json:"architecture"`
}

func (p *Provider) Models(ctx context.Context) ([]Model, error) {
	var out struct {
		Data []Model `json:"data"`
	}
	if err := p.request(ctx, http.MethodGet, "/models", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

type (
	question struct {
		Type         string `json:"type"`
		Instructions string `json:"instructions"`
		Criteria     any    `json:"criteria,omitempty"`
	}
	answer struct {
		Type          string             `json:"type"`
		Choice        string             `json:"choice"`
		Score         float64            `json:"score"`
		Noul          *float64           `json:"noul"`
		Probabilities map[string]float64 `json:"probabilities"`
		Confidence    float64            `json:"confidence"`
	}
)

// JSON spelling keeps boolean true distinct from the string "true".
func choiceKey(v decide.Value) string { b, _ := json.Marshal(v); return string(b) }

func (p *Provider) Decide(ctx context.Context, req decide.Request) (decide.Response, error) {
	if err := req.Validate(); err != nil {
		return decide.Response{}, err
	}
	var state any = req.Input
	images := []string{}
	if len(req.Messages) > 0 {
		messages := []map[string]string{}
		for _, m := range req.Messages {
			messages = append(messages, map[string]string{"role": "user", "content": m.Text})
			for _, u := range m.Images {
				if !strings.HasPrefix(u, "data:image/") {
					return decide.Response{}, fmt.Errorf("systemone: images require data URLs: %w", decide.ErrInvalidRequest)
				}
				images = append(images, u)
			}
		}
		state = messages
	}
	questions := map[string]question{}
	for _, q := range req.Questions {
		w := question{Type: string(q.Type), Instructions: q.Instructions}
		switch q.Type {
		case decide.Predicate:
			w.Type = "noul"
		case decide.Choice:
			criteria := map[string]any{}
			for _, c := range q.Choices {
				var desc any
				if c.Description != "" {
					desc = c.Description
				}
				criteria[choiceKey(c.Value)] = desc
			}
			w.Criteria = criteria
		case decide.Score:
			if len(q.Levels) > 10 {
				return decide.Response{}, fmt.Errorf("systemone: at most 10 levels: %w", decide.ErrInvalidRequest)
			}
			levels := []string{}
			for _, l := range q.Levels {
				d := l.Label
				if l.Description != "" {
					d += ": " + l.Description
				}
				levels = append(levels, d)
			}
			w.Criteria = levels
		}
		questions[q.Name] = w
	}
	body := struct {
		Model     string              `json:"model"`
		State     any                 `json:"state"`
		Images    []string            `json:"images,omitempty"`
		Questions map[string]question `json:"questions"`
	}{req.Model, state, images, questions}
	b, err := json.Marshal(body)
	if err != nil {
		return decide.Response{}, err
	}
	var wire struct {
		Model   string            `json:"model"`
		Answers map[string]answer `json:"answers"`
		Usage   decide.Usage      `json:"usage"`
	}
	if err := p.request(ctx, http.MethodPost, "/systemone", b, &wire); err != nil {
		return decide.Response{}, err
	}
	out := decide.Response{Model: wire.Model, Usage: wire.Usage}
	out.Usage.TotalTokens = out.Usage.InputTokens + out.Usage.OutputTokens
	for _, q := range req.Questions {
		w, ok := wire.Answers[q.Name]
		if !ok || w.Type != questions[q.Name].Type {
			return decide.Response{}, fmt.Errorf("systemone: missing or mismatched answer %q", q.Name)
		}
		a := decide.Answer{Name: q.Name, Type: q.Type, Confidence: w.Confidence}
		switch q.Type {
		case decide.Predicate:
			if w.Noul == nil {
				return decide.Response{}, fmt.Errorf("systemone: missing noul probability")
			}
			a.Predicate = &decide.PredicateAnswer{Probability: *w.Noul}
			a.Confidence = math.Max(*w.Noul, 1-*w.Noul)
		case decide.Choice:
			a.Choice = &decide.ChoiceAnswer{}
			found := false
			for _, c := range q.Choices {
				key := choiceKey(c.Value)
				prob, ok := w.Probabilities[key]
				if !ok {
					return decide.Response{}, fmt.Errorf("systemone: missing choice probability %q", key)
				}
				a.Choice.Probabilities = append(a.Choice.Probabilities, decide.ChoiceProbability{Value: c.Value, Probability: prob})
				if w.Choice == key {
					a.Choice.Choice = c.Value
					found = true
				}
			}
			if !found {
				return decide.Response{}, fmt.Errorf("systemone: unknown choice %q", w.Choice)
			}
		case decide.Score:
			a.Score = &decide.ScoreAnswer{Score: w.Score}
			for i, l := range q.Levels {
				prob, ok := w.Probabilities[strconv.Itoa(i)]
				if !ok {
					return decide.Response{}, fmt.Errorf("systemone: missing score probability %d", i)
				}
				a.Score.Probabilities = append(a.Score.Probabilities, decide.ScoreProbability{Label: l.Label, Value: i, Probability: prob})
			}
		}
		out.Answers = append(out.Answers, a)
	}
	return out, nil
}
