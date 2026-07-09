// Command speech-to-text demonstrates the transcription API pattern
// using the AI SDK.
//
// This example shows the API pattern for audio transcription. It currently
// prints a placeholder because no provider in this SDK implements
// transcribe.Provider. Provider authors can implement the interface to
// enable the pattern shown here.
//
//	Usage:
//	  go run ./ai-sdk-examples/speech-to-text/
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"os"
	"time"

	"github.com/samcharles93/ai-sdk/provider/openai"
	"github.com/samcharles93/ai-sdk/transcribe"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("OPENAI_API_KEY environment variable is required")
	}

	provider, err := openai.New(openai.Config{APIKey: apiKey})
	if err != nil {
		return fmt.Errorf("create openai provider: %w", err)
	}

	// Generate a minimal WAV file programmatically (1 second of 440Hz sine tone)
	audio := generateTestWAV()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	resp, err := provider.Transcribe(ctx, transcribe.TranscribeRequest{
		Model:    "whisper-1",
		Audio:    audio,
		Language: "en",
	})
	if err != nil {
		return fmt.Errorf("transcribe: %w", err)
	}

	fmt.Println("Transcription:")
	fmt.Println(resp.Text)
	if len(resp.Segments) > 0 {
		for _, seg := range resp.Segments {
			fmt.Printf("  [%.1fs-%.1fs] %s\n", seg.Start, seg.End, seg.Text)
		}
	}
	return nil
}

// generateTestWAV creates a minimal PCM WAV file with a 1-second 440Hz tone.
func generateTestWAV() []byte {
	sampleRate := 8000
	seconds := 1
	numChannels := 1
	bitsPerSample := 16
	bytesPerSample := bitsPerSample / 8
	samples := sampleRate * seconds
	dataSize := samples * numChannels * bytesPerSample

	buf := &bytes.Buffer{}

	// RIFF header
	buf.Write([]byte("RIFF"))
	// ChunkSize = 36 + SubChunk2Size
	_ = binary.Write(buf, binary.LittleEndian, uint32(36+dataSize))
	buf.Write([]byte("WAVE"))

	// fmt chunk
	buf.Write([]byte("fmt "))
	_ = binary.Write(buf, binary.LittleEndian, uint32(16))          // Subchunk1Size
	_ = binary.Write(buf, binary.LittleEndian, uint16(1))           // AudioFormat PCM
	_ = binary.Write(buf, binary.LittleEndian, uint16(numChannels)) // NumChannels
	_ = binary.Write(buf, binary.LittleEndian, uint32(sampleRate))  // SampleRate
	byteRate := sampleRate * numChannels * bytesPerSample
	_ = binary.Write(buf, binary.LittleEndian, uint32(byteRate))                   // ByteRate
	_ = binary.Write(buf, binary.LittleEndian, uint16(numChannels*bytesPerSample)) // BlockAlign
	_ = binary.Write(buf, binary.LittleEndian, uint16(bitsPerSample))              // BitsPerSample

	// data chunk
	buf.Write([]byte("data"))
	_ = binary.Write(buf, binary.LittleEndian, uint32(dataSize))

	// Generate sine wave samples
	for i := 0; i < samples; i++ {
		t := float64(i) / float64(sampleRate)
		v := int16(16384 * math.Sin(2*math.Pi*440*t))
		_ = binary.Write(buf, binary.LittleEndian, v)
	}

	return buf.Bytes()
}
