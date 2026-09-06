package minimax

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/samcharles93/ai-sdk/speech"
)

const (
	defaultSpeechModel = "speech-2.8-hd"
	defaultVoiceID     = "English_expressive_narrator"
)

// GenerateSpeech synthesises speech audio from text using MiniMax's
// synchronous T2A HTTP endpoint (/v1/t2a_v2). It returns the raw audio bytes
// (hex-encoded on the wire, decoded here) with an mp3 format. It satisfies
// speech.Provider.
func (p *Provider) GenerateSpeech(ctx context.Context, req speech.GenerateSpeechRequest) (speech.GenerateSpeechResponse, error) {
	if req.Text == "" {
		return speech.GenerateSpeechResponse{}, fmt.Errorf("minimax: text is required: %w", speech.ErrInvalidRequest)
	}
	model := req.Model
	if model == "" {
		model = defaultSpeechModel
	}
	voice := req.Voice
	if voice == "" {
		voice = defaultVoiceID
	}
	speed := req.Speed
	if speed == 0 {
		speed = 1
	}

	body := map[string]any{
		"model":         model,
		"text":          req.Text,
		"stream":        false,
		"output_format": "hex",
		"voice_setting": map[string]any{
			"voice_id": voice,
			"speed":    speed,
			"vol":      1,
			"pitch":    0,
		},
	}

	var out wireT2AResponse
	if err := p.doJSON(ctx, http.MethodPost, "/v1/t2a_v2", body, &out, speechSentinels); err != nil {
		return speech.GenerateSpeechResponse{}, err
	}
	if out.Data == nil || out.Data.Audio == "" {
		return speech.GenerateSpeechResponse{}, fmt.Errorf("minimax: t2a returned no audio")
	}
	audio, err := hex.DecodeString(out.Data.Audio)
	if err != nil {
		return speech.GenerateSpeechResponse{}, fmt.Errorf("minimax: decode t2a hex audio: %w", err)
	}
	return speech.GenerateSpeechResponse{Audio: audio, Format: "mp3"}, nil
}

// wireT2AResponse mirrors MiniMax's synchronous T2A response. data.audio is
// hex-encoded when request output_format is hex.
type wireT2AResponse struct {
	Data *struct {
		Audio  string `json:"audio"`
		Status int    `json:"status"`
	} `json:"data"`
	TraceID string `json:"trace_id"`
}

// Compile-time assertion that *Provider satisfies speech.Provider.
var _ speech.Provider = (*Provider)(nil)
