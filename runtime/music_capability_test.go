package runtime

import (
	"context"
	"testing"
)

// TestMinimaxClassSurfacesMusic verifies the minimax class advertises
// CapabilityMusic and the granular builder surfaces a music.Provider on the
// ProviderSet, guarding the music domain wiring.
func TestMinimaxClassSurfacesMusic(t *testing.T) {
	RegisterBuiltinClasses()

	cls, ok := GetClass("minimax")
	if !ok {
		t.Fatal("minimax class not registered")
	}
	if !cls.Supports(CapabilityMusic) {
		t.Fatal("minimax class must advertise CapabilityMusic")
	}

	cfg := ProviderConfig{
		ID:      "minimax",
		Class:   "minimax",
		BaseURL: "https://api.minimax.io",
		Auth:    AuthConfig{Type: AuthTypeAPIKey, APIKey: "test-key"},
	}
	model := ModelInfo{ID: "music-3.0", ProviderID: "minimax", URL: "https://api.minimax.io"}

	set, err := cls.New(context.Background(), cfg, model)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if set.Music == nil {
		t.Fatal("expected minimax ProviderSet to surface a music.Provider")
	}
	if !set.Has(CapabilityMusic) {
		t.Fatal("set must report music support")
	}
	// The class also wires the other advertised domains.
	if set.Chat == nil || set.Image == nil || set.Speech == nil || set.Video == nil {
		t.Fatal("expected chat/image/speech/video providers to be wired on the minimax set")
	}
}
