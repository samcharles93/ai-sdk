package embed

import (
	"errors"
	"testing"
)

func TestNewIndex_RequiresModel(t *testing.T) {
	if _, err := NewIndex(""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("want ErrInvalidRequest, got %v", err)
	}
}

func TestIndex_AddAndSearch(t *testing.T) {
	idx, err := NewIndex("nomic-embed-text")
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Add("a", []float32{1, 0, 0}, map[string]any{"k": "a"}); err != nil {
		t.Fatal(err)
	}
	if err := idx.Add("b", []float32{0, 1, 0}, nil); err != nil {
		t.Fatal(err)
	}
	if err := idx.Add("c", []float32{0.9, 0.1, 0}, nil); err != nil {
		t.Fatal(err)
	}
	if idx.Len() != 3 || idx.Dim() != 3 {
		t.Fatalf("Len/Dim: %d/%d", idx.Len(), idx.Dim())
	}
	hits, err := idx.Search([]float32{1, 0, 0}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("want 2 hits, got %d", len(hits))
	}
	if hits[0].ID != "a" || hits[1].ID != "c" {
		t.Fatalf("ranking wrong: %+v", hits)
	}
	if hits[0].Score < hits[1].Score {
		t.Fatalf("scores not desc: %+v", hits)
	}
	if hits[0].Meta["k"] != "a" {
		t.Fatalf("meta lost: %+v", hits[0].Meta)
	}
}

func TestIndex_DimMismatch(t *testing.T) {
	idx, _ := NewIndex("m")
	_ = idx.Add("a", []float32{1, 2, 3}, nil)
	if err := idx.Add("b", []float32{1, 2}, nil); !errors.Is(err, ErrDimMismatch) {
		t.Fatalf("want ErrDimMismatch on Add, got %v", err)
	}
	if _, err := idx.Search([]float32{1, 2}, 1); !errors.Is(err, ErrDimMismatch) {
		t.Fatalf("want ErrDimMismatch on Search, got %v", err)
	}
}

func TestIndex_EnforceModel(t *testing.T) {
	idx, _ := NewIndex("m1")
	if err := idx.EnforceModel("m1"); err != nil {
		t.Fatalf("same model: %v", err)
	}
	if err := idx.EnforceModel("m2"); !errors.Is(err, ErrModelMismatch) {
		t.Fatalf("want ErrModelMismatch, got %v", err)
	}
}

func TestIndex_Empty(t *testing.T) {
	idx, _ := NewIndex("m")
	hits, err := idx.Search([]float32{1, 2, 3}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("empty index: want 0, got %d", len(hits))
	}
}

func TestIndex_TopKBounds(t *testing.T) {
	idx, _ := NewIndex("m")
	_ = idx.Add("a", []float32{1, 0}, nil)
	_ = idx.Add("b", []float32{0, 1}, nil)
	hits, _ := idx.Search([]float32{1, 0}, 0)
	if len(hits) != 0 {
		t.Fatalf("topK=0: want 0, got %d", len(hits))
	}
	hits, _ = idx.Search([]float32{1, 0}, 100)
	if len(hits) != 2 {
		t.Fatalf("topK>>n: want 2, got %d", len(hits))
	}
}

func TestIndex_AddValidation(t *testing.T) {
	idx, _ := NewIndex("m")
	if err := idx.Add("", []float32{1}, nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty id: want ErrInvalidRequest, got %v", err)
	}
	if err := idx.Add("x", nil, nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty vector: want ErrInvalidRequest, got %v", err)
	}
}

func TestIndex_AddEmbedding(t *testing.T) {
	idx, _ := NewIndex("m")
	if err := idx.AddEmbedding("a", Embedding{Index: 0, Vector: []float32{1, 2, 3}}, nil); err != nil {
		t.Fatal(err)
	}
	if idx.Len() != 1 {
		t.Fatalf("want 1 item, got %d", idx.Len())
	}
}
