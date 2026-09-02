package xai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samcharles93/ai-sdk/video"
)

// startVideoSrv returns a test server that records the POST create path/body
// and answers status polling with a completed video. It mirrors the pattern
// used elsewhere in this package.
func startVideoSrv(t *testing.T) (*httptest.Server, *string, *map[string]any) {
	t.Helper()
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			gotPath = r.URL.Path
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"request_id":"req-1"}`)
			return
		}
		// Status polling -> completed successfully on first poll.
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"done","video":{"url":"http://example.com/out.mp4"}}`)
	}))
	return srv, &gotPath, &gotBody
}

func TestGenerateVideo_EditVideo_Typed(t *testing.T) {
	srv, gotPath, gotBody := startVideoSrv(t)
	defer srv.Close()

	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.GenerateVideo(context.Background(), video.GenerateVideoRequest{
		Model:       "grok-video",
		Prompt:      "make it cinematic",
		Mode:        video.VideoModeEditVideo,
		SourceVideo: "http://example.com/src.mp4",
		ProviderOptions: map[string]any{
			"xai": VideoOptions{PollIntervalMs: 1},
		},
	})
	if err != nil {
		t.Fatalf("GenerateVideo: %v", err)
	}

	if *gotPath != "/v1/videos/edits" {
		t.Errorf("path = %q, want /v1/videos/edits", *gotPath)
	}
	if (*gotBody)["model"] != "grok-video" {
		t.Errorf("model = %v", (*gotBody)["model"])
	}
	videoBody, _ := (*gotBody)["video"].(map[string]any)
	if videoBody == nil || videoBody["url"] != "http://example.com/src.mp4" {
		t.Errorf("video = %v, want url http://example.com/src.mp4", (*gotBody)["video"])
	}
}

func TestGenerateVideo_ExtendVideo_Typed(t *testing.T) {
	srv, gotPath, gotBody := startVideoSrv(t)
	defer srv.Close()

	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.GenerateVideo(context.Background(), video.GenerateVideoRequest{
		Model:       "grok-video",
		Prompt:      "extend by 5s",
		Mode:        video.VideoModeExtendVideo,
		SourceVideo: "http://example.com/src.mp4",
		ProviderOptions: map[string]any{
			"xai": VideoOptions{PollIntervalMs: 1},
		},
	})
	if err != nil {
		t.Fatalf("GenerateVideo: %v", err)
	}

	if *gotPath != "/v1/videos/extensions" {
		t.Errorf("path = %q, want /v1/videos/extensions", *gotPath)
	}
	videoBody, _ := (*gotBody)["video"].(map[string]any)
	if videoBody == nil || videoBody["url"] != "http://example.com/src.mp4" {
		t.Errorf("video = %v, want url http://example.com/src.mp4", (*gotBody)["video"])
	}
}

func TestGenerateVideo_ReferenceToVideo_Typed(t *testing.T) {
	srv, gotPath, gotBody := startVideoSrv(t)
	defer srv.Close()

	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	refs := []string{"http://example.com/ref1.png", "http://example.com/ref2.png"}
	_, err := p.GenerateVideo(context.Background(), video.GenerateVideoRequest{
		Model:           "grok-video",
		Prompt:          "follow the reference",
		Mode:            video.VideoModeReferenceToVideo,
		ReferenceImages: refs,
		ProviderOptions: map[string]any{
			"xai": VideoOptions{PollIntervalMs: 1},
		},
	})
	if err != nil {
		t.Fatalf("GenerateVideo: %v", err)
	}

	if *gotPath != "/v1/videos/generations" {
		t.Errorf("path = %q, want /v1/videos/generations", *gotPath)
	}
	refsBody, _ := (*gotBody)["reference_images"].([]any)
	if len(refsBody) != 2 {
		t.Fatalf("reference_images = %v, want 2 entries", (*gotBody)["reference_images"])
	}
	for i, want := range refs {
		m, _ := refsBody[i].(map[string]any)
		if m == nil || m["url"] != want {
			t.Errorf("reference_images[%d] = %v, want url %s", i, refsBody[i], want)
		}
	}
}

func TestGenerateVideo_Ratio_Forwarded(t *testing.T) {
	srv, _, gotBody := startVideoSrv(t)
	defer srv.Close()

	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.GenerateVideo(context.Background(), video.GenerateVideoRequest{
		Model:  "grok-video",
		Prompt: "portrait",
		Ratio:  "16:9",
		ProviderOptions: map[string]any{
			"xai": VideoOptions{PollIntervalMs: 1},
		},
	})
	if err != nil {
		t.Fatalf("GenerateVideo: %v", err)
	}

	if (*gotBody)["ratio"] != "16:9" {
		t.Errorf("ratio = %v, want 16:9", (*gotBody)["ratio"])
	}
}

func TestGenerateVideo_BackCompatFallback(t *testing.T) {
	srv, gotPath, gotBody := startVideoSrv(t)
	defer srv.Close()

	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.GenerateVideo(context.Background(), video.GenerateVideoRequest{
		Model:  "grok-video",
		Prompt: "edited from legacy config",
		ProviderOptions: map[string]any{
			"xai": map[string]any{
				"mode":                 "edit-video",
				"video_url":            "http://example.com/legacy.mp4",
				"reference_image_urls": []any{"http://example.com/legacy.png"},
				"poll_interval_ms":     1,
			},
		},
	})
	if err != nil {
		t.Fatalf("GenerateVideo: %v", err)
	}

	if *gotPath != "/v1/videos/edits" {
		t.Errorf("path = %q, want /v1/videos/edits (from fallback mode)", *gotPath)
	}
	videoBody, _ := (*gotBody)["video"].(map[string]any)
	if videoBody == nil || videoBody["url"] != "http://example.com/legacy.mp4" {
		t.Errorf("video = %v, want url http://example.com/legacy.mp4 (from fallback video_url)", (*gotBody)["video"])
	}
}

func TestGenerateVideo_TypedOverFallback_Precedence(t *testing.T) {
	srv, gotPath, gotBody := startVideoSrv(t)
	defer srv.Close()

	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.GenerateVideo(context.Background(), video.GenerateVideoRequest{
		Model:           "grok-video",
		Prompt:          "typed wins",
		Mode:            video.VideoModeExtendVideo,
		SourceVideo:     "http://example.com/typed.mp4",
		ReferenceImages: []string{"http://example.com/typed.png"},
		ProviderOptions: map[string]any{
			"xai": map[string]any{
				"mode":                 "edit-video",
				"video_url":            "http://example.com/fallback.mp4",
				"reference_image_urls": []any{"http://example.com/fallback.png"},
				"poll_interval_ms":     1,
			},
		},
	})
	if err != nil {
		t.Fatalf("GenerateVideo: %v", err)
	}

	// Endpoint driven by typed mode (extend-video) -> /v1/videos/extensions,
	// not the fallback edit-video path.
	if *gotPath != "/v1/videos/extensions" {
		t.Errorf("path = %q, want /v1/videos/extensions (typed mode wins)", *gotPath)
	}
	// Source video from typed fields.
	videoBody, _ := (*gotBody)["video"].(map[string]any)
	if videoBody == nil || videoBody["url"] != "http://example.com/typed.mp4" {
		t.Errorf("video = %v, want url http://example.com/typed.mp4 (typed source wins)", (*gotBody)["video"])
	}
	// Reference images from typed fields (extend mode, so reference images
	// should not leak into the body).
	if _, ok := (*gotBody)["reference_images"]; ok {
		t.Errorf("reference_images present for extend mode: %v", (*gotBody)["reference_images"])
	}
}
