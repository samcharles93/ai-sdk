package tts

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/speech"
)

// StreamFormat selects the OpenAI-compatible streaming wire mode.
type StreamFormat string

const (
	// StreamFormatAudio streams raw container bytes over a chunked response.
	StreamFormatAudio StreamFormat = "audio"
	// StreamFormatSSE streams base64 audio deltas as server-sent events.
	StreamFormatSSE StreamFormat = "sse"
)

// Stream starts a streaming speech synthesis. In SSE mode the returned stream
// decodes speech.audio.delta and speech.audio.done events; in audio mode it
// emits the raw chunked container bytes as they arrive. Validation and non-2xx
// classification behave exactly as in Generate, so failures before the first
// chunk are returned here. The caller must Close the stream.
func Stream(ctx context.Context, cfg Config, req speech.GenerateSpeechRequest, mode StreamFormat) (speech.SpeechStream, string, error) {
	body, format, err := prepare(cfg, req)
	if err != nil {
		return nil, "", err
	}
	body["stream_format"] = string(mode)

	buf, err := json.Marshal(body)
	if err != nil {
		return nil, "", fmt.Errorf("%s: marshal speech request: %w", cfg.Provider, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+endpoint, bytes.NewReader(buf))
	if err != nil {
		return nil, "", fmt.Errorf("%s: build speech request: %w", cfg.Provider, err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	if mode == StreamFormatSSE {
		httpReq.Header.Set("Accept", "text/event-stream")
	} else {
		httpReq.Header.Set("Accept", "audio/*")
	}

	resp, err := cfg.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, "", fmt.Errorf("%s: http do: %w", cfg.Provider, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		snippet := chat.SanitizeErrorBody(respBody)
		if cfg.ClassifyError != nil {
			return nil, "", cfg.ClassifyError(resp, snippet)
		}
		return nil, "", defaultError(cfg.Provider, resp.StatusCode, snippet)
	}

	if mode == StreamFormatSSE {
		return newSSEStream(resp.Body, cfg.Provider, format), format, nil
	}
	return newRawStream(resp.Body, cfg.Provider, format), format, nil
}

// rawStream emits a chunked audio body as it arrives, then a Done chunk.
type rawStream struct {
	body     io.ReadCloser
	provider string
	format   string
	done     bool
	closed   bool
}

func newRawStream(body io.ReadCloser, provider, format string) speech.SpeechStream {
	return &rawStream{body: body, provider: provider, format: format}
}

func (s *rawStream) Next(ctx context.Context) (speech.SpeechChunk, error) {
	if s.done {
		return speech.SpeechChunk{}, io.EOF
	}
	if err := ctx.Err(); err != nil {
		return speech.SpeechChunk{}, err
	}
	buf := make([]byte, 32*1024)
	for {
		n, err := s.body.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			return speech.SpeechChunk{Data: data, Format: s.format}, nil
		}
		if errors.Is(err, io.EOF) {
			return s.finish(), nil
		}
		if err != nil {
			return speech.SpeechChunk{}, fmt.Errorf("%s: read audio stream: %w", s.provider, err)
		}
	}
}

func (s *rawStream) finish() speech.SpeechChunk {
	s.done = true
	return speech.SpeechChunk{Done: true, Format: s.format}
}

func (s *rawStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.body.Close()
}

// sseStream decodes OpenAI speech SSE events into audio chunks.
type sseStream struct {
	body     io.ReadCloser
	reader   *bufio.Reader
	provider string
	format   string
	done     bool
	closed   bool
}

func newSSEStream(body io.ReadCloser, provider, format string) speech.SpeechStream {
	return &sseStream{body: body, reader: bufio.NewReader(body), provider: provider, format: format}
}

// wireSpeechEvent mirrors the OpenAI streaming speech event union. Unknown
// event types are ignored.
type wireSpeechEvent struct {
	Type  string `json:"type"`
	Audio string `json:"audio,omitempty"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

func (s *sseStream) Next(ctx context.Context) (speech.SpeechChunk, error) {
	if s.done {
		return speech.SpeechChunk{}, io.EOF
	}
	if err := ctx.Err(); err != nil {
		return speech.SpeechChunk{}, err
	}
	for {
		line, err := s.reader.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				// A clean end without a done event still terminates the
				// stream; callers always see a Done chunk before io.EOF.
				return s.finish(), nil
			}
			return speech.SpeechChunk{}, fmt.Errorf("%s: stream read: %w", s.provider, err)
		}
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 || trimmed[0] == ':' {
			continue
		}
		if !bytes.HasPrefix(trimmed, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(trimmed[len("data:"):])
		if len(data) == 0 {
			continue
		}
		if bytes.Equal(data, []byte("[DONE]")) {
			return s.finish(), nil
		}

		var event wireSpeechEvent
		if err := json.Unmarshal(data, &event); err != nil {
			return speech.SpeechChunk{}, fmt.Errorf("%s: decode stream event: %w", s.provider, err)
		}
		switch event.Type {
		case "speech.audio.delta":
			if event.Audio == "" {
				continue
			}
			decoded, err := base64.StdEncoding.DecodeString(event.Audio)
			if err != nil {
				return speech.SpeechChunk{}, fmt.Errorf("%s: decode audio delta: %w", s.provider, err)
			}
			return speech.SpeechChunk{Data: decoded, Format: s.format}, nil
		case "speech.audio.done":
			chunk := s.finish()
			if event.Usage != nil {
				chunk.Usage = &speech.Usage{
					InputTokens:  event.Usage.InputTokens,
					OutputTokens: event.Usage.OutputTokens,
					TotalTokens:  event.Usage.TotalTokens,
				}
			}
			return chunk, nil
		default:
			continue
		}
	}
}

func (s *sseStream) finish() speech.SpeechChunk {
	s.done = true
	return speech.SpeechChunk{Done: true, Format: s.format}
}

func (s *sseStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.body.Close()
}
