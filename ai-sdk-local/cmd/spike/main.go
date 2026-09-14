// Command spike exercises the in-process sherpa-onnx provider through the
// ai-sdk runtime: it registers the custom class, runs Runtime.Speech
// (buffered) and Runtime.SpeechStream (chunked), and writes the buffered WAV
// to disk. It is a throwaway harness for bead ai-sdk-2z0.
//
//	export AI_SDK_SHERPA_MODEL_DIR=./models/vits-piper-en_US-lessac-medium
//	go run ./cmd/spike -output /tmp/spike.wav "Text to synthesise"
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/samcharles93/ai-sdk-local/sherpa/runtimeclass"
	"github.com/samcharles93/ai-sdk/runtime"
	"github.com/samcharles93/ai-sdk/speech"
)

func main() {
	output := flag.String("output", "/tmp/sherpa-spike.wav", "output WAV path for the buffered result")
	voice := flag.String("voice", "", "voice id (name or numeric speaker id)")
	family := flag.String("family", "vits", "model family: vits | kokoro | kitten")
	flag.Parse()

	text := flag.Arg(0)
	if text == "" {
		log.Fatal("provide text to synthesize")
	}
	modelDir := os.Getenv("AI_SDK_SHERPA_MODEL_DIR")
	if modelDir == "" {
		modelDir = "./models/vits-piper-en_US-lessac-medium"
	}

	runtimeclass.Register()
	rt := runtime.NewRuntime(runtime.Config{Providers: map[string]runtime.ProviderConfig{
		"local": {
			ID:    "local",
			Class: runtimeclass.ClassName,
			Options: map[string]any{
				"model_dir":     modelDir,
				"family":        *family,
				"num_threads":   2,
				"default_voice": *voice,
			},
		},
	}})

	ctx := context.Background()

	start := time.Now()
	resp, err := rt.Speech(ctx, "local/tts", speech.GenerateSpeechRequest{Text: text})
	if err != nil {
		log.Fatalf("Runtime.Speech: %v", err)
	}
	if err := os.WriteFile(*output, resp.Audio, 0o644); err != nil {
		log.Fatalf("write %s: %v", *output, err)
	}
	fmt.Printf("buffered: %d bytes of %s in %s -> %s\n", len(resp.Audio), resp.Format, time.Since(start).Round(time.Millisecond), *output)

	start = time.Now()
	stream, err := rt.SpeechStream(ctx, "local/tts", speech.GenerateSpeechRequest{Text: text})
	if err != nil {
		log.Fatalf("Runtime.SpeechStream: %v", err)
	}
	defer stream.Close()

	var total, chunks int
	firstChunk := time.Duration(0)
	for {
		chunk, err := stream.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("Next: %v", err)
		}
		if chunk.Done {
			break
		}
		if chunks == 0 {
			firstChunk = time.Since(start)
		}
		total += len(chunk.Data)
		chunks++
	}
	fmt.Printf("streamed: %d bytes in %d chunks, first chunk after %s, total %s\n", total, chunks, firstChunk.Round(time.Millisecond), time.Since(start).Round(time.Millisecond))
}
