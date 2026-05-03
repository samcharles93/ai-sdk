package embed

// Request is a provider-agnostic embedding request.
//
// Inputs is a batch of one or more strings to embed; providers must produce
// one Embedding per input, preserving order via Embedding.Index.
//
// ProviderOptions carries provider-specific options keyed by provider
// name (e.g. "ollama", "gemini"). See chat.ProviderOptionsFor for the
// extraction helper; embed providers use the same pattern.
type Request struct {
	Model           string         `json:"model"`
	Inputs          []string       `json:"inputs"`
	ProviderOptions map[string]any `json:"provider_options,omitempty"`
}

// Embedding is a single embedding vector produced for one input in a Request.
//
// Index identifies which entry in Request.Inputs this vector corresponds to.
type Embedding struct {
	Index  int       `json:"index"`
	Vector []float32 `json:"vector"`
}

// Usage reports token accounting for an embedding request.
type Usage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// Response is the result of an embedding request. Embeddings appear in the
// same order as Request.Inputs.
type Response struct {
	Model      string      `json:"model,omitempty"`
	Embeddings []Embedding `json:"embeddings"`
	Usage      Usage       `json:"usage"`
}
