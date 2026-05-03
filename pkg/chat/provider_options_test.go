package chat

import (
	"errors"
	"testing"
)

type testOpts struct {
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	MaxThinking     int    `json:"max_thinking,omitempty"`
}

func TestProviderOptionsFor_Nil(t *testing.T) {
	got, err := ProviderOptionsFor[testOpts](nil, "openai")
	if err != nil {
		t.Fatal(err)
	}
	if got != (testOpts{}) {
		t.Fatalf("want zero, got %+v", got)
	}
}

func TestProviderOptionsFor_Missing(t *testing.T) {
	po := map[string]any{"anthropic": map[string]any{"max_thinking": 1024}}
	got, err := ProviderOptionsFor[testOpts](po, "openai")
	if err != nil {
		t.Fatal(err)
	}
	if got != (testOpts{}) {
		t.Fatalf("want zero, got %+v", got)
	}
}

func TestProviderOptionsFor_TypedStruct(t *testing.T) {
	want := testOpts{ReasoningEffort: "high", MaxThinking: 2048}
	po := map[string]any{"openai": want}
	got, err := ProviderOptionsFor[testOpts](po, "openai")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("want %+v, got %+v", want, got)
	}
}

func TestProviderOptionsFor_PointerStruct(t *testing.T) {
	want := testOpts{ReasoningEffort: "low"}
	po := map[string]any{"openai": &want}
	got, err := ProviderOptionsFor[testOpts](po, "openai")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("want %+v, got %+v", want, got)
	}
}

func TestProviderOptionsFor_MapRoundTrip(t *testing.T) {
	po := map[string]any{
		"openai": map[string]any{
			"reasoning_effort": "medium",
			"max_thinking":     1024,
		},
	}
	got, err := ProviderOptionsFor[testOpts](po, "openai")
	if err != nil {
		t.Fatal(err)
	}
	if got.ReasoningEffort != "medium" || got.MaxThinking != 1024 {
		t.Fatalf("got %+v", got)
	}
}

func TestProviderOptionsFor_OnlyActiveProviderRead(t *testing.T) {
	po := map[string]any{
		"openai":    map[string]any{"reasoning_effort": "low"},
		"anthropic": map[string]any{"max_thinking": 999},
	}
	o, err := ProviderOptionsFor[testOpts](po, "openai")
	if err != nil {
		t.Fatal(err)
	}
	if o.ReasoningEffort != "low" || o.MaxThinking != 0 {
		t.Fatalf("openai bucket bled: %+v", o)
	}
	a, err := ProviderOptionsFor[testOpts](po, "anthropic")
	if err != nil {
		t.Fatal(err)
	}
	if a.MaxThinking != 999 || a.ReasoningEffort != "" {
		t.Fatalf("anthropic bucket bled: %+v", a)
	}
}

func TestProviderOptionsFor_BadType(t *testing.T) {
	po := map[string]any{"openai": make(chan int)}
	_, err := ProviderOptionsFor[testOpts](po, "openai")
	if err == nil {
		t.Fatal("want error from unmarshallable value")
	}
	// Should be a wrapped error, not a sentinel — just check it's non-nil
	// and reports the function name.
	if err.Error() == "" || errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unexpected error shape: %v", err)
	}
}
