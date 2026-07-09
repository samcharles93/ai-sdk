// Package util provides shared utility functions for the AI SDK.
package util

// DetectAudioFormat attempts to detect the audio format from the first few
// bytes of the audio data using magic bytes.
//
// Returns the file extension (e.g. "mp3", "wav", "ogg") or an empty string
// if the format cannot be determined.
func DetectAudioFormat(data []byte) string {
	if len(data) < 12 {
		return ""
	}
	// MP3: ID3 tag or sync word
	if len(data) >= 3 && data[0] == 'I' && data[1] == 'D' && data[2] == '3' {
		return "mp3"
	}
	if data[0] == 0xFF && (data[1]&0xE0) == 0xE0 {
		return "mp3"
	}
	// WAV: RIFF header
	if len(data) >= 4 && string(data[:4]) == "RIFF" {
		return "wav"
	}
	// OGG: OggS header
	if len(data) >= 4 && string(data[:4]) == "OggS" {
		return "ogg"
	}
	// WebM / Matroska
	if len(data) >= 4 && data[0] == 0x1a && data[1] == 0x45 && data[2] == 0xdf && data[3] == 0xa3 {
		return "webm"
	}
	// FLAC
	if len(data) >= 4 && string(data[:4]) == "fLaC" {
		return "flac"
	}
	return ""
}
