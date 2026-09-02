// Package xai provides access to xAI's image generation API
// via the image.Provider interface.
package xai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/samcharles93/ai-sdk/chat"
	errx "github.com/samcharles93/ai-sdk/error"
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
		return image.GenerateImageResponse{}, classifyImageHTTPError(resp)
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

// EditImage edits one or more existing images using xAI's image edits API.
// The source image (and optional mask) are sent as multipart file parts; it
// satisfies image.Editor.
func (p *Provider) EditImage(ctx context.Context, req image.EditImageRequest) (image.EditImageResponse, error) {
	if req.Model == "" {
		return image.EditImageResponse{}, fmt.Errorf("xai: model is required: %w", image.ErrInvalidRequest)
	}
	if req.Prompt == "" {
		return image.EditImageResponse{}, fmt.Errorf("xai: prompt is required: %w", image.ErrInvalidRequest)
	}
	if len(req.Image.Data) == 0 && req.Image.URL == "" {
		return image.EditImageResponse{}, fmt.Errorf("xai: source image is required: %w", image.ErrInvalidRequest)
	}

	opts, err := image.ProviderOptionsFor[ImageOptions](req.ProviderOptions, "xai")
	if err != nil {
		return image.EditImageResponse{}, fmt.Errorf("xai: parse provider options: %w", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("model", req.Model); err != nil {
		return image.EditImageResponse{}, fmt.Errorf("xai: write model field: %w", err)
	}
	if err := writer.WriteField("prompt", req.Prompt); err != nil {
		return image.EditImageResponse{}, fmt.Errorf("xai: write prompt field: %w", err)
	}
	if req.N > 0 {
		if err := writer.WriteField("n", strconv.Itoa(req.N)); err != nil {
			return image.EditImageResponse{}, fmt.Errorf("xai: write n field: %w", err)
		}
	}
	if req.Size != "" {
		if err := writer.WriteField("size", req.Size); err != nil {
			return image.EditImageResponse{}, fmt.Errorf("xai: write size field: %w", err)
		}
	}
	if opts.OutputFormat != "" {
		if err := writer.WriteField("response_format", opts.OutputFormat); err != nil {
			return image.EditImageResponse{}, fmt.Errorf("xai: write response_format field: %w", err)
		}
	}

	if len(req.Image.Data) > 0 {
		part, err := writer.CreateFormFile("image", sourceImageFilename(req.Image))
		if err != nil {
			return image.EditImageResponse{}, fmt.Errorf("xai: build image part: %w", err)
		}
		if _, err := part.Write(req.Image.Data); err != nil {
			return image.EditImageResponse{}, fmt.Errorf("xai: write image part: %w", err)
		}
	}
	if len(req.Mask.Data) > 0 {
		part, err := writer.CreateFormFile("mask", sourceImageFilename(req.Mask))
		if err != nil {
			return image.EditImageResponse{}, fmt.Errorf("xai: build mask part: %w", err)
		}
		if _, err := part.Write(req.Mask.Data); err != nil {
			return image.EditImageResponse{}, fmt.Errorf("xai: write mask part: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return image.EditImageResponse{}, fmt.Errorf("xai: close multipart body: %w", err)
	}

	url := p.baseURL + imageEditsAPIPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return image.EditImageResponse{}, fmt.Errorf("xai: build edit request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", writer.FormDataContentType())
	httpReq.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return image.EditImageResponse{}, fmt.Errorf("xai: http do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return image.EditImageResponse{}, classifyImageHTTPError(resp)
	}

	var wr wireImageResponse
	if err := json.NewDecoder(resp.Body).Decode(&wr); err != nil {
		return image.EditImageResponse{}, fmt.Errorf("xai: decode edit response: %w", err)
	}

	out := image.EditImageResponse{
		Images: make([]image.GeneratedImage, len(wr.Data)),
	}
	mediaType := outputFormatToMediaType(opts.OutputFormat)
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

func classifyImageHTTPError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	snippet := chat.SanitizeErrorBody(b)
	code := resp.StatusCode
	bodyLower := strings.ToLower(snippet)
	var base error
	retryable := false
	switch {
	case code == 401 || code == 403:
		base = image.ErrAuthFailed
	case code == 429:
		base = image.ErrRateLimited
		retryable = true
	case code >= 500:
		base = image.ErrProviderUnavailable
		retryable = true
	case code == 400 && isContentFiltered(bodyLower):
		base = image.ErrContentFiltered
	default:
		base = image.ErrProviderUnavailable
		retryable = true
	}
	return errx.NewProviderError("xai", resp, base, snippet, retryable)
}

// isContentFiltered reports whether a sanitised, lower-cased error body
// indicates a content-filter/safety rejection (typically HTTP 400).
func isContentFiltered(bodyLower string) bool {
	for _, marker := range []string{
		"content_filter",
		"content_policy_violation",
		"content policy",
		"safety system",
		"filtered",
	} {
		if strings.Contains(bodyLower, marker) {
			return true
		}
	}
	return false
}

// Compile-time assertions that *Provider satisfies image.Provider and
// image.Editor.
var _ image.Provider = (*Provider)(nil)
var _ image.Editor = (*Provider)(nil)

// sourceImageFilename derives a multipart filename for an image/mask part
// from its media type, defaulting to PNG.
func sourceImageFilename(src image.EditImageSource) string {
	switch src.MediaType {
	case "image/jpeg", "image/jpg":
		return "image.jpg"
	case "image/webp":
		return "image.webp"
	case "image/gif":
		return "image.gif"
	default:
		return "image.png"
	}
}

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
