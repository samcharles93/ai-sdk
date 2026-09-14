# In-process TTS spike (ai-sdk-local)

Experimental, unshipped. This module hosts a text-to-speech engine **inside the
Go process** using the official
[sherpa-onnx](https://github.com/k2-fsa/sherpa-onnx) Go bindings, and exposes
it to the ai-sdk runtime as a custom `ProviderClass`.

It exists to answer bead **ai-sdk-2z0**: can TTS run in Go without an external
server? Yes — with a native engine, not with pure Go. Details in the bead's
design notes. `node-tts/` contains the competing Node/Transformers.js sidecar
built for comparison.

## Why a separate module

The engine is a C/C++ library reached through CGO with prebuilt per-OS shared
objects (~32 MB on Linux). Keeping it here means the core `ai-sdk` module stays
pure Go, cross-compiles freely, and never pulls native libraries into a
consumer that only wants the HTTP backends.

## Model acquisition

Models are downloaded separately from the sherpa-onnx release assets. Example
(a small Piper voice, ~79 MB unpacked):

```bash
mkdir -p models && cd models
curl -SLO https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/vits-piper-en_US-lessac-medium.tar.bz2
tar xf vits-piper-en_US-lessac-medium.tar.bz2
rm vits-piper-en_US-lessac-medium.tar.bz2
```

Kokoro English is the higher-quality option:

```bash
curl -SLO https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/kokoro-en-v0_19.tar.bz2
tar xf kokoro-en-v0_19.tar.bz2
```

Each directory contains the ONNX model, `tokens.txt`, `espeak-ng-data/`, and
(for Kokoro/Kitten) `voices.bin`.

## Run the spike

```bash
export AI_SDK_SHERPA_MODEL_DIR=$PWD/models/vits-piper-en_US-lessac-medium
go run ./cmd/spike -output /tmp/spike.wav "In process speech synthesis, running inside Go."
```

Expected output: buffered WAV bytes and a chunked stream, both produced
in-process. The engine stays loaded between calls.

## Use through the runtime

```go
runtimeclass.Register()

rt := runtime.NewRuntime(runtime.Config{Providers: map[string]runtime.ProviderConfig{
    "local": {
        ID:    "local",
        Class: runtimeclass.ClassName, // "sherpa-local"
        Options: map[string]any{
            "model_dir":     "/models/vits-piper-en_US-lessac-medium",
            "family":        "vits", // vits | kokoro | kitten
            "num_threads":   2,
            "default_voice": "af_heart",
            "voice_ids":     map[string]any{"af_heart": 0, "am_michael": 1},
        },
    },
}})

audio, err := rt.Speech(ctx, "local/tts", speech.GenerateSpeechRequest{Text: "Hello"})
```

Per-model `extra` entries win over provider `options` for the same keys.

## Tests

```bash
AI_SDK_SHERPA_MODEL_DIR=$PWD/models/vits-piper-en_US-lessac-medium go test ./...
```

For Kokoro add the family:

```bash
AI_SDK_SHERPA_MODEL_DIR=$PWD/models/kokoro-en-v0_19 AI_SDK_SHERPA_FAMILY=kokoro go test ./sherpa/ -v
```

Without the env var the integration test skips; validation and audio-format
unit tests always run. This module is not part of the root `task check`.

## Supported / not supported

| Feature | Status |
| --- | --- |
| `GenerateSpeech` (WAV or raw PCM) | yes |
| `StreamSpeech` (chunked PCM16 via the engine callback) | yes |
| Voice selection (name map or numeric id) | yes |
| `speech.VoiceLister` voice discovery | no — voices come from `voices.bin`/config; the provider does not implement `VoiceLister` |
| `Instructions`, `SampleRate` | rejected with `ErrInvalidRequest` |
| Matcha family | not wired (needs a separate vocoder model) |

## Licensing

- sherpa-onnx and the Go bindings: Apache-2.0.
- ONNX Runtime (bundled in the prebuilt libs): MIT.
- **espeak-ng**, compiled into the prebuilt library and shipped as model data:
  GPL-3.0. Verify redistribution obligations before shipping this in a
  commercial product.
- Voice models carry their own licenses (check each model card).

## Platform notes

- CGO required (`CGO_ENABLED=1`); prebuilt libs cover linux/macos/windows on
  amd64/arm64, **no musl**.
- Streaming is chunked delivery: sherpa synthesises the utterance and invokes
  the callback per chunk; it is not incremental decoding.
