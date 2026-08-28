package util

import "testing"

func TestRepairPartialJSON_ClosesOpenObject(t *testing.T) {
	got := RepairPartialJSON(`{"name":"Alice","age":3`)
	want := `{"name":"Alice","age":3}`
	if got != want {
		t.Errorf("RepairPartialJSON() = %q, want %q", got, want)
	}
}

func TestRepairPartialJSON_ClosesOpenString(t *testing.T) {
	got := RepairPartialJSON(`{"name":"Ali`)
	want := `{"name":"Ali"}`
	if got != want {
		t.Errorf("RepairPartialJSON() = %q, want %q", got, want)
	}
}

func TestRepairPartialJSON_ClosesNestedStructures(t *testing.T) {
	got := RepairPartialJSON(`{"items":[{"sku":"A1"},{"sku":"B2"`)
	want := `{"items":[{"sku":"A1"},{"sku":"B2"}]}`
	if got != want {
		t.Errorf("RepairPartialJSON() = %q, want %q", got, want)
	}
}

func TestRepairPartialJSON_TrimsDanglingKey(t *testing.T) {
	// "age" key opened but no colon/value yet — not safely closeable,
	// so the repair must back off to the last complete value.
	got := RepairPartialJSON(`{"name":"Alice","age`)
	want := `{"name":"Alice"}`
	if got != want {
		t.Errorf("RepairPartialJSON() = %q, want %q", got, want)
	}
}

func TestRepairPartialJSON_TrimsDanglingComma(t *testing.T) {
	got := RepairPartialJSON(`{"name":"Alice",`)
	want := `{"name":"Alice"}`
	if got != want {
		t.Errorf("RepairPartialJSON() = %q, want %q", got, want)
	}
}

func TestRepairPartialJSON_AlreadyValid(t *testing.T) {
	got := RepairPartialJSON(`{"name":"Alice"}`)
	want := `{"name":"Alice"}`
	if got != want {
		t.Errorf("RepairPartialJSON() = %q, want %q", got, want)
	}
}

func TestRepairPartialJSON_EmptyOrNoRecovery(t *testing.T) {
	if got := RepairPartialJSON(""); got != "" {
		t.Errorf("RepairPartialJSON(\"\") = %q, want \"\"", got)
	}
	// A lone opening brace closes to a valid, empty object.
	if got := RepairPartialJSON(`{`); got != "{}" {
		t.Errorf("RepairPartialJSON(%q) = %q, want %q", "{", got, "{}")
	}
	// Genuinely unrecoverable input (nothing in the trim window parses).
	if got := RepairPartialJSON(`not json at all`); got != "" {
		t.Errorf("RepairPartialJSON(%q) = %q, want \"\"", "not json at all", got)
	}
}

func TestRepairPartialJSON_DoesNotCloseInsideEscapedQuote(t *testing.T) {
	got := RepairPartialJSON(`{"note":"she said \"hi`)
	want := `{"note":"she said \"hi"}`
	if got != want {
		t.Errorf("RepairPartialJSON() = %q, want %q", got, want)
	}
}
