package sherpa

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"testing"

	"github.com/samcharles93/ai-sdk/speech"
)

func testProvider() *Provider {
	return &Provider{cfg: Config{
		VoiceIDs:     map[string]int{"af_heart": 0, "am_michael": 1},
		DefaultVoice: "af_heart",
	}}
}

func TestResolve(t *testing.T) {
	t.Run("valid request resolves defaults", func(t *testing.T) {
		sid, speed, format, err := testProvider().resolve(speech.GenerateSpeechRequest{Text: "hi"})
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if sid != 0 || speed != 1 || format != "wav" {
			t.Fatalf("resolve = %d/%v/%s, want 0/1/wav", sid, speed, format)
		}
	})

	t.Run("request voice and speed win", func(t *testing.T) {
		sid, speed, format, err := testProvider().resolve(speech.GenerateSpeechRequest{Text: "hi", Voice: "am_michael", Speed: 1.25, Format: "pcm"})
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if sid != 1 || speed != 1.25 || format != "pcm" {
			t.Fatalf("resolve = %d/%v/%s, want 1/1.25/pcm", sid, speed, format)
		}
	})

	t.Run("numeric voice resolves directly", func(t *testing.T) {
		sid, _, _, err := testProvider().resolve(speech.GenerateSpeechRequest{Text: "hi", Voice: "3"})
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if sid != 3 {
			t.Fatalf("sid = %d, want 3", sid)
		}
	})

	tests := []struct {
		name string
		req  speech.GenerateSpeechRequest
	}{
		{name: "missing text", req: speech.GenerateSpeechRequest{}},
		{name: "instructions unsupported", req: speech.GenerateSpeechRequest{Text: "hi", Instructions: "whisper"}},
		{name: "sample rate unsupported", req: speech.GenerateSpeechRequest{Text: "hi", SampleRate: 24000}},
		{name: "invalid format", req: speech.GenerateSpeechRequest{Text: "hi", Format: "mp3"}},
		{name: "unknown voice", req: speech.GenerateSpeechRequest{Text: "hi", Voice: "nope"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := testProvider().GenerateSpeech(context.Background(), tc.req)
			if !errors.Is(err, speech.ErrInvalidRequest) {
				t.Fatalf("error = %v, want ErrInvalidRequest", err)
			}
		})
	}
}

// TestGenerateSpeechIntegration loads a real model directory configured by
// AI_SDK_SHERPA_MODEL_DIR and synthesises both buffered and streamed audio.
func TestGenerateSpeechIntegration(t *testing.T) {
	dir := os.Getenv("AI_SDK_SHERPA_MODEL_DIR")
	if dir == "" {
		t.Skip("AI_SDK_SHERPA_MODEL_DIR not set; skipping in-process TTS integration test")
	}
	family := Family(os.Getenv("AI_SDK_SHERPA_FAMILY"))
	if family == "" {
		family = FamilyVITS
	}
	p, err := New(Config{ModelDir: dir, Family: family, NumThreads: 2})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Close()

	const text = "In process speech synthesis, running inside Go."
	resp, err := p.GenerateSpeech(context.Background(), speech.GenerateSpeechRequest{Text: text})
	if err != nil {
		t.Fatalf("GenerateSpeech: %v", err)
	}
	if resp.Format != "wav" || len(resp.Audio) <= 44 {
		t.Fatalf("wav = %d bytes format %q, want a populated WAV", len(resp.Audio), resp.Format)
	}
	if string(resp.Audio[0:4]) != "RIFF" || string(resp.Audio[8:12]) != "WAVE" {
		t.Fatalf("audio does not start with a RIFF/WAVE header")
	}
	t.Logf("buffered %d bytes", len(resp.Audio))

	stream, err := p.StreamSpeech(context.Background(), speech.GenerateSpeechRequest{Text: text})
	if err != nil {
		t.Fatalf("StreamSpeech: %v", err)
	}
	defer stream.Close()

	var total, chunks int
	done := false
	for {
		chunk, err := stream.Next(context.Background())
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if chunk.Done {
			done = true
			break
		}
		total += len(chunk.Data)
		chunks++
	}
	if !done || total == 0 {
		t.Fatalf("streamed %d bytes in %d chunks done=%v", total, chunks, done)
	}
	t.Logf("streamed %d bytes in %d chunks", total, chunks)
}

func TestEncodeWAVHeader(t *testing.T) {
	wav := encodeWAV([]float32{0, 0}, 22050)
	if len(wav) != 44+4 {
		t.Fatalf("len = %d, want 48", len(wav))
	}
	if string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		t.Fatal("missing RIFF/WAVE magic")
	}
	if rate := binary.LittleEndian.Uint32(wav[24:28]); rate != 22050 {
		t.Fatalf("sample rate = %d, want 22050", rate)
	}
	if size := binary.LittleEndian.Uint32(wav[40:44]); size != 4 {
		t.Fatalf("data size = %d, want 4", size)
	}
}

func TestPCM16Clamps(t *testing.T) {
	got := pcm16([]float32{0, 1, -1, 2, -2})
	want := []int16{0, 32767, -32767, 32767, -32767}
	for i, w := range want {
		if v := int16(binary.LittleEndian.Uint16(got[i*2:])); v != w {
			t.Fatalf("sample %d = %d, want %d", i, v, w)
		}
	}
}
