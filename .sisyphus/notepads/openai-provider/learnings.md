## OpenAI Provider Implementation

### Key Patterns
- DeepSeek provider is the best reference — it already speaks OpenAI-compatible wire format
- OpenAI path is `/v1/chat/completions` (not `/chat/completions` like DeepSeek)
- OpenAI does NOT have `reasoning_content` in wire format (DeepSeek-specific field for deepseek-reasoner)
- ReasoningPart on input: appended to text for user/system, warned+dropped for assistant (no replay mechanism)
- Stream options: `stream_options: {include_usage: true}` works same as DeepSeek for trailing usage chunk
- SSE buffered-final pattern: identical to DeepSeek — defer Done chunk until trailing usage arrives

### Test Coverage
- 9 tests covering: API key validation, non-streaming chat, streaming SSE, tool calls (non-streaming + streaming), auth error, rate limit, empty model, empty messages
- All tests pass with `-race` flag
- go vet passes cleanly
