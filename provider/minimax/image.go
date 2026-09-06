package minimax

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/samcharles93/ai-sdk/image"
	"github.com/samcharles93/ai-sdk/upload"
)

const defaultImageModel = "image-01"

// ImageOptions carries MiniMax-specific image generation options.
type ImageOptions struct {
	// Model overrides the default image model.
	Model string `json:"model,omitempty"`
	// AspectRatio is the target aspect ratio (e.g. "16:9"). When set it wins
	// over the request's [image.GenerateImageRequest.AspectRatio].
	AspectRatio string `json:"aspect_ratio,omitempty"`
}

// GenerateImage creates an image from a text prompt using MiniMax's
// text-to-image endpoint (/v1/image_generation). It returns the decoded
// image bytes and their detected media type. It satisfies image.Provider.
func (p *Provider) GenerateImage(ctx context.Context, req image.GenerateImageRequest) (image.GenerateImageResponse, error) {
	if req.Prompt == "" {
		return image.GenerateImageResponse{}, fmt.Errorf("minimax: prompt is required: %w", image.ErrInvalidRequest)
	}
	opts, err := image.ProviderOptionsFor[ImageOptions](req.ProviderOptions, "minimax")
	if err != nil {
		return image.GenerateImageResponse{}, fmt.Errorf("minimax: parse image provider options: %w", err)
	}
	model := req.Model
	if model == "" {
		model = opts.Model
	}
	if model == "" {
		model = defaultImageModel
	}
	ratio := req.AspectRatio
	if ratio == "" {
		ratio = opts.AspectRatio
	}
	if ratio == "" {
		ratio = "16:9"
	}

	// Always request base64 so the image bytes can be returned directly.
	// (A "url" response would return a separate image_url field; the
	// image.Provider contract carries bytes, so base64 is the natural fit.)
	body := map[string]any{
		"model":           model,
		"prompt":          req.Prompt,
		"aspect_ratio":    ratio,
		"response_format": "base64",
	}
	if req.N > 0 {
		body["n"] = req.N
	}
	if req.NegativePrompt != "" {
		body["negative_prompt"] = req.NegativePrompt
	}

	var out wireImageResponse
	if err := p.doJSON(ctx, http.MethodPost, "/v1/image_generation", body, &out, imageSentinels); err != nil {
		return image.GenerateImageResponse{}, err
	}
	if out.BaseResp != nil && out.BaseResp.StatusCode != 0 && out.BaseResp.StatusCode != http.StatusOK {
		return image.GenerateImageResponse{}, fmt.Errorf("minimax: image generation failed: %s", out.BaseResp.StatusMsg)
	}
	b64s := out.images()
	if len(b64s) == 0 {
		return image.GenerateImageResponse{}, fmt.Errorf("minimax: image generation returned no image")
	}

	resp := image.GenerateImageResponse{}
	for _, b64 := range b64s {
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return image.GenerateImageResponse{}, fmt.Errorf("minimax: decode image base64: %w", err)
		}
		mediaType := upload.DetectMediaType(data)
		if mediaType == "" {
			mediaType = "image/png"
		}
		resp.Images = append(resp.Images, image.GeneratedImage{
			Data:      data,
			Base64:    b64,
			MediaType: mediaType,
		})
	}
	return resp, nil
}

// wireImageResponse mirrors MiniMax's image-generation response. Most models
// return a single data.image_base64 string; an images array is accepted for
// back-compat with batch responses.
type wireImageResponse struct {
	BaseResp *struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp"`
	Data *struct {
		ImageBase64 string   `json:"image_base64"`
		Images      []string `json:"images"`
	} `json:"data"`
}

// images flattens the response into the list of base64 image strings.
func (w wireImageResponse) images() []string {
	if w.Data == nil {
		return nil
	}
	if len(w.Data.Images) > 0 {
		return w.Data.Images
	}
	if w.Data.ImageBase64 != "" {
		return []string{w.Data.ImageBase64}
	}
	return nil
}

// Compile-time assertion that *Provider satisfies image.Provider.
var _ image.Provider = (*Provider)(nil)
