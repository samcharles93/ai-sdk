package runtime

import "testing"

func TestModelSupports(t *testing.T) {
	tests := []struct {
		name      string
		model     ModelInfo
		cap       Capability
		supported bool
		found     bool
	}{
		{
			name:      "explicit capabilities support",
			model:     ModelInfo{Capabilities: []Capability{CapabilitySpeech}},
			cap:       CapabilitySpeech,
			supported: true,
			found:     true,
		},
		{
			name:      "explicit capabilities reject",
			model:     ModelInfo{Capabilities: []Capability{CapabilityChat}},
			cap:       CapabilitySpeech,
			supported: false,
			found:     true,
		},
		{
			name:      "explicit capabilities win over modalities",
			model:     ModelInfo{Capabilities: []Capability{CapabilitySpeech}, OutputModalities: []string{"text"}},
			cap:       CapabilitySpeech,
			supported: true,
			found:     true,
		},
		{
			name:      "audio modality implies speech",
			model:     ModelInfo{OutputModalities: []string{"audio"}},
			cap:       CapabilitySpeech,
			supported: true,
			found:     true,
		},
		{
			name:      "speech alias implies speech",
			model:     ModelInfo{OutputModalities: []string{"speech"}},
			cap:       CapabilitySpeech,
			supported: true,
			found:     true,
		},
		{
			name:      "modality match is case insensitive",
			model:     ModelInfo{OutputModalities: []string{"Audio"}},
			cap:       CapabilitySpeech,
			supported: true,
			found:     true,
		},
		{
			name:      "text only rejects speech",
			model:     ModelInfo{OutputModalities: []string{"text"}},
			cap:       CapabilitySpeech,
			supported: false,
			found:     true,
		},
		{
			name:      "text implies chat",
			model:     ModelInfo{OutputModalities: []string{"text"}},
			cap:       CapabilityChat,
			supported: true,
			found:     true,
		},
		{
			name:      "image implies image generation",
			model:     ModelInfo{OutputModalities: []string{"image"}},
			cap:       CapabilityImage,
			supported: true,
			found:     true,
		},
		{
			name:      "empty metadata is unknown",
			model:     ModelInfo{},
			cap:       CapabilitySpeech,
			supported: false,
			found:     false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			supported, found := modelSupports(tc.model, tc.cap)
			if supported != tc.supported || found != tc.found {
				t.Fatalf("modelSupports = (%v, %v), want (%v, %v)", supported, found, tc.supported, tc.found)
			}
		})
	}
}
