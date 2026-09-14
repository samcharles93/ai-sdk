package runtime

import "strings"

// capabilityOutputModalities maps each capability to the modality values that
// imply it. Providers publish both the models.dev vocabulary (audio) and live
// API variants (Groq's speech), so both are accepted.
var capabilityOutputModalities = map[Capability][]string{
	CapabilityChat:   {"text"},
	CapabilitySpeech: {"audio", "speech"},
	CapabilityImage:  {"image"},
	CapabilityVideo:  {"video"},
	CapabilityMusic:  {"music"},
}

// modelSupports reports whether a model's metadata confirms it supports cap.
// found is false when the metadata is silent (no explicit capabilities and no
// output modalities); callers fail open in that case rather than guessing.
func modelSupports(model ModelInfo, cap Capability) (supported, found bool) {
	if len(model.Capabilities) > 0 {
		for _, c := range model.Capabilities {
			if c == cap {
				return true, true
			}
		}
		return false, true
	}
	if len(model.OutputModalities) == 0 {
		return false, false
	}
	want := capabilityOutputModalities[cap]
	for _, modality := range model.OutputModalities {
		for _, w := range want {
			if strings.EqualFold(modality, w) {
				return true, true
			}
		}
	}
	return false, true
}
