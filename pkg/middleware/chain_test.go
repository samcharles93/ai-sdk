package middleware

import "testing"

type mockProvider struct {
	calls []string
}

// For tests we use a simple function type as the provider so we can observe
// middleware composition. We'll model T as func(string) string in the test by
// using the generic Chain with that concrete type.

func TestChain_ComposesLeftToRight(t *testing.T) {
	// base provider: appends "base" to input
	base := func(s string) string { return s + "->base" }

	// middleware1: wraps provider to append "m1" before calling next
	m1 := func(next func(string) string) func(string) string {
		return func(s string) string {
			// m1 should run before m2 when composed left-to-right
			return next(s + "->m1")
		}
	}

	// middleware2: wraps provider to append "m2" before calling next
	m2 := func(next func(string) string) func(string) string {
		return func(s string) string {
			return next(s + "->m2")
		}
	}

	composed := Chain[func(string) string](funcs(m1, m2)...)(base)

	out := composed("start")

	// Expected order: start -> m1 -> m2 -> base
	want := "start->m1->m2->base"
	if out != want {
		t.Fatalf("unexpected composition order: got %q want %q", out, want)
	}
}

// funcs helper converts variadic middlewares into the exact type needed for
// Chain generically in this test file.
func funcs(mws ...func(func(string) string) func(string) string) []func(func(string) string) func(string) string {
	return mws
}
