// Package xai provides access to xAI's video generation API
// via the video.Provider interface.
package xai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/samcharles93/ai-sdk/chat"
	errx "github.com/samcharles93/ai-sdk/error"
	"github.com/samcharles93/ai-sdk/video"
)

const (
	videoGenerationsAPIPath = "/v1/videos/generations"
	videoStatusAPIPath      = "/v1/videos/"
	defaultPollInterval     = 5 * time.Second
)

// --- wire types ----------------------------------------------------------

type wireVideoCreateResponse struct {
	RequestID string `json:"request_id"`
}

type wireVideoStatusResponse struct {
	Status   string          `json:"status,omitempty"`
	Video    *wireVideoInfo  `json:"video,omitempty"`
	Model    string          `json:"model,omitempty"`
	Usage    *wireVideoUsage `json:"usage,omitempty"`
	Progress float64         `json:"progress,omitempty"`
	Error    *wireVideoError `json:"error,omitempty"`
}

type wireVideoInfo struct {
	URL               string  `json:"url,omitempty"`
	Duration          float64 `json:"duration,omitempty"`
	RespectModeration *bool   `json:"respect_moderation,omitempty"`
}

type wireVideoUsage struct {
	CostInUsdTicks float64 `json:"cost_in_usd_ticks,omitempty"`
}

type wireVideoError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// VideoOptions carries xAI-specific video generation options.
type VideoOptions struct {
	Resolution     string `json:"resolution,omitempty"`
	PollIntervalMs int    `json:"poll_interval_ms,omitempty"`
	// PollTimeoutMs optionally limits the complete polling phase. Zero leaves
	// polling bounded only by the GenerateVideo caller's context.
	PollTimeoutMs int `json:"poll_timeout_ms,omitempty"`
}

// videoCompatOptions captures the legacy untyped xAI video fields so callers
// can keep passing mode/video_url/reference_image_urls via the xAI
// ProviderOptions bucket. These fields are now first-class on
// video.GenerateVideoRequest; they only take effect when the typed request
// fields are unset.
type videoCompatOptions struct {
	Mode               string   `json:"mode,omitempty"`
	VideoURL           string   `json:"video_url,omitempty"`
	ReferenceImageURLs []string `json:"reference_image_urls,omitempty"`
}

// --- Video Generation ----------------------------------------------------

func pollContext(ctx context.Context, timeoutMS int) (context.Context, context.CancelFunc) {
	if timeoutMS <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
}

// GenerateVideo creates videos from text prompts using xAI's video generation API.
// It satisfies video.Provider.
func (p *Provider) GenerateVideo(ctx context.Context, req video.GenerateVideoRequest) (video.GenerateVideoResponse, error) {
	if req.Model == "" {
		return video.GenerateVideoResponse{}, fmt.Errorf("xai: model is required: %w", video.ErrInvalidRequest)
	}
	if req.Prompt == "" {
		return video.GenerateVideoResponse{}, fmt.Errorf("xai: prompt is required: %w", video.ErrInvalidRequest)
	}

	// Parse provider-specific knobs (resolution + polling).
	opts, err := video.ProviderOptionsFor[VideoOptions](req.ProviderOptions, "xai")
	if err != nil {
		return video.GenerateVideoResponse{}, fmt.Errorf("xai: parse video provider options: %w", err)
	}

	// Back-compat: read the legacy untyped mode/video_url/reference_image_urls
	// only when the corresponding typed request fields are unset.
	compat, err := video.ProviderOptionsFor[videoCompatOptions](req.ProviderOptions, "xai")
	if err != nil {
		return video.GenerateVideoResponse{}, fmt.Errorf("xai: parse video compat options: %w", err)
	}

	// Effective mode/source/references: typed request fields win over the
	// legacy ProviderOptions bucket.
	mode := string(req.Mode)
	if mode == "" {
		mode = compat.Mode
	}
	sourceVideo := req.SourceVideo
	if sourceVideo == "" {
		sourceVideo = compat.VideoURL
	}
	referenceImages := req.ReferenceImages
	if len(referenceImages) == 0 {
		referenceImages = compat.ReferenceImageURLs
	}

	// Build request body.
	body := map[string]any{
		"model":  req.Model,
		"prompt": req.Prompt,
	}

	// Duration.
	if req.Duration != "" {
		body["duration"] = req.Duration
	}

	// Resolution mapping for xAI.
	if req.Resolution != "" {
		res := mapResolution(req.Resolution)
		if res != "" {
			body["resolution"] = res
		}
	}
	if opts.Resolution != "" {
		body["resolution"] = opts.Resolution
	}

	// Aspect ratio.
	if req.Ratio != "" {
		body["ratio"] = req.Ratio
	}

	// Video editing/extension: pass the source video URL.
	if sourceVideo != "" && (mode == "edit-video" || mode == "extend-video") {
		body["video"] = map[string]any{"url": sourceVideo}
	}

	// Reference images for reference-to-video mode.
	if mode == "reference-to-video" && len(referenceImages) > 0 {
		refs := make([]map[string]string, len(referenceImages))
		for i, url := range referenceImages {
			refs[i] = map[string]string{"url": url}
		}
		body["reference_images"] = refs
	}

	// Determine endpoint based on the effective mode.
	endpoint := p.baseURL + videoGenerationsAPIPath
	switch mode {
	case "edit-video":
		endpoint = p.baseURL + "/v1/videos/edits"
	case "extend-video":
		endpoint = p.baseURL + "/v1/videos/extensions"
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return video.GenerateVideoResponse{}, fmt.Errorf("xai: marshal video request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(buf))
	if err != nil {
		return video.GenerateVideoResponse{}, fmt.Errorf("xai: build video request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return video.GenerateVideoResponse{}, fmt.Errorf("xai: http do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return video.GenerateVideoResponse{}, classifyVideoHTTPError(resp)
	}

	var createResp wireVideoCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
		return video.GenerateVideoResponse{}, fmt.Errorf("xai: decode create response: %w", err)
	}

	requestID := createResp.RequestID
	if requestID == "" {
		return video.GenerateVideoResponse{}, fmt.Errorf("xai: no request_id returned")
	}

	// Poll for completion.
	pollInterval := time.Duration(opts.PollIntervalMs) * time.Millisecond
	if pollInterval == 0 {
		pollInterval = defaultPollInterval
	}

	pollCtx, cancel := pollContext(ctx, opts.PollTimeoutMs)
	defer cancel()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-pollCtx.Done():
			return video.GenerateVideoResponse{}, fmt.Errorf("xai: video generation timed out: %w", pollCtx.Err())
		case <-ticker.C:
			status, err := p.pollVideoStatus(pollCtx, requestID)
			if err != nil {
				return video.GenerateVideoResponse{}, err
			}

			switch status.Status {
			case "done":
				if status.Video == nil || status.Video.URL == "" {
					return video.GenerateVideoResponse{}, fmt.Errorf("xai: video generation completed but no URL returned")
				}
				if status.Video.RespectModeration != nil && !*status.Video.RespectModeration {
					return video.GenerateVideoResponse{}, fmt.Errorf("xai: video generation blocked by content policy")
				}
				return video.GenerateVideoResponse{
					Videos: []video.VideoResult{
						{
							URL:       status.Video.URL,
							MediaType: "video/mp4",
						},
					},
				}, nil

			case "expired":
				return video.GenerateVideoResponse{}, fmt.Errorf("xai: video generation request expired")

			case "failed":
				if status.Error != nil {
					return video.GenerateVideoResponse{}, fmt.Errorf("xai: video generation failed: %s (code: %s)", status.Error.Message, status.Error.Code)
				}
				return video.GenerateVideoResponse{}, fmt.Errorf("xai: video generation failed")

				// "pending" or unknown - continue polling
			}
		}
	}
}

// pollVideoStatus checks the status of a video generation request.
func (p *Provider) pollVideoStatus(ctx context.Context, requestID string) (*wireVideoStatusResponse, error) {
	url := p.baseURL + videoStatusAPIPath + requestID
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("xai: build status request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("xai: http do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, classifyVideoHTTPError(resp)
	}

	var status wireVideoStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("xai: decode status response: %w", err)
	}

	return &status, nil
}

// mapResolution converts standard resolution strings to xAI format.
func mapResolution(res string) string {
	switch res {
	case "1920x1080", "1280x720":
		return "720p"
	case "854x480", "640x480":
		return "480p"
	default:
		return ""
	}
}

func classifyVideoHTTPError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	snippet := chat.SanitizeErrorBody(b)
	code := resp.StatusCode
	var base error
	retryable := false
	switch {
	case code == 401 || code == 403:
		base = video.ErrAuthFailed
	case code == 429:
		base = video.ErrRateLimited
		retryable = true
	case code >= 500:
		base = video.ErrProviderUnavailable
		retryable = true
	default:
		base = video.ErrProviderUnavailable
		retryable = true
	}
	return errx.NewProviderError("xai", resp, base, snippet, retryable)
}

// Compile-time assertion that *Provider satisfies video.Provider.
var _ video.Provider = (*Provider)(nil)
