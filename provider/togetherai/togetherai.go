// Package togetherai provides Together AI provider implementations for
// the SDK. Supported capabilities: image generation
// (/v1/images/generations) and document reranking (/v1/rerank).
// (https://docs.together.ai/reference/post_images-generations).
//
// Wire shape (request):
//
//	POST {baseURL}/images/generations
//	Authorization: Bearer <api-key>
//	{
//	  "model": "...",
//	  "prompt": "...",
//	  "seed": 42,
//	  "n": 2,
//	  "width": 1024,
//	  "height": 1024,
//	  "response_format": "base64",
//	  // optional pass-through (Together AI specific):
//	  "steps": 28,
//	  "guidance": 3.5,
//	  "negative_prompt": "...",
//	  "disable_safety_checker": false
//	}
//
// Response:
//
//	{ "data": [ { "b64_json": "..." }, ... ] }
//
// Errors:
//
//	{ "error": { "message": "..." } }
//
// Per-call provider-specific options are passed through
// [image.GenerateImageRequest.ProviderOptions] under the "togetherai"
// key as a [Options] struct (or any json-compatible map shape); see
// [Options] for the recognised fields.
package togetherai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/samcharles93/ai-sdk/image"
)

// DefaultBaseURL is the standard Together AI API root.
const DefaultBaseURL = "https://api.together.xyz/v1"

const providerName = "togetherai"

// Config configures a [Provider].
type Config struct {
	// APIKey is the Together AI bearer token. Required.
	APIKey string
	// BaseURL overrides [DefaultBaseURL]. Trailing slashes are trimmed.
	BaseURL string
	// HTTPClient is used for all outbound requests. If nil, a client
	// with a 5-minute timeout is used.
	HTTPClient *http.Client
}

// Provider implements [image.Provider] against the Together AI API.
type Provider struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

// New returns a new Together AI Provider. APIKey is required when
// using the default api.together.xyz endpoint, but may be empty for
// self-hosted or local Together AI-compatible endpoints.
func New(cfg Config) (*Provider, error) {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = DefaultBaseURL
	}
	base = strings.TrimRight(base, "/")
	if cfg.APIKey == "" && base == DefaultBaseURL {
		return nil, fmt.Errorf("%w: APIKey is required when using the default base URL", errProviderConfig)
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Minute}
	}
	return &Provider{apiKey: cfg.APIKey, baseURL: base, http: hc}, nil
}

// Name returns the provider identifier — "togetherai".
func (p *Provider) Name() string { return providerName }

// Options is the typed view of Together AI's provider-specific
// options. All fields are optional; zero values are omitted from the
// wire body. Fields that apply only to specific capabilities (image
// vs rerank) are silently ignored by the provider for endpoints that
// don't recognise them.
//
// Use as the "togetherai" bucket of Request.ProviderOptions:
//
//	req.ProviderOptions = map[string]any{
//	    "togetherai": togetherai.Options{Steps: 28, RankFields: []string{"title","content"}},
//	}
type Options struct {
	// Steps is the number of generation steps (higher → typically better quality).
	// Image generation only.
	Steps int `json:"steps,omitempty"`
	// Guidance is the classifier-free guidance scale. Image generation only.
	Guidance float64 `json:"guidance,omitempty"`
	// NegativePrompt mirrors domain-level negative prompt but takes
	// precedence when set. Image generation only.
	NegativePrompt string `json:"negative_prompt,omitempty"`
	// DisableSafetyChecker disables Together AI's NSFW filter (not all
	// models support this — Flux Schnell Free and Flux Pro reject it).
	DisableSafetyChecker bool `json:"disable_safety_checker,omitempty"`
	// RankFields specifies which fields to use for ranking when
	// documents are JSON objects. Rerank only.
	RankFields []string `json:"rank_fields,omitempty"`
	// Extra carries any additional Together-specific fields that are
	// not first-class on Options. Keys are passed through verbatim.
	Extra map[string]any `json:"-"`
}

// togetherImageBody is the wire representation of an image generation
// request. It is built lazily as a map[string]any so that we can
// preserve omit-empty semantics across optional fields without
// needing pointer-to-everything.
type togetherImageBody = map[string]any

type togetherImageResponse struct {
	Data []togetherImageItem `json:"data"`
}

type togetherImageItem struct {
	B64JSON string `json:"b64_json"`
	URL     string `json:"url,omitempty"`
}

type togetherErrorEnvelope struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type,omitempty"`
		Code    string `json:"code,omitempty"`
	} `json:"error"`
}

// GenerateImage implements [image.Provider].
func (p *Provider) GenerateImage(ctx context.Context, req image.GenerateImageRequest) (image.GenerateImageResponse, error) {
	if req.Prompt == "" || req.Model == "" {
		return image.GenerateImageResponse{}, image.ErrInvalidRequest
	}

	body, warnings, err := buildBody(req)
	if err != nil {
		return image.GenerateImageResponse{}, err
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return image.GenerateImageResponse{}, fmt.Errorf("togetherai: marshal: %w", err)
	}

	url := p.baseURL + "/images/generations"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return image.GenerateImageResponse{}, fmt.Errorf("togetherai: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.http.Do(httpReq)
	if err != nil {
		return image.GenerateImageResponse{}, fmt.Errorf("togetherai: %w: %v", image.ErrProviderUnavailable, err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return image.GenerateImageResponse{}, fmt.Errorf("togetherai: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return image.GenerateImageResponse{}, mapHTTPError(resp.StatusCode, bodyBytes)
	}

	var decoded togetherImageResponse
	if err := json.Unmarshal(bodyBytes, &decoded); err != nil {
		return image.GenerateImageResponse{}, fmt.Errorf("togetherai: decode: %w", err)
	}
	if len(decoded.Data) == 0 {
		return image.GenerateImageResponse{}, fmt.Errorf("togetherai: empty response: %w", image.ErrProviderUnavailable)
	}

	out := image.GenerateImageResponse{
		Images:   make([]image.GeneratedImage, 0, len(decoded.Data)),
		Warnings: warnings,
	}
	for _, item := range decoded.Data {
		out.Images = append(out.Images, image.GeneratedImage{
			Base64:    item.B64JSON,
			URL:       item.URL,
			MediaType: "image/png",
		})
	}
	return out, nil
}

// buildBody assembles the JSON-encoded body for a Together AI image
// generation request and returns any non-fatal warnings about fields
// the API does not support (e.g. AspectRatio).
func buildBody(req image.GenerateImageRequest) (togetherImageBody, []string, error) {
	body := togetherImageBody{
		"model":           req.Model,
		"prompt":          req.Prompt,
		"response_format": "base64",
	}

	var warnings []string

	if req.N > 1 {
		body["n"] = req.N
	}
	if req.Seed != nil {
		body["seed"] = *req.Seed
	}
	if req.NegativePrompt != "" {
		body["negative_prompt"] = req.NegativePrompt
	}
	if req.Size != "" {
		w, h, err := parseSize(req.Size)
		if err != nil {
			return nil, nil, fmt.Errorf("togetherai: %w: %v", image.ErrInvalidRequest, err)
		}
		body["width"] = w
		body["height"] = h
	}
	if req.AspectRatio != "" {
		warnings = append(warnings,
			"togetherai does not support aspect_ratio; use Size (e.g. \"1024x1024\") instead — value ignored")
	}

	opts, err := image.ProviderOptionsFor[Options](req.ProviderOptions, providerName)
	if err != nil {
		return nil, nil, fmt.Errorf("togetherai: provider options: %w", err)
	}
	if opts.Steps != 0 {
		body["steps"] = opts.Steps
	}
	if opts.Guidance != 0 {
		body["guidance"] = opts.Guidance
	}
	if opts.NegativePrompt != "" {
		body["negative_prompt"] = opts.NegativePrompt
	}
	if opts.DisableSafetyChecker {
		body["disable_safety_checker"] = true
	}
	for k, v := range opts.Extra {
		// Per-key passthrough for forward compatibility. We do not
		// overwrite first-class fields the SDK already populated.
		if _, exists := body[k]; !exists {
			body[k] = v
		}
	}
	return body, warnings, nil
}

// parseSize parses a "WIDTHxHEIGHT" string into integer dimensions.
func parseSize(size string) (int, int, error) {
	parts := strings.SplitN(size, "x", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid size %q (want WIDTHxHEIGHT)", size)
	}
	w, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || w <= 0 {
		return 0, 0, fmt.Errorf("invalid width in size %q", size)
	}
	h, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || h <= 0 {
		return 0, 0, fmt.Errorf("invalid height in size %q", size)
	}
	return w, h, nil
}

// mapHTTPError converts an HTTP failure response into a wrapped image
// sentinel error. The Together AI error envelope is decoded best-effort;
// when it fails the raw body is included verbatim.
func mapHTTPError(status int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	var env togetherErrorEnvelope
	if json.Unmarshal(body, &env) == nil && env.Error.Message != "" {
		msg = env.Error.Message
	}

	var sentinel error
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		sentinel = image.ErrAuthFailed
	case status == http.StatusTooManyRequests:
		sentinel = image.ErrRateLimited
	case status == http.StatusBadRequest:
		// Together returns 400 for content policy violations — surface as
		// content-filtered when the message hints at it; otherwise treat
		// as an invalid request.
		if isContentFilterMessage(msg) {
			sentinel = image.ErrContentFiltered
		} else {
			sentinel = image.ErrInvalidRequest
		}
	case status >= 500:
		sentinel = image.ErrProviderUnavailable
	default:
		sentinel = image.ErrProviderUnavailable
	}
	return fmt.Errorf("togetherai: HTTP %d: %s: %w", status, msg, sentinel)
}

func isContentFilterMessage(msg string) bool {
	low := strings.ToLower(msg)
	return strings.Contains(low, "safety") ||
		strings.Contains(low, "nsfw") ||
		strings.Contains(low, "content policy") ||
		strings.Contains(low, "moderation")
}

// Compile-time assertion that *Provider satisfies image.Provider.
var _ image.Provider = (*Provider)(nil)

// errProviderConfig is reserved for future configuration validation
// errors raised by [New]. Keeping the symbol stable lets callers
// errors.Is against it without an additional import path churn when
// validation lands.
var errProviderConfig = errors.New("togetherai: invalid provider config")

var _ = errProviderConfig // referenced for future validation
