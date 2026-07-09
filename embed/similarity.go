package embed

import (
	"errors"
	"math"
)

// CosineSimilarity returns the cosine similarity between two equal-length
// vectors a and b. The result is in the range [-1, 1] for non-zero vectors:
// 1 means identical direction, 0 means orthogonal, -1 means opposite.
//
// CosineSimilarity returns ErrInvalidRequest when the vectors differ in
// length or either vector has zero magnitude (cosine similarity is
// undefined for the zero vector).
func CosineSimilarity(a, b []float32) (float32, error) {
	if len(a) != len(b) {
		return 0, errors.Join(ErrInvalidRequest, errors.New("embed: vector length mismatch"))
	}
	if len(a) == 0 {
		return 0, errors.Join(ErrInvalidRequest, errors.New("embed: empty vectors"))
	}
	var dot, na, nb float64
	for i := range a {
		af := float64(a[i])
		bf := float64(b[i])
		dot += af * bf
		na += af * af
		nb += bf * bf
	}
	if na == 0 || nb == 0 {
		return 0, errors.Join(ErrInvalidRequest, errors.New("embed: zero-magnitude vector"))
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb))), nil
}

// DotProduct returns the dot product of a and b. Returns ErrInvalidRequest
// if the vectors differ in length.
func DotProduct(a, b []float32) (float32, error) {
	if len(a) != len(b) {
		return 0, errors.Join(ErrInvalidRequest, errors.New("embed: vector length mismatch"))
	}
	var sum float64
	for i := range a {
		sum += float64(a[i]) * float64(b[i])
	}
	return float32(sum), nil
}

// Norm returns the Euclidean (L2) norm of v.
func Norm(v []float32) float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	return float32(math.Sqrt(sum))
}
