// Spike: an OpenAI-compatible /v1/audio/speech sidecar backed by kokoro-js
// (Transformers.js). It exists to compare against the in-process Go provider
// in ../sherpa: same wire protocol, same model family, different runtime.
//
// Usage: npm install && npm start
// Env: PORT (3000), KOKORO_MODEL_ID, KOKORO_DTYPE (q8), KOKORO_VOICE (af_heart)
import http from "node:http";
import { KokoroTTS } from "kokoro-js";

const modelId = process.env.KOKORO_MODEL_ID ?? "onnx-community/Kokoro-82M-v1.0-ONNX";
const port = Number(process.env.PORT ?? 3000);
const dtype = process.env.KOKORO_DTYPE ?? "q8";
const defaultVoice = process.env.KOKORO_VOICE ?? "af_heart";

console.log(`loading ${modelId} (dtype=${dtype}, device=cpu)`);
const tts = await KokoroTTS.from_pretrained(modelId, { dtype, device: "cpu" });

// Voice metadata keyed by voice id: { id: { name, language, gender, ... } }.
// list_voices() only prints a table and returns undefined, so use the getter.
const voiceMeta = tts.voices;
const voiceIds = Object.keys(voiceMeta);
console.log(`ready on http://localhost:${port} (${voiceIds.length} voices)`);

const readBody = (req) =>
  new Promise((resolve, reject) => {
    let data = "";
    req.on("data", (chunk) => (data += chunk));
    req.on("end", () => resolve(data));
    req.on("error", reject);
  });

const sendJSON = (res, status, payload) => {
  const body = JSON.stringify(payload);
  res.writeHead(status, { "content-type": "application/json", "content-length": Buffer.byteLength(body) });
  res.end(body);
};

const server = http.createServer(async (req, res) => {
  try {
    if (req.method === "GET" && req.url === "/v1/audio/voices") {
      return sendJSON(res, 200, {
        voices: voiceIds.map((id) => ({
          id,
          name: voiceMeta[id]?.name ?? id,
          language: voiceMeta[id]?.language,
          gender: voiceMeta[id]?.gender,
        })),
      });
    }

    if (req.method === "GET" && (req.url === "/health" || req.url === "/")) {
      return sendJSON(res, 200, { status: "ok", voices: voiceIds.length });
    }

    if (req.method === "POST" && (req.url === "/v1/audio/speech" || req.url === "/audio/speech")) {
      const body = JSON.parse((await readBody(req)) || "{}");
      const { input, voice = defaultVoice, response_format: format = "wav" } = body;

      if (!input) {
        return sendJSON(res, 400, { error: { message: "input is required" } });
      }
      if (format !== "wav") {
        return sendJSON(res, 400, { error: { message: `unsupported response_format ${format}; only wav` } });
      }
      if (!voiceIds.includes(voice)) {
        return sendJSON(res, 400, { error: { message: `unknown voice ${voice}` } });
      }

      const audio = await tts.generate(input, { voice });
      const wav = audio.toWav ? Buffer.from(audio.toWav()) : Buffer.from(await audio.toBlob().arrayBuffer());
      res.writeHead(200, { "content-type": "audio/wav", "content-length": wav.length });
      return res.end(wav);
    }

    return sendJSON(res, 404, { error: { message: `not found: ${req.method} ${req.url}` } });
  } catch (err) {
    return sendJSON(res, 500, { error: { message: String(err?.message ?? err) } });
  }
});

server.listen(port);
