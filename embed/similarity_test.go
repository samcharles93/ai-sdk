package embed

import (
	"errors"
	"math"
	"testing"
)

func TestCosineSimilarity_Identical(t *testing.T) {
	a := []float32{1, 2, 3}
	got, err := CosineSimilarity(a, a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(float64(got)-1.0) > 1e-6 {
		t.Fatalf("identical vectors: want 1, got %v", got)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	got, err := CosineSimilarity([]float32{1, 0}, []float32{0, 1})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(float64(got)) > 1e-6 {
		t.Fatalf("orthogonal: want 0, got %v", got)
	}
}

func TestCosineSimilarity_Opposite(t *testing.T) {
	got, err := CosineSimilarity([]float32{1, 1}, []float32{-1, -1})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(float64(got)+1.0) > 1e-6 {
		t.Fatalf("opposite: want -1, got %v", got)
	}
}

func TestCosineSimilarity_LengthMismatch(t *testing.T) {
	_, err := CosineSimilarity([]float32{1, 2}, []float32{1, 2, 3})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("want ErrInvalidRequest, got %v", err)
	}
}

func TestCosineSimilarity_Empty(t *testing.T) {
	_, err := CosineSimilarity(nil, nil)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("want ErrInvalidRequest, got %v", err)
	}
}

func TestCosineSimilarity_ZeroVector(t *testing.T) {
	_, err := CosineSimilarity([]float32{0, 0, 0}, []float32{1, 2, 3})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("want ErrInvalidRequest, got %v", err)
	}
}

func TestDotProduct(t *testing.T) {
	got, err := DotProduct([]float32{1, 2, 3}, []float32{4, 5, 6})
	if err != nil {
		t.Fatal(err)
	}
	if got != 32 {
		t.Fatalf("want 32, got %v", got)
	}
	if _, err := DotProduct([]float32{1}, []float32{1, 2}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("want ErrInvalidRequest, got %v", err)
	}
}

func TestNorm(t *testing.T) {
	got := Norm([]float32{3, 4})
	if math.Abs(float64(got)-5.0) > 1e-6 {
		t.Fatalf("want 5, got %v", got)
	}
}
