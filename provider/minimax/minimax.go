// Package minimax provides access to MiniMax's video generation API
// (https://platform.minimax.io/docs/api-reference/video-generation-v2-create)
// via the video.Provider interface.
//
// Generation on MiniMax's side is asynchronous: a create call submits a job
// and returns a task_id, and the caller polls a query endpoint until the job
// reaches a terminal status ("succeeded"/"failed"/"cancelled"). GenerateVideo
// blocks, polling internally, until the video is ready or the poll phase
// times out, mirroring the pattern used by the xai provider.
//
// MiniMax supports text-to-video only in this implementation; edit /
// extend / reference-to-video modes are rejected with ErrInvalidRequest.
package minimax

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/samcharles93/ai-sdk/chat"
	errx "github.com/samcharles93/ai-sdk/error"
	"github.com/samcharles93/ai-sdk/image"
	"github.com/samcharles93/ai-sdk/music"
	"github.com/samcharles93/ai-sdk/speech"
	"github.com/samcharles93/ai-sdk/video"
)

const (
	defaultBaseURL = "https://api.minimax.io"

	// defaultModel is the only model documented for the v2 video endpoint.
	defaultModel = "MiniMax-H3"

	// defaultResolution and defaultDuration sit in the middle of MiniMax's
	// documented ranges (768P/2K; 4-15s) so an unconfigured request is
	// neither cheapest-lowest nor most expensive by default.
	defaultResolution = "768P"
	defaultDuration   = 6
	defaultRatio      = "16:9"

	// defaultPollInterval and defaultPollTimeout bound one generation.
	defaultPollInterval = 5 * time.Second
	defaultPollTimeout  = 5 * time.Minute
)

// Config configures a MiniMax Provider.
type Config struct {
	// APIKey authenticates every request as a Bearer token. Required.
	APIKey string
	// BaseURL overrides the API host. Empty uses defaultBaseURL.
	BaseURL string
	// HTTPClient overrides the client used for requests. If nil, requests are
	// bounded only by their caller contexts.
	HTTPClient *http.Client
}

// Provider is a video.Provider backed by the MiniMax video generation API.
type Provider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// New returns a new MiniMax Provider. It returns an error if APIKey is empty.
func New(cfg Config) (*Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("minimax: APIKey is required: %w", video.ErrInvalidRequest)
	}
	base := cfg.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	base = strings.TrimRight(base, "/")
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{}
	}
	return &Provider{apiKey: cfg.APIKey, baseURL: base, client: hc}, nil
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return "minimax" }

// VideoOptions carries MiniMax-specific video generation options.
type VideoOptions struct {
	// Resolution is the MiniMax-native resolution string ("768P" or "2K").
	// When set it wins over the request's standard Resolution mapping.
	Resolution string `json:"resolution,omitempty"`
	// PollIntervalMs controls the delay between status queries. Zero uses
	// defaultPollInterval.
	PollIntervalMs int `json:"poll_interval_ms,omitempty"`
	// PollTimeoutMs bounds the complete submit-and-poll phase. Zero uses
	// defaultPollTimeout.
	PollTimeoutMs int `json:"poll_timeout_ms,omitempty"`
}

// --- wire types ----------------------------------------------------------

type wireCreateResponse struct {
	TaskID string `json:"task_id"`
}

type wireTaskStatus struct {
	Status string `json:"status"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
	Content struct {
		URL string `json:"url"`
	} `json:"content"`
}

type wireQueryResponse struct {
	Task wireTaskStatus `json:"task"`
}

// --- Video Generation ----------------------------------------------------

// GenerateVideo submits a text-to-video job and polls until MiniMax reports a
// terminal status, returning the hosted video URL. It satisfies
// video.Provider.
func (p *Provider) GenerateVideo(ctx context.Context, req video.GenerateVideoRequest) (video.GenerateVideoResponse, error) {
	if req.Prompt == "" {
		return video.GenerateVideoResponse{}, fmt.Errorf("minimax: prompt is required: %w", video.ErrInvalidRequest)
	}
	if req.Mode != "" && req.Mode != video.VideoModeTextToVideo {
		return video.GenerateVideoResponse{}, fmt.Errorf("minimax: only text-to-video is supported: %w", video.ErrInvalidRequest)
	}
	if req.SourceVideo != "" || len(req.ReferenceImages) > 0 {
		return video.GenerateVideoResponse{}, fmt.Errorf("minimax: source video and reference images are not supported: %w", video.ErrInvalidRequest)
	}

	opts, err := video.ProviderOptionsFor[VideoOptions](req.ProviderOptions, "minimax")
	if err != nil {
		return video.GenerateVideoResponse{}, fmt.Errorf("minimax: parse video provider options: %w", err)
	}

	body, err := buildRequestBody(req, opts)
	if err != nil {
		return video.GenerateVideoResponse{}, err
	}

	taskID, err := p.create(ctx, body)
	if err != nil {
		return video.GenerateVideoResponse{}, err
	}

	pollInterval := time.Duration(opts.PollIntervalMs) * time.Millisecond
	if pollInterval == 0 {
		pollInterval = defaultPollInterval
	}
	pollTimeout := time.Duration(opts.PollTimeoutMs) * time.Millisecond
	if pollTimeout == 0 {
		pollTimeout = defaultPollTimeout
	}

	videoURL, err := p.poll(ctx, taskID, pollInterval, pollTimeout)
	if err != nil {
		return video.GenerateVideoResponse{}, err
	}

	return video.GenerateVideoResponse{
		Videos: []video.VideoResult{{URL: videoURL, MediaType: "video/mp4"}},
	}, nil
}

// buildRequestBody maps the provider-agnostic request and MiniMax-specific
// options onto MiniMax's create body.
func buildRequestBody(req video.GenerateVideoRequest, opts VideoOptions) (map[string]any, error) {
	model := req.Model
	if model == "" {
		model = defaultModel
	}

	duration := defaultDuration
	if raw := strings.TrimSpace(req.Duration); raw != "" {
		n, err := parseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("minimax: %w", err)
		}
		duration = n
	}
	// MiniMax caps duration at 4-15s; clamp a request that asks for more.
	if duration < 4 {
		duration = 4
	}
	if duration > 15 {
		duration = 15
	}

	resolution := opts.Resolution
	if resolution == "" {
		resolution = mapResolution(req.Resolution)
	}
	if resolution == "" {
		resolution = defaultResolution
	}

	ratio := req.Ratio
	if ratio == "" || ratio == "adaptive" {
		// MiniMax rejects "adaptive" for text-to-video; default to a common
		// ratio rather than forwarding an invalid value to the API.
		ratio = defaultRatio
	}
	if !allowedRatios[ratio] {
		return nil, fmt.Errorf("minimax: unsupported ratio %q; allowed: 16:9/4:3/1:1/3:4/9:16/21:9: %w", ratio, video.ErrInvalidRequest)
	}

	return map[string]any{
		"model":      model,
		"resolution": resolution,
		"duration":   duration,
		"ratio":      ratio,
		"content": []map[string]string{
			{"type": "text", "text": req.Prompt},
		},
	}, nil
}

// allowedRatios is the set of aspect ratios MiniMax accepts for text-to-video.
var allowedRatios = map[string]bool{
	"16:9": true,
	"4:3":  true,
	"1:1":  true,
	"3:4":  true,
	"9:16": true,
	"21:9": true,
}

// mapResolution maps a provider-agnostic resolution string to the nearest
// MiniMax value. MiniMax supports only 768P and 2K; unknown or low
// resolutions fall back to 768P, high ones to 2K.
func mapResolution(res string) string {
	switch strings.ToLower(strings.TrimSpace(res)) {
	case "", "768p", "720p", "1280x720", "854x480", "640x480", "480p":
		return "768P"
	case "2k", "1080p", "1920x1080", "2160p", "4k":
		return "2K"
	default:
		return "768P"
	}
}

// parseDuration parses a duration that is either "HH:MM:SS" or a plain
// seconds string into an integer number of seconds.
func parseDuration(s string) (int, error) {
	if strings.Contains(s, ":") {
		parts := strings.Split(s, ":")
		if len(parts) != 3 {
			return 0, fmt.Errorf("unparseable duration %q", s)
		}
		secs := 0
		for _, p := range parts {
			n, err := strconv.Atoi(p)
			if err != nil {
				return 0, fmt.Errorf("unparseable duration %q", s)
			}
			secs = secs*60 + n
		}
		return secs, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("unparseable duration %q", s)
	}
	return n, nil
}

// create submits a generation job and returns the task ID MiniMax assigned.
func (p *Provider) create(ctx context.Context, body map[string]any) (string, error) {
	var out wireCreateResponse
	if err := p.doJSON(ctx, http.MethodPost, "/v2/video_generation", body, &out, videoSentinels); err != nil {
		return "", err
	}
	if out.TaskID == "" {
		return "", fmt.Errorf("minimax: create response carried no task_id")
	}
	return out.TaskID, nil
}

// poll queries a job until it reaches a terminal status, a poll-phase
// timeout, or the caller's context is cancelled. It returns the hosted URL on
// success.
func (p *Provider) poll(ctx context.Context, taskID string, interval, timeout time.Duration) (string, error) {
	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-pollCtx.Done():
			return "", fmt.Errorf("minimax: task %s timed out waiting for completion: %w", taskID, pollCtx.Err())
		case <-ticker.C:
			task, err := p.query(pollCtx, taskID)
			if err != nil {
				// If the poll deadline expired while a query was in flight,
				// report the timeout rather than the underlying request error.
				if pollCtx.Err() != nil {
					return "", fmt.Errorf("minimax: task %s timed out waiting for completion: %w", taskID, pollCtx.Err())
				}
				return "", err
			}

			switch task.Status {
			case "succeeded":
				if task.Content.URL == "" {
					return "", fmt.Errorf("minimax: task %s succeeded but reported no video url", taskID)
				}
				return task.Content.URL, nil
			case "failed", "cancelled":
				return "", fmt.Errorf("minimax: task %s %s: %s", taskID, task.Status, task.errorMessage())
			}
			// "queued", "running", or unknown — keep polling.
		}
	}
}

// errorMessage reports the platform's own failure detail, falling back to a
// placeholder when MiniMax reports a terminal failure with no error object.
func (t wireTaskStatus) errorMessage() string {
	if t.Error == nil || t.Error.Message == "" {
		return "no error detail reported"
	}
	return t.Error.Message
}

// query fetches the current status of taskID.
func (p *Provider) query(ctx context.Context, taskID string) (wireTaskStatus, error) {
	var out wireQueryResponse
	path := "/v2/query/video_generation/" + taskID
	if err := p.doJSON(ctx, http.MethodGet, path, nil, &out, videoSentinels); err != nil {
		return wireTaskStatus{}, err
	}
	return out.Task, nil
}

// --- HTTP plumbing -------------------------------------------------------

// apiError is the documented error envelope MiniMax returns on a non-200.
type apiError struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// doJSON issues one request and decodes a 200 response into out. A non-200
// response is classified into a typed ProviderError carrying MiniMax's own
// message when present, so a create/query failure is never reported as a bare
// HTTP status. base maps a status code to the domain sentinel of the caller
// (video, speech, or image).
func (p *Provider) doJSON(ctx context.Context, method, path string, body, out any, base sentinelSet) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("minimax: encode request: %w", err)
		}
		reader = bytes.NewReader(data)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("minimax: build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("minimax: request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("minimax: read response: %w", err)
	}

	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("minimax: decode response: %w", err)
		}
		return nil
	}

	var msg string
	var ae apiError
	if err := json.Unmarshal(data, &ae); err == nil {
		msg = ae.Error.Message
	}
	if msg == "" {
		msg = chat.SanitizeErrorBody(data)
	}
	return errx.NewProviderError("minimax", resp, base.base(resp.StatusCode), msg, retryable(resp.StatusCode))
}

// sentinelSet holds the three status-mapped sentinels a domain uses so the
// shared doJSON can classify failures without knowing which domain
// (video/speech/image) the caller serves.
type sentinelSet struct {
	auth        error
	rateLimited error
	unavailable error
}

// base maps a MiniMax HTTP status code to the domain sentinel.
func (s sentinelSet) base(code int) error {
	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		return s.auth
	case http.StatusPaymentRequired, http.StatusTooManyRequests:
		return s.rateLimited
	default:
		return s.unavailable
	}
}

// Domain sentinel sets for the domains the minimax provider implements.
var (
	videoSentinels  = sentinelSet{video.ErrAuthFailed, video.ErrRateLimited, video.ErrProviderUnavailable}
	speechSentinels = sentinelSet{speech.ErrAuthFailed, speech.ErrRateLimited, speech.ErrProviderUnavailable}
	imageSentinels  = sentinelSet{image.ErrAuthFailed, image.ErrRateLimited, image.ErrProviderUnavailable}
	musicSentinels  = sentinelSet{music.ErrAuthFailed, music.ErrRateLimited, music.ErrProviderUnavailable}
)

// retryable reports whether a status code represents a transient failure
// worth retrying. 402 (insufficient balance) is not retried, since retrying
// cannot replenish the balance.
func retryable(code int) bool {
	if code == http.StatusTooManyRequests {
		return true
	}
	return code >= 500
}

// Compile-time assertion that *Provider satisfies video.Provider.
var _ video.Provider = (*Provider)(nil)
