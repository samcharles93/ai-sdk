package runtimeclass

import (
	"testing"
)

func TestProviderConfig(t *testing.T) {
	t.Run("options supply the model directory", func(t *testing.T) {
		cfg, err := providerConfig(map[string]any{"model_dir": "/models/kokoro", "family": "kokoro"}, nil)
		if err != nil {
			t.Fatalf("providerConfig: %v", err)
		}
		if cfg.ModelDir != "/models/kokoro" || cfg.Family != "kokoro" {
			t.Fatalf("cfg = %+v, want kokoro model dir", cfg)
		}
	})

	t.Run("extra wins over options", func(t *testing.T) {
		cfg, err := providerConfig(
			map[string]any{"model_dir": "/models/a", "default_voice": "af_heart", "num_threads": float64(2)},
			map[string]any{"model_dir": "/models/b", "default_voice": "am_michael"},
		)
		if err != nil {
			t.Fatalf("providerConfig: %v", err)
		}
		if cfg.ModelDir != "/models/b" || cfg.DefaultVoice != "am_michael" || cfg.NumThreads != 2 {
			t.Fatalf("cfg = %+v, want per-model overrides", cfg)
		}
	})

	t.Run("voice ids are parsed", func(t *testing.T) {
		cfg, err := providerConfig(map[string]any{
			"model_dir": "/models/kokoro",
			"voice_ids": map[string]any{"af_heart": float64(0), "am_michael": 1},
		}, nil)
		if err != nil {
			t.Fatalf("providerConfig: %v", err)
		}
		if cfg.VoiceIDs["af_heart"] != 0 || cfg.VoiceIDs["am_michael"] != 1 {
			t.Fatalf("voice ids = %+v, want both entries", cfg.VoiceIDs)
		}
	})

	t.Run("missing model directory", func(t *testing.T) {
		if _, err := providerConfig(map[string]any{}, nil); err == nil {
			t.Fatal("expected an error")
		}
	})

	t.Run("invalid voice ids", func(t *testing.T) {
		_, err := providerConfig(map[string]any{
			"model_dir": "/models/kokoro",
			"voice_ids": map[string]any{"af_heart": "first"},
		}, nil)
		if err == nil {
			t.Fatal("expected an error")
		}
	})
}
