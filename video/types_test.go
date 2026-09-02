package video

import (
	"testing"
)

func TestVideoModeConstants(t *testing.T) {
	cases := map[VideoMode]string{
		VideoModeTextToVideo:      "text-to-video",
		VideoModeEditVideo:        "edit-video",
		VideoModeExtendVideo:      "extend-video",
		VideoModeReferenceToVideo: "reference-to-video",
	}
	for mode, want := range cases {
		if string(mode) != want {
			t.Errorf("VideoMode(%q).String() = %q, want %q", mode, string(mode), want)
		}
	}
}

func TestGenerateVideoRequest_TypedEditFields(t *testing.T) {
	req := GenerateVideoRequest{
		Model:           "grok-video",
		Prompt:          "a cat",
		Duration:        "00:00:10",
		Mode:            VideoModeEditVideo,
		SourceVideo:     "http://example.com/src.mp4",
		ReferenceImages: []string{"http://example.com/ref.png"},
		Ratio:           "16:9",
	}

	if req.Mode != VideoModeEditVideo {
		t.Errorf("Mode = %q, want %q", req.Mode, VideoModeEditVideo)
	}
	if req.SourceVideo != "http://example.com/src.mp4" {
		t.Errorf("SourceVideo = %q", req.SourceVideo)
	}
	if len(req.ReferenceImages) != 1 || req.ReferenceImages[0] != "http://example.com/ref.png" {
		t.Errorf("ReferenceImages = %v", req.ReferenceImages)
	}
	if req.Ratio != "16:9" {
		t.Errorf("Ratio = %q, want 16:9", req.Ratio)
	}
}

func TestGenerateVideoRequest_DurationRemainsString(t *testing.T) {
	// The existing Duration field is intentionally a string; ensure the new
	// typed fields do not introduce a competing seconds field.
	req := GenerateVideoRequest{Duration: "00:00:10"}
	if req.Duration != "00:00:10" {
		t.Errorf("Duration = %q, want 00:00:10", req.Duration)
	}
}
