// Package xai provides access to xAI's image generation API
// via the image.Provider interface.
package xai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/image"
)

const (
	imageGenerationsAPIPath = "/v1/images/generations"
	imageEditsAPIPath       = "/v1/images/edits"
)

// --- wire types ----------------------------------------------------------

type wireImageRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	N              int    `json:"n,omitempty"`
	AspectRatio    string `json:"aspect_ratio,omitempty"`
	OutputFormat   string `json:"output_format,omitempty"`
	Quality        string `json:"quality,omitempty"`
	SyncMode       bool   `json:"sync_mode,omitempty"`
	Resolution     string `json:"resolution,omitempty"`
	User           string `json:"user,omitempty"`
	ResponseFormat string `json:"response_format"`
}

type wireImageDatum struct {
	URL           string `json:"url,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

type wireImageResponse struct {
	Data  []wireImageDatum `json:"data"`
	Usage *wireImageUsage  `json:"usage,omitempty"`
}

type wireImageUsage struct {
	CostInUsdTicks float64 `json:"cost_in_usd_ticks,omitempty"`
}

// ImageOptions carries xAI-specific image generation options.
type ImageOptions struct {
	OutputFormat string `json:"output_format,omitempty"`
	SyncMode     bool   `json:"sync_mode,omitempty"`
	AspectRatio  string `json:"aspect_ratio,omitempty"`
	Resolution   string `json:"resolution,omitempty"`
	Quality      string `json:"quality,omitempty"`
	User         string `json:"user,omitempty"`
}

// --- Image Generation -----------------------------------------------------

// GenerateImage creates images from text prompts using xAI's image generation API.
// It satisfies image.Provider.
func (p *Provider) GenerateImage(ctx context.Context, req image.GenerateImageRequest) (image.GenerateImageResponse, error) {
	if req.Model == "" {
		return image.GenerateImageResponse{}, fmt.Errorf("xai: model is required: %w", image.ErrInvalidRequest)
	}
	if req.Prompt == "" {
		return image.GenerateImageResponse{}, fmt.Errorf("xai: prompt is required: %w", image.ErrInvalidRequest)
	}

	body := wireImageRequest{
		Model:          req.Model,
		Prompt:         req.Prompt,
		ResponseFormat: "b64_json",
	}

	// Number of images.
	if req.N > 0 {
		body.N = req.N
	} else {
		body.N = 1
	}

	// Aspect ratio (xAI uses aspect_ratio).
	if req.AspectRatio != "" {
		body.AspectRatio = req.AspectRatio
	}

	// Parse provider options for xAI-specific settings.
	opts, err := image.ProviderOptionsFor[ImageOptions](req.ProviderOptions, "xai")
	if err != nil {
		return image.GenerateImageResponse{}, fmt.Errorf("xai: parse provider options: %w", err)
	}
	if opts.OutputFormat != "" {
		body.OutputFormat = opts.OutputFormat
	}
	if opts.SyncMode {
		body.SyncMode = true
	}
	if opts.AspectRatio != "" && body.AspectRatio == "" {
		body.AspectRatio = opts.AspectRatio
	}
	if opts.Resolution != "" {
		body.Resolution = opts.Resolution
	}
	if opts.Quality != "" {
		body.Quality = opts.Quality
	}
	if opts.User != "" {
		body.User = opts.User
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return image.GenerateImageResponse{}, fmt.Errorf("xai: marshal image request: %w", err)
	}

	url := p.baseURL + imageGenerationsAPIPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return image.GenerateImageResponse{}, fmt.Errorf("xai: build image request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return image.GenerateImageResponse{}, fmt.Errorf("xai: http do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		snippet := chat.SanitizeErrorBody(respBody)
		return image.GenerateImageResponse{}, classifyImageHTTPError(resp.StatusCode, snippet)
	}

	var wr wireImageResponse
	if err := json.NewDecoder(resp.Body).Decode(&wr); err != nil {
		return image.GenerateImageResponse{}, fmt.Errorf("xai: decode image response: %w", err)
	}

	out := image.GenerateImageResponse{
		Images: make([]image.GeneratedImage, len(wr.Data)),
	}

	// Derive media type from requested output format.
	mediaType := "image/png" // default
	if body.OutputFormat != "" {
		mediaType = outputFormatToMediaType(body.OutputFormat)
	}

	for i, d := range wr.Data {
		img := image.GeneratedImage{}
		if d.B64JSON != "" {
			img.Base64 = d.B64JSON
			img.MediaType = mediaType
		}
		if d.URL != "" {
			img.URL = d.URL
		}
		out.Images[i] = img
	}

	return out, nil
}

func classifyImageHTTPError(code int, body string) error {
	var base error
	switch {
	case code == 401 || code == 403:
		base = image.ErrAuthFailed
	case code == 429:
		base = image.ErrRateLimited
	case code >= 500:
		base = image.ErrProviderUnavailable
	default:
		base = image.ErrProviderUnavailable
	}
	return fmt.Errorf("xai: status %d: %s: %w", code, body, base)
}

// Compile-time assertion that *Provider satisfies image.Provider.
var _ image.Provider = (*Provider)(nil)

// outputFormatToMediaType maps xAI output_format values to MIME types.
func outputFormatToMediaType(format string) string {
	switch format {
	case "png":
		return "image/png"
	case "jpeg", "jpg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	case "gif":
		return "image/gif"
	default:
		return "image/png"
	}
}
