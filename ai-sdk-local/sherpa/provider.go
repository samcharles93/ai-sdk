// Package sherpa provides an in-process text-to-speech provider backed by the
// sherpa-onnx runtime (https://github.com/k2-fsa/sherpa-onnx).
//
// It is an experimental spike for bead ai-sdk-2z0: the engine is a C library
// reached through CGO and prebuilt per-OS shared objects, so this package
// lives in its own module and the core ai-sdk stays pure Go. It implements
// speech.Provider and speech.Streamer.
package sherpa

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	sherpaonnx "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
	"github.com/samcharles93/ai-sdk/speech"
)

// Family names the sherpa-onnx model architecture. VITS covers the Piper
// voices; Kokoro and Kitten are the newer multi-voice families.
type Family string

const (
	FamilyVITS   Family = "vits"
	FamilyKokoro Family = "kokoro"
	FamilyKitten Family = "kitten"
)

// Config configures an in-process provider for one model directory.
type Config struct {
	// ModelDir is the directory holding the ONNX model, tokens.txt,
	// espeak-ng-data and (for Kokoro/Kitten) voices.bin.
	ModelDir string
	// Family selects the model architecture. Empty defaults to FamilyVITS.
	Family Family
	// VoiceIDs maps voice names to speaker ids. A request voice is resolved
	// through this map; numeric names (for example "2") resolve directly.
	VoiceIDs map[string]int
	// DefaultVoice is used when a request does not set Voice.
	DefaultVoice string
	// NumThreads limits the inference thread count. Zero defaults to 1.
	NumThreads int
	// MaxNumSentences splits long text into batches to bound memory. Zero
	// defaults to 1.
	MaxNumSentences int
}

// Provider synthesises speech in-process through sherpa-onnx.
type Provider struct {
	cfg    Config
	engine *sherpaonnx.OfflineTts
	mu     sync.Mutex
}

// Compile-time assertions for the supported speech capabilities.
var (
	_ speech.Provider = (*Provider)(nil)
	_ speech.Streamer = (*Provider)(nil)
)

// New loads a model directory and constructs the engine. Loading can take a
// few seconds for larger voices.
func New(cfg Config) (*Provider, error) {
	dir := strings.TrimSpace(cfg.ModelDir)
	if dir == "" {
		return nil, fmt.Errorf("sherpa: ModelDir is required")
	}
	if cfg.Family == "" {
		cfg.Family = FamilyVITS
	}
	if cfg.NumThreads <= 0 {
		cfg.NumThreads = 1
	}
	if cfg.MaxNumSentences <= 0 {
		cfg.MaxNumSentences = 1
	}

	modelPath, err := findModelFile(dir)
	if err != nil {
		return nil, err
	}
	tokens := filepath.Join(dir, "tokens.txt")
	dataDir := filepath.Join(dir, "espeak-ng-data")
	for _, required := range []string{tokens, dataDir} {
		if _, err := os.Stat(required); err != nil {
			return nil, fmt.Errorf("sherpa: missing %s: %w", required, err)
		}
	}

	oc := sherpaonnx.OfflineTtsConfig{}
	oc.Model.NumThreads = cfg.NumThreads
	oc.Model.Debug = 0
	oc.Model.Provider = "cpu"
	oc.MaxNumSentences = cfg.MaxNumSentences
	oc.SilenceScale = 0.2

	switch cfg.Family {
	case FamilyVITS:
		oc.Model.Vits.Model = modelPath
		oc.Model.Vits.Tokens = tokens
		oc.Model.Vits.DataDir = dataDir
		if lexicon := filepath.Join(dir, "lexicon.txt"); fileExists(lexicon) {
			oc.Model.Vits.Lexicon = lexicon
		}
	case FamilyKokoro:
		oc.Model.Kokoro.Model = modelPath
		oc.Model.Kokoro.Tokens = tokens
		oc.Model.Kokoro.DataDir = dataDir
		oc.Model.Kokoro.Voices = filepath.Join(dir, "voices.bin")
	case FamilyKitten:
		oc.Model.Kitten.Model = modelPath
		oc.Model.Kitten.Tokens = tokens
		oc.Model.Kitten.DataDir = dataDir
		oc.Model.Kitten.Voices = filepath.Join(dir, "voices.bin")
	default:
		return nil, fmt.Errorf("sherpa: unsupported family %q", cfg.Family)
	}

	engine := sherpaonnx.NewOfflineTts(&oc)
	if engine == nil {
		return nil, fmt.Errorf("sherpa: failed to create engine for %s", dir)
	}
	return &Provider{cfg: cfg, engine: engine}, nil
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return "sherpa" }

// Close releases the engine. The provider must not be used afterwards.
func (p *Provider) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.engine != nil {
		sherpaonnx.DeleteOfflineTts(p.engine)
		p.engine = nil
	}
}

// GenerateSpeech synthesises the whole clip in-process. Format "wav"
// (default) returns a RIFF/WAVE container; "pcm" returns raw mono int16
// little-endian samples at the engine's sample rate.
func (p *Provider) GenerateSpeech(ctx context.Context, req speech.GenerateSpeechRequest) (speech.GenerateSpeechResponse, error) {
	if err := ctx.Err(); err != nil {
		return speech.GenerateSpeechResponse{}, err
	}
	sid, speed, format, err := p.resolve(req)
	if err != nil {
		return speech.GenerateSpeechResponse{}, err
	}

	p.mu.Lock()
	audio := p.engine.GenerateWithConfig(req.Text, &sherpaonnx.GenerationConfig{
		SilenceScale: 0.2,
		Speed:        speed,
		Sid:          sid,
	}, nil)
	p.mu.Unlock()

	if audio == nil {
		return speech.GenerateSpeechResponse{}, fmt.Errorf("sherpa: synthesis returned no audio: %w", speech.ErrProviderUnavailable)
	}
	if format == "pcm" {
		return speech.GenerateSpeechResponse{Audio: pcm16(audio.Samples), Format: "pcm"}, nil
	}
	return speech.GenerateSpeechResponse{Audio: encodeWAV(audio.Samples, audio.SampleRate), Format: "wav"}, nil
}

// StreamSpeech synthesises with the engine's sample callback, emitting
// PCM16 chunks followed by a Done chunk. The engine still produces the whole
// utterance, so this is chunked delivery rather than true incremental
// decoding.
func (p *Provider) StreamSpeech(ctx context.Context, req speech.GenerateSpeechRequest) (speech.SpeechStream, error) {
	sid, speed, _, err := p.resolve(req)
	if err != nil {
		return nil, err
	}

	s := &pcmStream{chunks: make(chan speech.SpeechChunk, 8), cancel: make(chan struct{})}
	go func() {
		defer close(s.chunks)
		p.mu.Lock()
		audio := p.engine.GenerateWithCallback(req.Text, sid, speed, func(samples []float32) bool {
			if len(samples) == 0 {
				return true
			}
			select {
			case <-s.cancel:
				return false
			case s.chunks <- speech.SpeechChunk{Data: pcm16(samples), Format: "pcm"}:
				return true
			}
		})
		p.mu.Unlock()

		if audio == nil {
			s.setErr(fmt.Errorf("sherpa: synthesis returned no audio: %w", speech.ErrProviderUnavailable))
			return
		}
		select {
		case <-s.cancel:
		case s.chunks <- speech.SpeechChunk{Done: true, Format: "pcm"}:
		}
	}()
	return s, nil
}

// resolve validates a request and maps it onto engine parameters. Unsupported
// domain fields are rejected rather than ignored.
func (p *Provider) resolve(req speech.GenerateSpeechRequest) (sid int, speed float32, format string, err error) {
	if strings.TrimSpace(req.Text) == "" {
		return 0, 0, "", fmt.Errorf("sherpa: text is required: %w", speech.ErrInvalidRequest)
	}
	if req.Instructions != "" {
		return 0, 0, "", fmt.Errorf("sherpa: instructions are not supported: %w", speech.ErrInvalidRequest)
	}
	if req.SampleRate != 0 {
		return 0, 0, "", fmt.Errorf("sherpa: sample_rate is not supported: %w", speech.ErrInvalidRequest)
	}

	format = req.Format
	if format == "" {
		format = "wav"
	}
	if format != "wav" && format != "pcm" {
		return 0, 0, "", fmt.Errorf("sherpa: invalid output format %q: %w", format, speech.ErrInvalidRequest)
	}

	voice := req.Voice
	if voice == "" {
		voice = p.cfg.DefaultVoice
	}
	if voice != "" {
		id, ok := p.lookupVoice(voice)
		if !ok {
			return 0, 0, "", fmt.Errorf("sherpa: unknown voice %q (configured: %s): %w", voice, strings.Join(p.voiceNames(), ", "), speech.ErrInvalidRequest)
		}
		sid = id
	}

	speed = 1
	if req.Speed != 0 {
		speed = float32(req.Speed)
	}
	return sid, speed, format, nil
}

// lookupVoice resolves a voice name to a speaker id, accepting configured
// names and numeric ids.
func (p *Provider) lookupVoice(voice string) (int, bool) {
	if id, ok := p.cfg.VoiceIDs[voice]; ok {
		return id, true
	}
	if id, err := strconv.Atoi(voice); err == nil && id >= 0 {
		return id, true
	}
	return 0, false
}

func (p *Provider) voiceNames() []string {
	names := make([]string, 0, len(p.cfg.VoiceIDs))
	for name := range p.cfg.VoiceIDs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// pcmStream adapts the engine callback to speech.SpeechStream.
type pcmStream struct {
	chunks    chan speech.SpeechChunk
	cancel    chan struct{}
	closeOnce sync.Once
	mu        sync.Mutex
	err       error
}

func (s *pcmStream) setErr(err error) {
	s.mu.Lock()
	s.err = err
	s.mu.Unlock()
}

func (s *pcmStream) Next(ctx context.Context) (speech.SpeechChunk, error) {
	select {
	case <-ctx.Done():
		return speech.SpeechChunk{}, ctx.Err()
	case chunk, ok := <-s.chunks:
		if !ok {
			s.mu.Lock()
			err := s.err
			s.mu.Unlock()
			if err != nil {
				return speech.SpeechChunk{}, err
			}
			return speech.SpeechChunk{}, io.EOF
		}
		return chunk, nil
	}
}

func (s *pcmStream) Close() error {
	s.closeOnce.Do(func() { close(s.cancel) })
	return nil
}

// findModelFile locates the single ONNX model in a model directory, ignoring
// the sidecar .onnx.json metadata.
func findModelFile(dir string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.onnx"))
	if err != nil {
		return "", fmt.Errorf("sherpa: scan model directory: %w", err)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("sherpa: no .onnx model found in %s", dir)
	}
	sort.Strings(matches)
	return matches[0], nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
