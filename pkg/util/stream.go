package util

import (
	"math"
	"strings"
	"time"
)

// SimulateStream creates a channel that emits tokens from text one at a
// time with the specified delay between each token. Intended for testing
// and development.
//
// This is the Go equivalent of the AI SDK's simulateReadableStream.
func SimulateStream(text string, delay time.Duration) <-chan string {
	ch := make(chan string)
	go func() {
		defer close(ch)
		tokens := strings.SplitSeq(text, "")
		for t := range tokens {
			ch <- t
			time.Sleep(delay)
		}
	}()
	return ch
}

// SmoothStream options for controlling stream smoothing behaviour.
type SmoothStreamOptions struct {
	// Delay is the base delay between chunks.
	Delay time.Duration
}

// SmoothStream wraps a string channel and applies inter-chunk delays to
// create a smoother streaming experience. Intended for providers that
// return large chunks all at once.
//
// This is the Go equivalent of the AI SDK's smoothStream.
func SmoothStream(ch <-chan string, opts SmoothStreamOptions) <-chan string {
	out := make(chan string)
	go func() {
		defer close(out)
		var prev time.Time
		for s := range ch {
			if !prev.IsZero() {
				elapsed := time.Since(prev)
				if elapsed < opts.Delay {
					time.Sleep(opts.Delay - elapsed)
				}
			}
			prev = time.Now()
			out <- s
		}
	}()
	return out
}

// CosineSimilarity64 calculates the cosine similarity between two float64
// vectors. Returns 0 if either vector has zero magnitude.
func CosineSimilarity64(a, b []float64) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
