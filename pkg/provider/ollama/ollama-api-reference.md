# Ollama API Reference — Complete Provider Resource

> Source: [ollama/ollama docs/api.md](https://github.com/ollama/ollama/blob/main/docs/api.md) and [docs.ollama.com](https://docs.ollama.com)

---

## 1. Introduction

Ollama's API allows you to run and interact with models programmatically.

**Base URL** (local): `http://localhost:11434/api`

**Base URL** (cloud): `https://ollama.com/api`

**Libraries**: [Python](https://github.com/ollama/ollama-python) · [JavaScript](https://github.com/ollama/ollama-js)

**Versioning**: API is stable and backwards-compatible. Deprecations are announced in [release notes](https://github.com/ollama/ollama/releases).

---

## 2. Authentication

No authentication is required when accessing Ollama's API locally via `http://localhost:11434`.

Authentication is required for:
- Running cloud models via ollama.com
- Publishing models
- Downloading private models

**Two authentication methods:**

### Signing in
```bash
ollama signin
```
Once signed in, Ollama automatically authenticates commands.

### API Keys
Create an [API key](https://ollama.com/settings/keys), then:
```bash
export OLLAMA_API_KEY=your_api_key
curl https://ollama.com/api/generate \
  -H "Authorization: Bearer $OLLAMA_API_KEY" \
  -d '{"model": "gpt-oss:120b", "prompt": "Why is the sky blue?", "stream": false}'
```

API keys do not expire but can be revoked at any time.

---

## 3. Streaming

Certain API endpoints stream responses by default (e.g., `/api/generate`, `/api/chat`). Responses use newline-delimited JSON (`application/x-ndjson`).

```json
{"model":"gemma3","created_at":"2025-10-26T17:15:24.097767Z","response":"That","done":false}
{"model":"gemma3","created_at":"2025-10-26T17:15:24.166576Z","response":"!","done":true, "done_reason":"stop"}
```

**Disable streaming**: Set `{"stream": false}` in the request body.

**When to use each:**
- **Streaming**: Real-time generation, lower perceived latency, better for long generations
- **Non-streaming**: Simpler to process, better for short responses or structured outputs

---

## 4. Usage / Metrics

API responses include performance metrics (all timing in nanoseconds):

| Field | Description |
|---|---|
| `total_duration` | Total time spent generating the response |
| `load_duration` | Time spent loading the model |
| `prompt_eval_count` | Number of input tokens processed |
| `prompt_eval_duration` | Time spent evaluating the prompt |
| `eval_count` | Number of output tokens generated |
| `eval_duration` | Time spent generating output tokens |

**Tokens per second**: `eval_count / eval_duration * 10^9`

For streaming endpoints, metrics appear only in the final chunk where `done: true`.

---

## 5. Errors

### HTTP Status Codes
- `200` — Success
- `400` — Bad Request (missing parameters, invalid JSON)
- `404` — Not Found (model doesn't exist)
- `429` — Too Many Requests (rate limit exceeded)
- `500` — Internal Server Error
- `502` — Bad Gateway (cloud model unreachable)

### Error Response Format
```json
{"error": "the model failed to generate a response"}
```

### Errors During Streaming
If an error occurs mid-stream, an error object appears inline:
```json
{"model":"gemma3","response":"Yes","done":false}
{"error":"an error was encountered while running the model"}
```
The HTTP status code will NOT change (the response already started).

---

## 6. OpenAI Compatibility

Ollama provides compatibility with the [OpenAI API](https://platform.openai.com/docs/api-reference).

**Base URL**: `http://localhost:11434/v1/`

### Supported Endpoints

| Endpoint | Status |
|---|---|
| `/v1/chat/completions` | Full support |
| `/v1/completions` | Full support |
| `/v1/models` | Supported |
| `/v1/models/{model}` | Supported |
| `/v1/embeddings` | Supported |
| `/v1/images/generations` | Experimental |
| `/v1/responses` | Supported (non-stateful only) |

### `/v1/chat/completions` — Supported Features
✅ Chat completions, Streaming, JSON mode, Reproducible outputs, Vision, Tools, Reasoning/thinking (`"high"`, `"medium"`, `"low"`, `"none"`)
❌ Logprobs, `tool_choice`, `logit_bias`, `user`, `n`

### `/v1/chat/completions` — Supported Request Fields
✅ `model`, `messages` (text, base64 images, array of content parts), `frequency_penalty`, `presence_penalty`, `response_format`, `seed`, `stop`, `stream`, `stream_options.include_usage`, `temperature`, `top_p`, `max_tokens`, `tools`, `reasoning_effort`, `reasoning.effort`

### `/v1/completions` — Notes
- `prompt` currently only accepts a string

### `/v1/embeddings` — Supported Request Fields
✅ `model`, `input` (string, array of strings), `encoding format`, `dimensions`
❌ Array of tokens, array of token arrays, `user`

### `/v1/images/generations` (experimental)
✅ `model`, `prompt`, `size`, `response_format` (only `b64_json`)
❌ `n`, `quality`, `style`, `user`

### `/v1/responses` (Ollama v0.13.3+)
Non-stateful only (no `previous_response_id` or `conversation`). Supports streaming, tools, and reasoning summaries.

### Quick Start
```python
from openai import OpenAI
client = OpenAI(base_url='http://localhost:11434/v1/', api_key='ollama')
chat_completion = client.chat.completions.create(
    messages=[{'role': 'user', 'content': 'Say this is a test'}],
    model='gpt-oss:20b',
)
```

```bash
curl http://localhost:11434/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-oss:20b", "messages": [{"role": "user", "content": "Say this is a test"}]}'
```

### Default Model Names
For tooling that expects OpenAI model names:
```bash
ollama cp llama3.2 gpt-3.5-turbo
```

### Setting Context Size
Create a `Modelfile`:
```
FROM <some model>
PARAMETER num_ctx <context size>
```
Then: `ollama create mymodel`

---

## 7. Anthropic Compatibility

Ollama provides compatibility with the [Anthropic Messages API](https://docs.anthropic.com/en/api/messages).

**Base URL**: `http://localhost:11434`

### Environment Variables
```bash
export ANTHROPIC_AUTH_TOKEN=ollama  # required but ignored
export ANTHROPIC_BASE_URL=http://localhost:11434
```

### `/v1/messages` — Supported Features
✅ Messages, Streaming, System prompts, Multi-turn conversations, Vision (images), Tools (function calling), Tool results, Thinking/extended thinking
❌ `/v1/messages/count_tokens`, `tool_choice`, `metadata`, Prompt caching, Batches API, Citations, PDF support

### `/v1/messages` — Supported Request Fields
✅ `model`, `max_tokens`, `messages` (text, base64 images, array of content blocks, tool_use blocks, tool_result blocks, thinking blocks), `system` (string or array), `stream`, `temperature`, `top_p`, `top_k`, `stop_sequences`, `tools`, `thinking`
❌ `tool_choice`, `metadata`

### `/v1/messages` — Supported Response Fields
✅ `id`, `type`, `role`, `model`, `content` (text, tool_use, thinking blocks), `stop_reason` (end_turn, max_tokens, tool_use), `usage` (input_tokens, output_tokens)

### Streaming Events
✅ `message_start`, `content_block_start`, `content_block_delta` (text_delta, input_json_delta, thinking_delta), `content_block_stop`, `message_delta`, `message_stop`, `ping`, `error`

### Quick Start
```python
import anthropic
client = anthropic.Anthropic(base_url='http://localhost:11434', api_key='ollama')
message = client.messages.create(
    model='qwen3-coder', max_tokens=1024,
    messages=[{'role': 'user', 'content': 'Hello, how are you?'}]
)
```

```bash
curl -X POST http://localhost:11434/v1/messages \
  -H "Content-Type: application/json" -H "x-api-key: ollama" \
  -H "anthropic-version: 2023-06-01" \
  -d '{"model": "qwen3-coder", "max_tokens": 1024, "messages": [{"role": "user", "content": "Hello"}]}'
```

### Claude Code Integration
```bash
ollama launch claude
# or manual:
ANTHROPIC_AUTH_TOKEN=ollama ANTHROPIC_BASE_URL=http://localhost:11434 claude --model qwen3-coder
```

### Differences from Anthropic API
- API key is accepted but not validated
- `anthropic-version` header is accepted but not used
- Token counts are approximations
- URL images not supported (base64 only)
- Extended thinking: `budget_tokens` accepted but not enforced

---

## 8. Conventions

### Model Names
Format: `model:tag` (e.g., `llama3:70b`, `orca-mini:3b-q8_0`). Optional namespace: `example/model`. Tag defaults to `latest`.

### Durations
All durations are returned in **nanoseconds**.

---

## 9. Endpoint: POST `/api/generate` — Generate a Completion

Generate a response for a given prompt. **Streaming endpoint** (series of JSON objects).

### Request Parameters
| Parameter | Type | Required | Description |
|---|---|---|---|
| `model` | string | ✅ | Model name |
| `prompt` | string | — | The prompt to generate for |
| `suffix` | string | — | Text after the model response |
| `images` | string[] | — | Base64-encoded images (multimodal models) |
| `think` | boolean | — | Enable thinking for thinking models |
| `format` | string/object | — | `"json"` or JSON schema for structured output |
| `options` | object | — | Model parameters (temperature, etc.) |
| `system` | string | — | System message (overrides Modelfile) |
| `template` | string | — | Prompt template (overrides Modelfile) |
| `stream` | boolean | — | `false` for single response object (default: streaming) |
| `raw` | boolean | — | If `true`, bypass template formatting; no context returned |
| `keep_alive` | string | — | Model keep-alive duration (default: `"5m"`) |
| `context` | number[] | — | **(deprecated)** Context from previous request |

### Experimental Image Generation Parameters
| Parameter | Type | Description |
|---|---|---|
| `width` | integer | Width of generated image in pixels |
| `height` | integer | Height of generated image in pixels |
| `steps` | integer | Number of diffusion steps |

### Model Options (in `options` object)
| Option | Type | Description |
|---|---|---|
| `num_keep` | integer | Tokens to keep in context |
| `seed` | integer | Random seed for reproducibility |
| `num_predict` | integer | Max tokens to generate |
| `top_k` | integer | Top-K sampling |
| `top_p` | float | Top-P (nucleus) sampling |
| `min_p` | float | Minimum probability threshold |
| `typical_p` | float | Typical sampling |
| `repeat_last_n` | integer | Tokens to look back for repetition penalty |
| `temperature` | float | Temperature (higher = more random) |
| `repeat_penalty` | float | Repetition penalty |
| `presence_penalty` | float | Presence penalty |
| `frequency_penalty` | float | Frequency penalty |
| `penalize_newline` | boolean | Penalize newline tokens |
| `stop` | string/string[] | Stop sequences |
| `numa` | boolean | NUMA support |
| `num_ctx` | integer | Context window size |
| `num_batch` | integer | Batch size |
| `num_gpu` | integer | Number of GPUs |
| `main_gpu` | integer | Primary GPU index |
| `use_mmap` | boolean | Memory-mapped model loading |
| `num_thread` | integer | CPU thread count |

### Response Fields
| Field | Description |
|---|---|
| `model` | Model name |
| `created_at` | ISO 8601 timestamp |
| `response` | Generated text (empty when streaming; full response when `stream: false`) |
| `done` | `true` for final chunk |
| `done_reason` | `"stop"`, `"load"`, or `"unload"` |
| `context` | Token encoding for conversation memory |
| `total_duration` | Total generation time (ns) |
| `load_duration` | Model loading time (ns) |
| `prompt_eval_count` | Input token count |
| `prompt_eval_duration` | Prompt evaluation time (ns) |
| `eval_count` | Output token count |
| `eval_duration` | Output generation time (ns) |

### Examples

**Basic streaming request:**
```bash
curl http://localhost:11434/api/generate -d '{
  "model": "llama3.2",
  "prompt": "Why is the sky blue?"
}'
```

**Non-streaming request:**
```bash
curl http://localhost:11434/api/generate -d '{
  "model": "llama3.2",
  "prompt": "Why is the sky blue?",
  "stream": false
}'
```

**With suffix (code completion):**
```bash
curl http://localhost:11434/api/generate -d '{
  "model": "codellama:code",
  "prompt": "def compute_gcd(a, b):",
  "suffix": "    return result",
  "options": {"temperature": 0},
  "stream": false
}'
```

**Structured output (JSON schema):**
```bash
curl -X POST http://localhost:11434/api/generate -H "Content-Type: application/json" -d '{
  "model": "llama3.1:8b",
  "prompt": "Ollama is 22 years old and is busy saving the world. Respond using JSON",
  "stream": false,
  "format": {
    "type": "object",
    "properties": {"age": {"type": "integer"}, "available": {"type": "boolean"}},
    "required": ["age", "available"]
  }
}'
```

**JSON mode:**
```bash
curl http://localhost:11434/api/generate -d '{
  "model": "llama3.2",
  "prompt": "What color is the sky at different times of the day? Respond using JSON",
  "format": "json",
  "stream": false
}'
```

**With images (vision):**
```bash
curl http://localhost:11434/api/generate -d '{
  "model": "llava",
  "prompt": "What is in this picture?",
  "stream": false,
  "images": ["<base64-encoded-image>"]
}'
```

**Raw mode (bypass template):**
```bash
curl http://localhost:11434/api/generate -d '{
  "model": "mistral",
  "prompt": "[INST] why is the sky blue? [/INST]",
  "raw": true,
  "stream": false
}'
```

**Reproducible outputs:**
```bash
curl http://localhost:11434/api/generate -d '{
  "model": "mistral",
  "prompt": "Why is the sky blue?",
  "options": {"seed": 123}
}'
```

**Load a model (empty prompt):**
```bash
curl http://localhost:11434/api/generate -d '{"model": "llama3.2"}'
```

**Unload a model:**
```bash
curl http://localhost:11434/api/generate -d '{"model": "llama3.2", "keep_alive": 0}'
# Response: {"done_reason": "unload"}
```

---

## 10. Endpoint: POST `/api/chat` — Chat Completion

Generate the next message in a conversation. **Streaming endpoint**.

### Request Parameters
| Parameter | Type | Required | Description |
|---|---|---|---|
| `model` | string | ✅ | Model name |
| `messages` | object[] | — | Chat message history |
| `tools` | object[] | — | Tool definitions for function calling |
| `think` | boolean | — | Enable thinking for thinking models |
| `format` | string/object | — | `"json"` or JSON schema |
| `options` | object | — | Model parameters |
| `stream` | boolean | — | `false` for single response |
| `keep_alive` | string | — | Model keep-alive duration (default: `"5m"`) |

### Message Object
| Field | Type | Description |
|---|---|---|
| `role` | string | `"system"`, `"user"`, `"assistant"`, or `"tool"` |
| `content` | string | Message content |
| `thinking` | string | Thinking process (thinking models) |
| `images` | string[] | Base64-encoded images (multimodal) |
| `tool_calls` | object[] | Tool calls from the model |
| `tool_name` | string | Name of executed tool (for tool results) |

### Tool Calling
Supported by providing a `tools` list:
```json
{
  "type": "function",
  "function": {
    "name": "get_weather",
    "description": "Get the weather in a given city",
    "parameters": {
      "type": "object",
      "properties": {"city": {"type": "string", "description": "The city"}},
      "required": ["city"]
    }
  }
}
```

Response includes `tool_calls` in the message:
```json
{"message": {"role": "assistant", "content": "", "tool_calls": [{"function": {"name": "get_weather", "arguments": {"city": "Tokyo"}}}]}}
```

Models can explain tool results by including the result with `role: "tool"` and `tool_name`.

### Examples

**Basic chat (streaming):**
```bash
curl http://localhost:11434/api/chat -d '{
  "model": "llama3.2",
  "messages": [{"role": "user", "content": "why is the sky blue?"}]
}'
```

**With tools:**
```bash
curl http://localhost:11434/api/chat -d '{
  "model": "llama3.2",
  "messages": [{"role": "user", "content": "what is the weather in tokyo?"}],
  "stream": false,
  "tools": [{
    "type": "function",
    "function": {
      "name": "get_weather",
      "description": "Get the weather in a given city",
      "parameters": {
        "type": "object",
        "properties": {"city": {"type": "string", "description": "The city"}},
        "required": ["city"]
      }
    }
  }]
}'
```

**With history and tools:**
```bash
curl http://localhost:11434/api/chat -d '{
  "model": "llama3.2",
  "messages": [
    {"role": "user", "content": "what is the weather in Toronto?"},
    {"role": "assistant", "content": "", "tool_calls": [{"function": {"name": "get_weather", "arguments": {"city": "Toronto"}}}]},
    {"role": "tool", "content": "11 degrees celsius", "tool_name": "get_weather"}
  ],
  "stream": false,
  "tools": [...]
}'
```

**Structured outputs:**
```bash
curl -X POST http://localhost:11434/api/chat -H "Content-Type: application/json" -d '{
  "model": "llama3.1",
  "messages": [{"role": "user", "content": "Ollama is 22 years old and busy saving the world. Return a JSON object with the age and availability."}],
  "stream": false,
  "format": {
    "type": "object",
    "properties": {"age": {"type": "integer"}, "available": {"type": "boolean"}},
    "required": ["age", "available"]
  },
  "options": {"temperature": 0}
}'
```

**With images:**
```bash
curl http://localhost:11434/api/chat -d '{
  "model": "llava",
  "messages": [{"role": "user", "content": "what is in this image?", "images": ["<base64>"]}]
}'
```

**Reproducible outputs:**
```bash
curl http://localhost:11434/api/chat -d '{
  "model": "llama3.2",
  "messages": [{"role": "user", "content": "Hello!"}],
  "options": {"seed": 101, "temperature": 0}
}'
```

**Load / Unload a model:**
```bash
# Load
curl http://localhost:11434/api/chat -d '{"model": "llama3.2", "messages": []}'
# Response: {"done_reason": "load"}

# Unload
curl http://localhost:11434/api/chat -d '{"model": "llama3.2", "messages": [], "keep_alive": 0}'
# Response: {"done_reason": "unload"}
```

---

## 11. Endpoint: POST `/api/embed` — Generate Embeddings

Creates vector embeddings representing the input text.

### Request Parameters
| Parameter | Type | Required | Description |
|---|---|---|---|
| `model` | string | ✅ | Model name |
| `input` | string/string[] | ✅ | Text or array of texts |
| `truncate` | boolean | — | Truncate inputs exceeding context window (default: `true`) |
| `dimensions` | integer | — | Number of embedding dimensions |
| `keep_alive` | string | — | Model keep-alive duration |
| `options` | object | — | Model options |

### Response Fields
| Field | Description |
|---|---|
| `model` | Model that produced the embeddings |
| `embeddings` | Array of vector arrays |
| `total_duration` | Total time (ns) |
| `load_duration` | Load time (ns) |
| `prompt_eval_count` | Input token count |

### Examples
```bash
# Single input
curl http://localhost:11434/api/embed -d '{
  "model": "embeddinggemma",
  "input": "Why is the sky blue?"
}'

# Multiple inputs
curl http://localhost:11434/api/embed -d '{
  "model": "embeddinggemma",
  "input": ["Why is the sky blue?", "Why is the grass green?"]
}'

# With truncation control
curl http://localhost:11434/api/embed -d '{
  "model": "embeddinggemma",
  "input": "Generate embeddings for this text",
  "truncate": true
}'

# With dimensions
curl http://localhost:11434/api/embed -d '{
  "model": "embeddinggemma",
  "input": "Generate embeddings for this text",
  "dimensions": 128
}'
```

---

## 12. Endpoint: GET `/api/tags` — List Models

Fetch a list of locally available models and their details.

### Request
```bash
curl http://localhost:11434/api/tags
```

### Response
```json
{
  "models": [
    {
      "name": "gemma3",
      "model": "gemma3",
      "modified_at": "2025-10-03T23:34:03.409490317-07:00",
      "size": 3338801804,
      "digest": "a2af6cc3eb7fa8be8504abaf9b04e88f17a119ec3f04a3addf55f92841195f5a",
      "details": {
        "format": "gguf",
        "family": "gemma",
        "families": ["gemma"],
        "parameter_size": "4.3B",
        "quantization_level": "Q4_K_M"
      }
    }
  ]
}
```

---

## 13. Endpoint: GET `/api/ps` — List Running Models

Retrieve models currently loaded into memory.

### Request
```bash
curl http://localhost:11434/api/ps
```

### Response
```json
{
  "models": [
    {
      "name": "gemma3",
      "model": "gemma3",
      "size": 6591830464,
      "digest": "a2af6cc3eb7fa8be8504abaf9b04e88f17a119ec3f04a3addf55f92841195f5a",
      "details": {
        "parent_model": "",
        "format": "gguf",
        "family": "gemma3",
        "families": ["gemma3"],
        "parameter_size": "4.3B",
        "quantization_level": "Q4_K_M"
      },
      "expires_at": "2025-10-17T16:47:07.93355-07:00",
      "size_vram": 5333539264,
      "context_length": 4096
    }
  ]
}
```

---

## 14. Endpoint: POST `/api/show` — Show Model Details

Retrieve detailed information about a model.

### Request Parameters
| Parameter | Type | Required | Description |
|---|---|---|---|
| `model` | string | ✅ | Model name |
| `verbose` | boolean | — | Include large verbose fields |

### Request
```bash
curl http://localhost:11434/api/show -d '{"model": "gemma3"}'
curl http://localhost:11434/api/show -d '{"model": "gemma3", "verbose": true}'
```

### Response Fields
| Field | Description |
|---|---|
| `parameters` | Model parameter settings serialized as text |
| `license` | Model license text |
| `modified_at` | Last modified ISO 8601 timestamp |
| `details` | High-level model details (format, family, parameter_size, quantization_level) |
| `template` | Prompt template |
| `capabilities` | List of supported features (e.g., `["completion", "vision"]`) |
| `model_info` | Detailed model metadata (architecture, tokenizer config, context length, etc.) |

---

## 15. Endpoint: POST `/api/create` — Create a Model

Create a model from an existing model, safetensors directory, or GGUF file.

### Request Parameters
| Parameter | Type | Required | Description |
|---|---|---|---|
| `model` | string | ✅ | Name for the new model |
| `from` | string | — | Existing model to base on |
| `template` | string | — | Prompt template |
| `license` | string/string[] | — | License(s) |
| `system` | string | — | System prompt |
| `parameters` | object | — | Key-value model parameters |
| `messages` | object[] | — | Message history |
| `quantize` | string | — | Quantization level (e.g., `"q4_K_M"`, `"q8_0"`) |
| `stream` | boolean | — | Stream status updates (default: `true`) |

### Examples
```bash
# Create from existing model
curl http://localhost:11434/api/create -d '{
  "from": "gemma3",
  "model": "alpaca",
  "system": "You are Alpaca, a helpful AI assistant. You only answer with Emojis."
}'

# Quantize an existing model
curl http://localhost:11434/api/create -d '{
  "model": "llama3.1:8b-instruct-Q4_K_M",
  "from": "llama3.1:8b-instruct-fp16",
  "quantize": "q4_K_M"
}'
```

### Response (streaming)
```json
{"status": "creating model layer"}
{"status": "writing model layer"}
{"status": "success"}
```

---

## 16. Endpoint: POST `/api/copy` — Copy a Model

Copy a model to a new name.

### Request Parameters
| Parameter | Type | Required | Description |
|---|---|---|---|
| `source` | string | ✅ | Existing model name |
| `destination` | string | ✅ | New model name |

### Example
```bash
curl http://localhost:11434/api/copy -d '{
  "source": "gemma3",
  "destination": "gemma3-backup"
}'
```

---

## 17. Endpoint: POST `/api/pull` — Pull a Model

Download a model from the registry.

### Request Parameters
| Parameter | Type | Required | Description |
|---|---|---|---|
| `model` | string | ✅ | Model name to download |
| `insecure` | boolean | — | Allow insecure connections |
| `stream` | boolean | — | Stream progress updates (default: `true`) |

### Examples
```bash
# Streaming pull
curl http://localhost:11434/api/pull -d '{"model": "gemma3"}'

# Non-streaming pull
curl http://localhost:11434/api/pull -d '{"model": "gemma3", "stream": false}'
```

### Response (streaming)
```json
{"status": "pulling manifest"}
{"status": "downloading a2af6cc3", "digest": "a2af6cc3...", "total": 3338801804, "completed": 1048576}
{"status": "success"}
```

---

## 18. Endpoint: POST `/api/push` — Push a Model

Publish a model to the registry.

### Request Parameters
| Parameter | Type | Required | Description |
|---|---|---|---|
| `model` | string | ✅ | Model name to publish (e.g., `my-username/my-model`) |
| `insecure` | boolean | — | Allow insecure connections |
| `stream` | boolean | — | Stream progress updates (default: `true`) |

### Examples
```bash
curl http://localhost:11434/api/push -d '{"model": "my-username/my-model"}'
curl http://localhost:11434/api/push -d '{"model": "my-username/my-model", "stream": false}'
```

---

## 19. Endpoint: DELETE `/api/delete` — Delete a Model

Delete a model and its data.

### Request Parameters
| Parameter | Type | Required | Description |
|---|---|---|---|
| `model` | string | ✅ | Model name to delete |

### Example
```bash
curl -X DELETE http://localhost:11434/api/delete -d '{"model": "gemma3"}'
```

---

## 20. Endpoint: GET `/api/version` — Get Version

Retrieve the running Ollama version.

### Request
```bash
curl http://localhost:11434/api/version
```

### Response
```json
{"version": "0.12.6"}
```

---

## 21. Structured Outputs

Structured outputs constrain model responses to a specific JSON schema. Supported in both `/api/generate` and `/api/chat`.

### Usage
- Set `format` to `"json"` for JSON mode
- Set `format` to a JSON schema object for structured output
- Pydantic (Python) or Zod (JavaScript) recommended for schema definition and response validation

### Python Example
```python
from ollama import chat
from pydantic import BaseModel

class Country(BaseModel):
    name: str
    capital: str
    languages: list[str]

response = chat(
    model='gpt-oss',
    messages=[{'role': 'user', 'content': 'Tell me about Canada.'}],
    format=Country.model_json_schema(),
)
country = Country.model_validate_json(response.message.content)
```

### JavaScript Example
```javascript
import ollama from 'ollama'
import { z } from 'zod'
import { zodToJsonSchema } from 'zod-to-json-schema'

const Country = z.object({ name: z.string(), capital: z.string(), languages: z.array(z.string()) })
const response = await ollama.chat({
    model: 'gpt-oss',
    messages: [{ role: 'user', content: 'Tell me about Canada.' }],
    format: zodToJsonSchema(Country),
})
const country = Country.parse(JSON.parse(response.message.content))
```

### Tips
- Lower temperature (to `0`) for deterministic output
- Add "return as JSON" to prompt
- Structured outputs work through OpenAI compatibility via `response_format`

---

## 22. Tool Calling / Function Calling

Ollama supports tool calling where models can invoke defined tools and use results in follow-up responses.

### Tool Definition Format
```json
{
  "type": "function",
  "function": {
    "name": "get_temperature",
    "description": "Get the current temperature for a city",
    "parameters": {
      "type": "object",
      "required": ["city"],
      "properties": {
        "city": {"type": "string", "description": "The name of the city"}
      }
    }
  }
}
```

### Workflow
1. Send user message + tool definitions → model returns `tool_calls`
2. Execute the tool, append result as `{"role": "tool", "tool_name": "...", "content": "..."}`
3. Send the full conversation back → model generates final answer

### Multi-turn Agent Loop (Python)
```python
from ollama import chat

messages = [{'role': 'user', 'content': 'What is (11434+12341)*412?'}]
while True:
    response = chat(model='qwen3', messages=messages, tools=[add, multiply], think=True)
    messages.append(response.message)
    if response.message.tool_calls:
        for tc in response.message.tool_calls:
            result = available_functions[tc.function.name](**tc.function.arguments)
            messages.append({'role': 'tool', 'tool_name': tc.function.name, 'content': str(result)})
    else:
        break
```

### Streaming Tool Calling
When streaming, accumulate `thinking`, `content`, and `tool_calls` across chunks, then return them together with tool results.

### Python SDK Function-as-Tool
```python
from ollama import chat

def get_temperature(city: str) -> str:
    """Get the current temperature for a city"""
    return temperatures.get(city, 'Unknown')

response = chat(model='qwen3', messages=messages, tools=[get_temperature], think=True)
```

### Supported Models
Models with tool calling capabilities can be found at [ollama.com/search?c=tool](https://ollama.com/search?c=tool).

---

## Summary — All Endpoints

| Method | Path | Description |
|---|---|---|
| POST | `/api/generate` | Generate a completion |
| POST | `/api/chat` | Generate a chat completion |
| POST | `/api/embed` | Generate embeddings |
| GET | `/api/tags` | List local models |
| GET | `/api/ps` | List running models |
| POST | `/api/show` | Show model details |
| POST | `/api/create` | Create a model |
| POST | `/api/copy` | Copy a model |
| POST | `/api/pull` | Pull (download) a model |
| POST | `/api/push` | Push (publish) a model |
| DELETE | `/api/delete` | Delete a model |
| GET | `/api/version` | Get Ollama version |

### OpenAI-Compatible Endpoints

| Method | Path | Description |
|---|---|---|
| POST | `/v1/chat/completions` | Chat completions |
| POST | `/v1/completions` | Text completions |
| POST | `/v1/embeddings` | Embeddings |
| GET | `/v1/models` | List models |
| GET | `/v1/models/{model}` | Get model info |
| POST | `/v1/images/generations` | Image generation (experimental) |
| POST | `/v1/responses` | Responses API (non-stateful, v0.13.3+) |

### Anthropic-Compatible Endpoints

| Method | Path | Description |
|---|---|---|
| POST | `/v1/messages` | Messages API |
