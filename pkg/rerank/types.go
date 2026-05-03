package rerank

// Request is a provider-agnostic reranking request.
type Request struct {
	// Model identifies the reranking model to use.
	Model string `json:"model"`
	// Query is the search query to rank documents against.
	Query string `json:"query"`
	// Documents are the texts to rerank. Providers may also support
	// structured documents (objects with text fields). For those cases
	// the text content should be flattened into this slice.
	Documents []string `json:"documents"`
	// TopN limits the result to the top N documents. When 0 (or unset)
	// all documents are returned in ranked order.
	TopN int `json:"top_n,omitempty"`
	// ProviderOptions carries provider-specific options.
	ProviderOptions map[string]any `json:"provider_options,omitempty"`
}

// RankingItem represents a single document in the ranked result set.
type RankingItem struct {
	// OriginalIndex is the index into [Request.Documents] this item
	// came from (0-indexed).
	OriginalIndex int `json:"original_index"`
	// Score is the relevance score assigned by the model. Higher is
	// more relevant. The score range is model-dependent.
	Score float64 `json:"score"`
	// Document is the document text (same as the input).
	Document string `json:"document"`
}

// Response is the result of a reranking request. Items appear in
// descending order of relevance (highest Score first).
type Response struct {
	// Model identifies the model that produced this result.
	Model string `json:"model,omitempty"`
	// Ranking contains the ranked documents.
	Ranking []RankingItem `json:"ranking"`
	// Warnings contains non-fatal provider warnings.
	Warnings []string `json:"warnings,omitempty"`
}
