// Package runtimeclass registers the in-process sherpa-onnx speech provider
// with the ai-sdk runtime as a custom ProviderClass. Keeping it separate from
// the provider package preserves the rule that provider packages do not
// import runtime.
package runtimeclass

import (
	"context"
	"fmt"
	"strings"

	"github.com/samcharles93/ai-sdk-local/sherpa"
	"github.com/samcharles93/ai-sdk/runtime"
)

// ClassName is the ProviderConfig.Class value that selects this class.
const ClassName = "sherpa-local"

// Register adds the class to the runtime registry.
func Register() { runtime.MustRegisterClass(Class{}) }

// Class builds in-process sherpa-onnx speech providers from provider options
// and per-model overrides.
type Class struct{}

var _ runtime.ProviderClass = Class{}

// Name returns the class identifier.
func (Class) Name() string { return ClassName }

// Supports reports that this class provides speech only.
func (Class) Supports(cap runtime.Capability) bool { return cap == runtime.CapabilitySpeech }

// New builds the provider. Options and per-model Extra share the same keys:
//
//	model_dir          (string, required) model directory
//	family             (string) vits (default) | kokoro | kitten
//	num_threads        (number) inference threads
//	max_num_sentences  (number) batch size for long text
//	default_voice      (string) voice used when a request omits Voice
//	voice_ids          (map) voice name -> speaker id
//
// A per-model Extra entry wins over the provider-level Options entry.
func (Class) New(_ context.Context, cfg runtime.ProviderConfig, model runtime.ModelInfo) (runtime.ProviderSet, error) {
	pcfg, err := providerConfig(cfg.Options, model.Extra)
	if err != nil {
		return runtime.ProviderSet{}, err
	}
	provider, err := sherpa.New(pcfg)
	if err != nil {
		return runtime.ProviderSet{}, err
	}
	return runtime.ProviderSet{Speech: provider}, nil
}

// providerConfig merges the provider options and per-model overrides.
func providerConfig(options, extra map[string]any) (sherpa.Config, error) {
	pick := func(key string) (any, bool) {
		if v, ok := extra[key]; ok {
			return v, true
		}
		v, ok := options[key]
		return v, ok
	}

	cfg := sherpa.Config{Family: sherpa.FamilyVITS}
	if v, ok := pick("model_dir"); ok {
		cfg.ModelDir, _ = v.(string)
	}
	if strings.TrimSpace(cfg.ModelDir) == "" {
		return sherpa.Config{}, fmt.Errorf("sherpa-local: options.model_dir is required")
	}
	if v, ok := pick("family"); ok {
		if s, _ := v.(string); s != "" {
			cfg.Family = sherpa.Family(s)
		}
	}
	if v, ok := pick("num_threads"); ok {
		if n, ok := number(v); ok {
			cfg.NumThreads = n
		}
	}
	if v, ok := pick("max_num_sentences"); ok {
		if n, ok := number(v); ok {
			cfg.MaxNumSentences = n
		}
	}
	if v, ok := pick("default_voice"); ok {
		cfg.DefaultVoice, _ = v.(string)
	}
	if v, ok := pick("voice_ids"); ok {
		ids, err := voiceIDs(v)
		if err != nil {
			return sherpa.Config{}, err
		}
		cfg.VoiceIDs = ids
	}
	return cfg, nil
}

func voiceIDs(v any) (map[string]int, error) {
	raw, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("sherpa-local: voice_ids must be a map of name to speaker id")
	}
	ids := make(map[string]int, len(raw))
	for name, value := range raw {
		n, ok := number(value)
		if !ok {
			return nil, fmt.Errorf("sherpa-local: voice_ids[%q] is not a number", name)
		}
		ids[name] = n
	}
	return ids, nil
}

func number(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}
