# kokoro-js TTS sidecar (comparison spike)

A ~70-line Node server that serves the OpenAI speech protocol
(`POST /v1/audio/speech`, `GET /v1/audio/voices`) using
[kokoro-js](https://www.npmjs.com/package/kokoro-js) (Transformers.js,
ONNX Runtime CPU). It exists to compare against the in-process Go provider in
`../sherpa` and to give the ai-sdk a zero-Go-code local backend.

## Run

```bash
npm install
npm start
```

Install note: `onnxruntime-node`'s postinstall tries to detect CUDA from
`nvcc`; on CUDA 13 it fails and falls back to a broken download. Force the CPU
build:

```bash
ONNXRUNTIME_NODE_INSTALL_CUDA=skip npm install
```

First start downloads the Kokoro weights (~80 MB q8) into the Transformers.js
cache; later starts are fast.

## Use with the ai-sdk

The SDK needs no changes — point the existing self-hosted path at it:

```bash
export AI_SDK_TTS_BASE_URL=http://localhost:3000/v1
export AI_SDK_TTS_MODEL=kokoro
export AI_SDK_TTS_VOICE=af_heart
export AI_SDK_TTS_FORMAT=wav
task test:live:tts
```

`speech.VoiceLister` / `Client.ListVoices` also work: the sidecar returns all
28 voices with `name`, `language` and `gender` metadata.

## Measured (this machine, CPU, q8)

| | kokoro-js sidecar | sherpa in-process (Go) |
| --- | --- | --- |
| Process | separate Node process | in the Go binary |
| Install | `node_modules` ~409 MB | prebuilt libs ~32 MB |
| Output | 422 KB WAV in ~2.7 s | 179 KB WAV in ~2.8 s |
| Voices | 28 with metadata | numeric ids via config map |
| Streaming | `TextSplitterStream` (per sentence) | engine callback (per sentence) |
| Platform | Node 18+, any OS, no CGO | CGO, prebuilt per-OS, no musl |

Both routes phonemise through espeak-ng (GPL-3.0), via `phonemizer` here and
inside the sherpa library there — the same licensing question applies.

## Env

| Variable | Default |
| --- | --- |
| `PORT` | `3000` |
| `KOKORO_MODEL_ID` | `onnx-community/Kokoro-82M-v1.0-ONNX` |
| `KOKORO_DTYPE` | `q8` (also `fp32`, `fp16`, `q4`, `q4f16`) |
| `KOKORO_VOICE` | `af_heart` |
