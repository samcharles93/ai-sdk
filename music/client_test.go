package music

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// stubProvider implements Provider for testing the Client facade.
type stubProvider struct {
	fn func(ctx context.Context, req GenerateMusicRequest) (GenerateMusicResponse, error)
}

func (s *stubProvider) Name() string { return "stub" }
func (s *stubProvider) GenerateMusic(ctx context.Context, req GenerateMusicRequest) (GenerateMusicResponse, error) {
	return s.fn(ctx, req)
}

func TestClient_IsNil(t *testing.T) {
	var c *Client
	_, err := c.GenerateMusic(context.Background(), GenerateMusicRequest{Prompt: "p"})
	if !errors.Is(err, ErrNoProvider) {
		t.Fatalf("nil client: got %v, want ErrNoProvider", err)
	}
	if c.Provider() != nil {
		t.Fatal("nil client.Provider must return nil")
	}
}

func TestClient_NoProvider(t *testing.T) {
	c := NewClient(nil)
	_, err := c.GenerateMusic(context.Background(), GenerateMusicRequest{Prompt: "p"})
	if !errors.Is(err, ErrNoProvider) {
		t.Fatalf("no provider: got %v, want ErrNoProvider", err)
	}
	if c.Provider() != nil {
		t.Fatal("nil-initialised client.Provider must return nil")
	}
}

func TestClient_ValidatesPrompt(t *testing.T) {
	c := NewClient(&stubProvider{fn: func(ctx context.Context, req GenerateMusicRequest) (GenerateMusicResponse, error) {
		t.Error("provider must not be called with empty prompt")
		return GenerateMusicResponse{}, nil
	}})
	_, err := c.GenerateMusic(context.Background(), GenerateMusicRequest{})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty prompt: got %v, want ErrInvalidRequest", err)
	}
}

func TestClient_DelegatesToProvider(t *testing.T) {
	wantReq := GenerateMusicRequest{Model: "m", Prompt: "a track", Instrumental: true}
	wantResp := GenerateMusicResponse{Audio: []byte("audio"), Format: "mp3"}
	c := NewClient(&stubProvider{fn: func(ctx context.Context, req GenerateMusicRequest) (GenerateMusicResponse, error) {
		if req.Model != wantReq.Model || req.Prompt != wantReq.Prompt || !req.Instrumental {
			t.Fatalf("unexpected request: %+v", req)
		}
		return wantResp, nil
	}})
	resp, err := c.GenerateMusic(context.Background(), wantReq)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if string(resp.Audio) != "audio" || resp.Format != "mp3" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestClient_ProviderPropagatesErrors(t *testing.T) {
	c := NewClient(&stubProvider{fn: func(ctx context.Context, req GenerateMusicRequest) (GenerateMusicResponse, error) {
		return GenerateMusicResponse{}, ErrRateLimited
	}})
	_, err := c.GenerateMusic(context.Background(), GenerateMusicRequest{Prompt: "p"})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("got %v, want ErrRateLimited", err)
	}
}

func TestSentinelErrors_AreDistinct(t *testing.T) {
	sentinels := []error{
		ErrNoProvider,
		ErrInvalidRequest,
		ErrProviderUnavailable,
		ErrRateLimited,
		ErrAuthFailed,
		ErrUnsupported,
	}
	for i, a := range sentinels {
		for j, b := range sentinels {
			if i != j && errors.Is(a, b) {
				t.Fatalf("sentinel %v and %v must be distinct via errors.Is", a, b)
			}
		}
	}
}

func TestSentinelErrors_ContainText(t *testing.T) {
	wants := map[error]string{
		ErrNoProvider:          "music:",
		ErrInvalidRequest:      "music:",
		ErrProviderUnavailable: "music:",
		ErrRateLimited:         "music:",
		ErrAuthFailed:          "music:",
		ErrUnsupported:         "music:",
	}
	for sentinel, prefix := range wants {
		if !strings.HasPrefix(sentinel.Error(), prefix) {
			t.Fatalf("%v does not have prefix %q", sentinel, prefix)
		}
	}
}
