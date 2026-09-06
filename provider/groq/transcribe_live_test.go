//go:build live

package groq

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"os"
	"testing"

	"github.com/samcharles93/ai-sdk/transcribe"
)

// TestGroqTranscribeLive calls the real Groq transcription endpoint. Gated
// behind a `live` build tag and skipped unless GROQ_API_KEY is set.
func TestGroqTranscribeLive(t *testing.T) {
	key := os.Getenv("GROQ_API_KEY")
	if key == "" {
		t.Skip("GROQ_API_KEY not set; skipping live transcription test")
	}
	p, err := New(Config{APIKey: key})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	resp, err := p.Transcribe(context.Background(), transcribe.TranscribeRequest{
		Model: "whisper-large-v3",
		Audio: toneWAV(),
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	t.Logf("groq transcribed: %q (len %d)", resp.Text, len(resp.Text))
}

// toneWAV returns a minimal 1-second 440 Hz PCM WAV file.
func toneWAV() []byte {
	sampleRate := 8000
	seconds := 1
	numChannels := 1
	bitsPerSample := 16
	bytesPerSample := bitsPerSample / 8
	samples := sampleRate * seconds
	dataSize := samples * numChannels * bytesPerSample

	buf := &bytes.Buffer{}
	buf.Write([]byte("RIFF"))
	_ = binary.Write(buf, binary.LittleEndian, uint32(36+dataSize))
	buf.Write([]byte("WAVE"))
	buf.Write([]byte("fmt "))
	_ = binary.Write(buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(buf, binary.LittleEndian, uint16(numChannels))
	_ = binary.Write(buf, binary.LittleEndian, uint32(sampleRate))
	byteRate := sampleRate * numChannels * bytesPerSample
	_ = binary.Write(buf, binary.LittleEndian, uint32(byteRate))
	_ = binary.Write(buf, binary.LittleEndian, uint16(numChannels*bytesPerSample))
	_ = binary.Write(buf, binary.LittleEndian, uint16(bitsPerSample))
	buf.Write([]byte("data"))
	_ = binary.Write(buf, binary.LittleEndian, uint32(dataSize))
	for i := 0; i < samples; i++ {
		t := float64(i) / float64(sampleRate)
		v := int16(16384 * math.Sin(2*math.Pi*440*t))
		_ = binary.Write(buf, binary.LittleEndian, v)
	}
	return buf.Bytes()
}
