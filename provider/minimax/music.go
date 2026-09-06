package minimax

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/samcharles93/ai-sdk/music"
)

const defaultMusicModel = "music-3.0"

// MusicOptions carries MiniMax-specific music generation options.
type MusicOptions struct {
	// SampleRate is the sampling rate: 16000, 24000, 32000, or 44100.
	SampleRate int `json:"sample_rate,omitempty"`
}

// GenerateMusic generates a music track from a prompt (and optional lyrics)
// using MiniMax's synchronous /v1/music_generation endpoint. The response
// audio is hex-encoded on the wire (status 2 = complete) and returned as raw
// bytes here. It satisfies music.Provider.
func (p *Provider) GenerateMusic(ctx context.Context, req music.GenerateMusicRequest) (music.GenerateMusicResponse, error) {
	if req.Prompt == "" {
		return music.GenerateMusicResponse{}, fmt.Errorf("minimax: prompt is required: %w", music.ErrInvalidRequest)
	}
	opts, err := music.ProviderOptionsFor[MusicOptions](req.ProviderOptions, "minimax")
	if err != nil {
		return music.GenerateMusicResponse{}, fmt.Errorf("minimax: parse music provider options: %w", err)
	}
	model := req.Model
	if model == "" {
		model = defaultMusicModel
	}
	format := req.Format
	if format == "" {
		format = "mp3"
	}

	audioSetting := map[string]any{"format": format}
	if opts.SampleRate != 0 {
		if !validSampleRate(opts.SampleRate) {
			return music.GenerateMusicResponse{}, fmt.Errorf("minimax: unsupported sample_rate %d; allowed: 16000/24000/32000/44100: %w", opts.SampleRate, music.ErrInvalidRequest)
		}
		audioSetting["sample_rate"] = opts.SampleRate
	}
	body := map[string]any{
		"model":         model,
		"prompt":        req.Prompt,
		"audio_setting": audioSetting,
	}
	if req.Lyrics != "" {
		body["lyrics"] = req.Lyrics
	}
	if req.Instrumental {
		body["is_instrumental"] = true
	}
	if req.LyricsOptimizer {
		body["lyrics_optimizer"] = true
	}

	var out wireMusicResponse
	if err := p.doJSON(ctx, http.MethodPost, "/v1/music_generation", body, &out, musicSentinels); err != nil {
		return music.GenerateMusicResponse{}, err
	}
	if out.BaseResp != nil && out.BaseResp.StatusCode != 0 {
		return music.GenerateMusicResponse{}, fmt.Errorf("minimax: music generation failed: %s", out.BaseResp.StatusMsg)
	}
	if out.Data == nil || out.Data.Audio == "" {
		return music.GenerateMusicResponse{}, fmt.Errorf("minimax: music generation returned no audio")
	}
	// The sync endpoint returns status 2 (complete); 1 means still synthesising.
	if out.Data.Status != 0 && out.Data.Status != 2 {
		return music.GenerateMusicResponse{}, fmt.Errorf("minimax: music generation not complete (status %d)", out.Data.Status)
	}
	audio, err := hex.DecodeString(out.Data.Audio)
	if err != nil {
		return music.GenerateMusicResponse{}, fmt.Errorf("minimax: decode music hex audio: %w", err)
	}
	return music.GenerateMusicResponse{Audio: audio, Format: format}, nil
}

// wireMusicResponse mirrors MiniMax's music generation response. data.audio is
// always hex-encoded (the wire encoding is hex for every audio_setting.format);
// data.status is 2 when the track is complete.
type wireMusicResponse struct {
	BaseResp *struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp"`
	Data *struct {
		Audio  string `json:"audio"`
		Status int    `json:"status"`
	} `json:"data"`
}

// validSampleRate reports whether v is one of MiniMax's allowed sampling
// rates.
func validSampleRate(v int) bool {
	switch v {
	case 16000, 24000, 32000, 44100:
		return true
	default:
		return false
	}
}

// Compile-time assertion that *Provider satisfies music.Provider.
var _ music.Provider = (*Provider)(nil)
