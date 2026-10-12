package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"

	"github.com/samcharles93/ai-sdk/decide"
)

var _ decide.Provider = (*Provider)(nil)

type DecideOptions struct {
	SafetyIdentifier string `json:"safety_identifier,omitempty"`
}

func (p *Provider) Decide(ctx context.Context, req decide.Request) (decide.Response, error) {
	if err := req.Validate(); err != nil {
		return decide.Response{}, err
	}
	opts, err := decide.ProviderOptionsFor[DecideOptions](req.ProviderOptions, p.Name())
	if err != nil {
		return decide.Response{}, err
	}
	var input any = req.Input
	if len(req.Messages) > 0 {
		messages := make([]map[string]any, 0, len(req.Messages))
		for _, m := range req.Messages {
			parts := []map[string]string{}
			if m.Text != "" {
				parts = append(parts, map[string]string{"type": "input_text", "text": m.Text})
			}
			for _, u := range m.Images {
				parts = append(parts, map[string]string{"type": "input_image", "image_url": u})
			}
			messages = append(messages, map[string]any{"role": "user", "content": parts})
		}
		input = messages
	}
	body := struct {
		Model            string            `json:"model"`
		Input            any               `json:"input"`
		Questions        []decide.Question `json:"questions"`
		SafetyIdentifier string            `json:"safety_identifier,omitempty"`
	}{req.Model, input, req.Questions, opts.SafetyIdentifier}
	b, err := json.Marshal(body)
	if err != nil {
		return decide.Response{}, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/decisions", bytes.NewReader(b))
	if err != nil {
		return decide.Response{}, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+p.apiKey)
	resp, err := p.client.Do(r)
	if err != nil {
		return decide.Response{}, fmt.Errorf("openai decisions: %w", err)
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
		return decide.Response{}, fmt.Errorf("openai decisions: HTTP %d: %w", resp.StatusCode, base)
	}
	var wire struct {
		Model   string            `json:"model"`
		Answers []json.RawMessage `json:"answers"`
		Usage   decide.Usage      `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return decide.Response{}, fmt.Errorf("openai decisions: decode: %w", err)
	}
	if len(wire.Answers) != len(req.Questions) {
		return decide.Response{}, fmt.Errorf("openai decisions: answer count mismatch")
	}
	out := decide.Response{Model: wire.Model, Usage: wire.Usage}
	for i, raw := range wire.Answers {
		var head struct {
			Name        string      `json:"name"`
			Type        decide.Kind `json:"type"`
			Confidence  float64     `json:"confidence"`
			Probability *float64    `json:"probability"`
		}
		if err := json.Unmarshal(raw, &head); err != nil {
			return decide.Response{}, err
		}
		if head.Name != req.Questions[i].Name || (head.Type != decide.Refusal && head.Type != req.Questions[i].Type) {
			return decide.Response{}, fmt.Errorf("openai decisions: mismatched answer %q", head.Name)
		}
		a := decide.Answer{Name: head.Name, Type: head.Type, Confidence: head.Confidence}
		switch head.Type {
		case decide.Predicate:
			if head.Probability == nil || *head.Probability < 0 || *head.Probability > 1 {
				return decide.Response{}, fmt.Errorf("openai decisions: invalid predicate probability")
			}
			a.Predicate = &decide.PredicateAnswer{Probability: *head.Probability}
			a.Confidence = math.Max(a.Predicate.Probability, 1-a.Predicate.Probability)
		case decide.Choice:
			a.Choice = &decide.ChoiceAnswer{}
			err = json.Unmarshal(raw, a.Choice)
		case decide.Score:
			a.Score = &decide.ScoreAnswer{}
			err = json.Unmarshal(raw, a.Score)
		case decide.Refusal:
			a.Refusal = &decide.RefusalAnswer{}
		default:
			return decide.Response{}, fmt.Errorf("openai decisions: unknown answer type %q", head.Type)
		}
		if err != nil {
			return decide.Response{}, fmt.Errorf("openai decisions: decode answer: %w", err)
		}
		out.Answers = append(out.Answers, a)
	}
	return out, nil
}
