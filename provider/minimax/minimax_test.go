package minimax

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/samcharles93/ai-sdk/video"
)

// fakeAPI serves a MiniMax API double. create answers with taskID (or
// createStatus/createBody when set); each call to query pops the next entry
// from queryResponses, repeating the last one once exhausted.
type fakeAPI struct {
	t              *testing.T
	createStatus   int
	createBody     string
	taskID         string
	queryResponses []string
	queryCalls     int
	lastAuth       string
	lastCreateBody map[string]any
}

func (f *fakeAPI) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.lastAuth = r.Header.Get("Authorization")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/v2/video_generation"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				f.t.Fatalf("decode create body: %v", err)
			}
			f.lastCreateBody = body

			if f.createStatus != 0 && f.createStatus != http.StatusOK {
				w.WriteHeader(f.createStatus)
				_, _ = w.Write([]byte(f.createBody))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"task_id":"` + f.taskID + `"}`))

		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/v2/query/video_generation/"):
			idx := f.queryCalls
			if idx >= len(f.queryResponses) {
				idx = len(f.queryResponses) - 1
			}
			f.queryCalls++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(f.queryResponses[idx]))

		default:
			f.t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
}

func newTestClient(t *testing.T, api *fakeAPI) *Provider {
	t.Helper()
	srv := api.server()
	t.Cleanup(srv.Close)
	p, err := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

func providerOptions(ms ...int) map[string]any {
	opts := map[string]any{}
	if len(ms) > 0 {
		opts["poll_interval_ms"] = ms[0]
	}
	if len(ms) > 1 {
		opts["poll_timeout_ms"] = ms[1]
	}
	return map[string]any{"minimax": opts}
}

func TestGenerateVideo_ReturnsURLOnSuccess(t *testing.T) {
	api := &fakeAPI{t: t, taskID: "task-1", queryResponses: []string{
		`{"task":{"id":"task-1","status":"succeeded","content":{"url":"https://cdn.example.com/v.mp4"}}}`,
	}}
	p := newTestClient(t, api)

	resp, err := p.GenerateVideo(context.Background(), video.GenerateVideoRequest{
		Prompt:          "a cat on a skateboard",
		ProviderOptions: providerOptions(1, 5000),
	})
	if err != nil {
		t.Fatalf("GenerateVideo: %v", err)
	}
	if len(resp.Videos) != 1 {
		t.Fatalf("videos = %d, want 1", len(resp.Videos))
	}
	if resp.Videos[0].URL != "https://cdn.example.com/v.mp4" {
		t.Errorf("url = %q, want the succeeded task's content url", resp.Videos[0].URL)
	}
	if resp.Videos[0].MediaType != "video/mp4" {
		t.Errorf("media type = %q, want video/mp4", resp.Videos[0].MediaType)
	}
	if api.lastAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want Bearer test-key", api.lastAuth)
	}
	content, _ := api.lastCreateBody["content"].([]any)
	if len(content) != 1 {
		t.Fatal("create body carried no content")
	}
	if api.lastCreateBody["model"] != "MiniMax-H3" {
		t.Errorf("model = %v, want MiniMax-H3", api.lastCreateBody["model"])
	}
	if api.lastCreateBody["resolution"] != "768P" {
		t.Errorf("resolution = %v, want 768P", api.lastCreateBody["resolution"])
	}
	if api.lastCreateBody["duration"] != float64(6) {
		t.Errorf("duration = %v, want 6", api.lastCreateBody["duration"])
	}
	if api.lastCreateBody["ratio"] != "16:9" {
		t.Errorf("ratio = %v, want 16:9 (adaptive is rejected by MiniMax)", api.lastCreateBody["ratio"])
	}
}

func TestGenerateVideo_PollsUntilTerminal(t *testing.T) {
	api := &fakeAPI{t: t, taskID: "task-1", queryResponses: []string{
		`{"task":{"id":"task-1","status":"queued"}}`,
		`{"task":{"id":"task-1","status":"running"}}`,
		`{"task":{"id":"task-1","status":"succeeded","content":{"url":"https://cdn.example.com/v.mp4"}}}`,
	}}
	p := newTestClient(t, api)

	_, err := p.GenerateVideo(context.Background(), video.GenerateVideoRequest{
		Prompt:          "a cat on a skateboard",
		ProviderOptions: providerOptions(1, 5000),
	})
	if err != nil {
		t.Fatalf("GenerateVideo: %v", err)
	}
	if api.queryCalls != 3 {
		t.Errorf("query calls = %d, want 3 (queued, running, succeeded)", api.queryCalls)
	}
}

func TestGenerateVideo_FailedStatusSurfacesMessage(t *testing.T) {
	api := &fakeAPI{t: t, taskID: "task-1", queryResponses: []string{
		`{"task":{"id":"task-1","status":"failed","error":{"code":"content_policy","message":"prompt rejected"}}}`,
	}}
	p := newTestClient(t, api)

	_, err := p.GenerateVideo(context.Background(), video.GenerateVideoRequest{
		Prompt:          "a cat on a skateboard",
		ProviderOptions: providerOptions(1, 5000),
	})
	if err == nil {
		t.Fatal("expected an error for a failed task")
	}
	if !strings.Contains(err.Error(), "prompt rejected") {
		t.Errorf("error = %v, want it to contain the platform's message", err)
	}
}

func TestGenerateVideo_CreateFailures(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantSubstr string
		wantBase   error
	}{
		{
			name:       "auth",
			status:     http.StatusUnauthorized,
			body:       `{"type":"error","error":{"type":"auth_error","message":"invalid API key","http_code":"401"},"request_id":"r1"}`,
			wantSubstr: "invalid API key",
			wantBase:   video.ErrAuthFailed,
		},
		{
			name:       "insufficient balance",
			status:     http.StatusPaymentRequired,
			body:       `{"type":"error","error":{"type":"balance_error","message":"insufficient balance","http_code":"402"},"request_id":"r2"}`,
			wantSubstr: "insufficient balance",
			wantBase:   video.ErrRateLimited,
		},
		{
			name:       "rate limited",
			status:     http.StatusTooManyRequests,
			body:       `{"type":"error","error":{"type":"rate_limit","message":"too many requests","http_code":"429"},"request_id":"r3"}`,
			wantSubstr: "too many requests",
			wantBase:   video.ErrRateLimited,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeAPI{t: t, createStatus: tt.status, createBody: tt.body}
			p := newTestClient(t, api)

			_, err := p.GenerateVideo(context.Background(), video.GenerateVideoRequest{
				Prompt:          "a cat",
				ProviderOptions: providerOptions(1, 5000),
			})
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Errorf("error = %v, want it to contain %q", err, tt.wantSubstr)
			}
			if !errors.Is(err, tt.wantBase) {
				t.Errorf("error = %v, want errors.Is(err, %v)", err, tt.wantBase)
			}
		})
	}
}

func TestGenerateVideo_TimesOut(t *testing.T) {
	api := &fakeAPI{t: t, taskID: "task-1", queryResponses: []string{
		`{"task":{"id":"task-1","status":"running"}}`,
	}}
	p := newTestClient(t, api)

	_, err := p.GenerateVideo(context.Background(), video.GenerateVideoRequest{
		Prompt:          "a cat",
		ProviderOptions: providerOptions(1, 100),
	})
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %v, want a timeout error", err)
	}
}

func TestGenerateVideo_RejectsNonTextToVideo(t *testing.T) {
	p := newTestClient(t, &fakeAPI{t: t})
	_, err := p.GenerateVideo(context.Background(), video.GenerateVideoRequest{
		Prompt: "a cat",
		Mode:   video.VideoModeEditVideo,
	})
	if !errors.Is(err, video.ErrInvalidRequest) {
		t.Errorf("expected ErrInvalidRequest for edit mode, got %v", err)
	}
}

func TestGenerateVideo_MapsRequestFields(t *testing.T) {
	tests := []struct {
		name           string
		req            video.GenerateVideoRequest
		wantModel      string
		wantResolution string
		wantDuration   float64
		wantRatio      string
	}{
		{
			name:           "seconds duration and 1080p",
			req:            video.GenerateVideoRequest{Prompt: "p", Duration: "10", Resolution: "1920x1080"},
			wantModel:      "MiniMax-H3",
			wantResolution: "2K",
			wantDuration:   10,
			wantRatio:      "16:9",
		},
		{
			name:           "HH:MM:SS duration clamped down",
			req:            video.GenerateVideoRequest{Prompt: "p", Duration: "00:00:02", Resolution: "720p"},
			wantModel:      "MiniMax-H3",
			wantResolution: "768P",
			wantDuration:   4,
			wantRatio:      "16:9",
		},
		{
			name:           "long duration clamped up",
			req:            video.GenerateVideoRequest{Prompt: "p", Duration: "30"},
			wantModel:      "MiniMax-H3",
			wantResolution: "768P",
			wantDuration:   15,
			wantRatio:      "16:9",
		},
		{
			name:           "explicit ratio",
			req:            video.GenerateVideoRequest{Prompt: "p", Ratio: "9:16"},
			wantModel:      "MiniMax-H3",
			wantResolution: "768P",
			wantDuration:   6,
			wantRatio:      "9:16",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeAPI{t: t, taskID: "task-1", queryResponses: []string{
				`{"task":{"id":"task-1","status":"succeeded","content":{"url":"https://cdn.example.com/v.mp4"}}}`,
			}}
			p := newTestClient(t, api)

			req := tt.req
			req.ProviderOptions = providerOptions(1, 5000)
			_, err := p.GenerateVideo(context.Background(), req)
			if err != nil {
				t.Fatalf("GenerateVideo: %v", err)
			}
			if api.lastCreateBody["model"] != tt.wantModel {
				t.Errorf("model = %v, want %q", api.lastCreateBody["model"], tt.wantModel)
			}
			if api.lastCreateBody["resolution"] != tt.wantResolution {
				t.Errorf("resolution = %v, want %q", api.lastCreateBody["resolution"], tt.wantResolution)
			}
			if api.lastCreateBody["duration"] != tt.wantDuration {
				t.Errorf("duration = %v, want %v", api.lastCreateBody["duration"], tt.wantDuration)
			}
			if api.lastCreateBody["ratio"] != tt.wantRatio {
				t.Errorf("ratio = %v, want %q", api.lastCreateBody["ratio"], tt.wantRatio)
			}
		})
	}
}

func TestGenerateVideo_RejectsInvalidRatio(t *testing.T) {
	api := &fakeAPI{t: t, taskID: "task-1", queryResponses: []string{
		`{"task":{"id":"task-1","status":"succeeded","content":{"url":"https://cdn.example.com/v.mp4"}}}`,
	}}
	p := newTestClient(t, api)

	// "adaptive" is invalid for text-to-video but is the legacy default, so it
	// must be coerced to a valid default, not rejected.
	resp, err := p.GenerateVideo(context.Background(), video.GenerateVideoRequest{
		Prompt:          "a cat",
		Ratio:           "adaptive",
		ProviderOptions: providerOptions(1, 5000),
	})
	if err != nil {
		t.Fatalf("adaptive ratio should be coerced: %v", err)
	}
	if len(resp.Videos) != 1 {
		t.Fatalf("videos = %d, want 1", len(resp.Videos))
	}

	// A genuinely invalid ratio must be rejected locally.
	_, err = p.GenerateVideo(context.Background(), video.GenerateVideoRequest{
		Prompt: "a cat",
		Ratio:  "999x999",
	})
	if !errors.Is(err, video.ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest for invalid ratio, got %v", err)
	}
}
