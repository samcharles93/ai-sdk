package embed

import (
	"errors"
	"fmt"
	"sort"
)

// IndexItem is a single entry stored in an Index: an opaque identifier, an
// embedding vector, and arbitrary application metadata.
type IndexItem struct {
	ID     string
	Vector []float32
	Meta   map[string]any
}

// SearchHit is a single result from Index.Search.
type SearchHit struct {
	ID    string
	Score float32
	Meta  map[string]any
}

// Index is a minimal in-memory vector index that enforces a single
// embedding model across all stored items. Embeddings produced by
// different models are not directly comparable, so Index refuses to mix
// them — callers must produce queries with the same Model that was used
// to build the index.
//
// Index is intended for small/medium workloads (tens of thousands of
// items). For larger corpora, persist to a vector database.
type Index struct {
	Model string
	Items []IndexItem

	dim int // 0 until first item; locks the dimensionality
}

// NewIndex returns an empty Index bound to the given embedding model.
// model must be non-empty.
func NewIndex(model string) (*Index, error) {
	if model == "" {
		return nil, fmt.Errorf("embed.NewIndex: %w: model required", ErrInvalidRequest)
	}
	return &Index{Model: model}, nil
}

// Add inserts an item into the index. Returns ErrDimMismatch when the
// vector's dimensionality differs from previously-added items, and
// ErrInvalidRequest for empty IDs or empty vectors.
func (idx *Index) Add(id string, vector []float32, meta map[string]any) error {
	if id == "" {
		return fmt.Errorf("embed.Index.Add: %w: id required", ErrInvalidRequest)
	}
	if len(vector) == 0 {
		return fmt.Errorf("embed.Index.Add: %w: empty vector", ErrInvalidRequest)
	}
	if idx.dim == 0 {
		idx.dim = len(vector)
	} else if len(vector) != idx.dim {
		return fmt.Errorf("embed.Index.Add: %w: have %d, got %d", ErrDimMismatch, idx.dim, len(vector))
	}
	v := make([]float32, len(vector))
	copy(v, vector)
	idx.Items = append(idx.Items, IndexItem{ID: id, Vector: v, Meta: meta})
	return nil
}

// AddEmbedding adds an Embedding to the index using its Vector. It is a
// convenience for use with Provider.Embed results.
func (idx *Index) AddEmbedding(id string, e Embedding, meta map[string]any) error {
	return idx.Add(id, e.Vector, meta)
}

// Dim reports the dimensionality of vectors stored in the index, or 0 if
// no items have been added yet.
func (idx *Index) Dim() int { return idx.dim }

// Len reports the number of items in the index.
func (idx *Index) Len() int { return len(idx.Items) }

// EnforceModel returns ErrModelMismatch if queryModel differs from the
// model bound to the index. Callers should call EnforceModel before
// performing a search whose query vector was produced by a separate
// embedding call, to catch model drift early.
func (idx *Index) EnforceModel(queryModel string) error {
	if queryModel != idx.Model {
		return fmt.Errorf("embed.Index.EnforceModel: %w: index=%q, query=%q", ErrModelMismatch, idx.Model, queryModel)
	}
	return nil
}

// Search returns the topK most-similar items to query by cosine
// similarity, sorted descending by Score. topK <= 0 returns an empty
// result. Returns ErrDimMismatch when the query vector dimensionality
// differs from the index, and ErrInvalidRequest when the index is empty
// or the query vector is empty.
//
// Search does NOT validate that the query was produced by Index.Model —
// call EnforceModel separately when you have the query model available.
func (idx *Index) Search(query []float32, topK int) ([]SearchHit, error) {
	if len(query) == 0 {
		return nil, fmt.Errorf("embed.Index.Search: %w: empty query", ErrInvalidRequest)
	}
	if len(idx.Items) == 0 {
		return nil, nil
	}
	if idx.dim != 0 && len(query) != idx.dim {
		return nil, fmt.Errorf("embed.Index.Search: %w: index=%d, query=%d", ErrDimMismatch, idx.dim, len(query))
	}
	if topK <= 0 {
		return nil, nil
	}
	hits := make([]SearchHit, 0, len(idx.Items))
	for _, it := range idx.Items {
		score, err := CosineSimilarity(query, it.Vector)
		if err != nil {
			// Skip zero-magnitude items rather than aborting the whole search.
			if errors.Is(err, ErrInvalidRequest) {
				continue
			}
			return nil, err
		}
		hits = append(hits, SearchHit{ID: it.ID, Score: score, Meta: it.Meta})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if topK < len(hits) {
		hits = hits[:topK]
	}
	return hits, nil
}
