package chat

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestParts_RoundTrip(t *testing.T) {
	in := Parts{
		TextPart{Text: "hello "},
		ImagePart{URL: "https://example.com/cat.png", MediaType: "image/png"},
		ImagePart{Data: []byte{0x89, 0x50, 0x4e, 0x47}, MediaType: "image/png"},
		FilePart{URL: "https://example.com/doc.pdf", MediaType: "application/pdf", Name: "doc.pdf"},
		ReasoningPart{Text: "thinking…", ProviderMetadata: map[string]any{"anthropic": map[string]any{"signature": "sig123"}}},
		TextPart{Text: "world"},
	}

	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Verify "type" tags appear for each entry.
	s := string(raw)
	for _, want := range []string{`"type":"text"`, `"type":"image"`, `"type":"file"`, `"type":"reasoning"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in %s", want, s)
		}
	}

	var out Parts
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("len: got %d want %d", len(out), len(in))
	}
	for i := range in {
		if in[i].Type() != out[i].Type() {
			t.Errorf("part %d type: got %s want %s", i, out[i].Type(), in[i].Type())
		}
	}
	// Spot check a few fields.
	if out[0].(TextPart).Text != "hello " {
		t.Errorf("text[0]: %+v", out[0])
	}
	if img, ok := out[2].(ImagePart); !ok || string(img.Data) != "\x89PNG" {
		t.Errorf("image[2] data round-trip: %+v", out[2])
	}
	if r, ok := out[4].(ReasoningPart); !ok || r.Text != "thinking…" {
		t.Errorf("reasoning round-trip: %+v", out[4])
	}
}

func TestParts_Text(t *testing.T) {
	ps := Parts{
		TextPart{Text: "look at "},
		ImagePart{URL: "https://example.com/x.png"},
		TextPart{Text: "this"},
	}
	if got, want := ps.Text(), "look at this"; got != want {
		t.Errorf("Text: got %q want %q", got, want)
	}
}

func TestParts_HasNonText(t *testing.T) {
	if (Parts{TextPart{Text: "a"}}).HasNonText() {
		t.Errorf("text-only must report false")
	}
	if !(Parts{TextPart{Text: "a"}, ImagePart{URL: "x"}}).HasNonText() {
		t.Errorf("text+image must report true")
	}
}

func TestParts_NilUnmarshal(t *testing.T) {
	var ps Parts
	if err := json.Unmarshal([]byte("null"), &ps); err != nil {
		t.Fatalf("null: %v", err)
	}
	if ps != nil {
		t.Errorf("null must yield nil slice, got %+v", ps)
	}
}

func TestParts_NilMarshal(t *testing.T) {
	var ps Parts
	raw, err := json.Marshal(ps)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(raw) != "null" {
		t.Errorf("nil Parts must marshal to null, got %s", raw)
	}
}

func TestParts_UnknownType(t *testing.T) {
	var ps Parts
	err := json.Unmarshal([]byte(`[{"type":"video","url":"x"}]`), &ps)
	if !errors.Is(err, ErrInvalidPart) {
		t.Errorf("unknown type must error, got %v", err)
	}
}

func TestParts_MissingType(t *testing.T) {
	var ps Parts
	err := json.Unmarshal([]byte(`[{"text":"x"}]`), &ps)
	if !errors.Is(err, ErrInvalidPart) {
		t.Errorf("missing discriminator must error, got %v", err)
	}
}

func TestParts_Validate(t *testing.T) {
	cases := []struct {
		name    string
		parts   Parts
		wantErr bool
	}{
		{"empty ok", Parts{}, false},
		{"text empty ok", Parts{TextPart{}}, false},
		{"image url ok", Parts{ImagePart{URL: "https://x"}}, false},
		{"image data+mt ok", Parts{ImagePart{Data: []byte{1}, MediaType: "image/png"}}, false},
		{"image neither", Parts{ImagePart{}}, true},
		{"image both", Parts{ImagePart{URL: "u", Data: []byte{1}, MediaType: "image/png"}}, true},
		{"image data no mt", Parts{ImagePart{Data: []byte{1}}}, true},
		{"file url ok", Parts{FilePart{URL: "u", MediaType: "application/pdf"}}, false},
		{"file neither", Parts{FilePart{}}, true},
		{"reasoning empty ok", Parts{ReasoningPart{}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.parts.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("want error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tc.wantErr && err != nil && !errors.Is(err, ErrInvalidPart) {
				t.Errorf("error must wrap ErrInvalidPart: %v", err)
			}
		})
	}
}

func TestMessage_GetParts_LegacyContent(t *testing.T) {
	m := Message{Role: RoleUser, Content: "hi"}
	parts := m.GetParts()
	if len(parts) != 1 {
		t.Fatalf("expected 1 promoted part, got %d", len(parts))
	}
	if tp, ok := parts[0].(TextPart); !ok || tp.Text != "hi" {
		t.Errorf("got %+v", parts[0])
	}
}

func TestMessage_GetParts_PartsWins(t *testing.T) {
	m := Message{
		Role:    RoleUser,
		Content: "ignored",
		Parts:   Parts{TextPart{Text: "real"}},
	}
	if got := m.Text(); got != "real" {
		t.Errorf("Text: got %q want %q", got, "real")
	}
	if got := m.GetParts(); len(got) != 1 || got[0].(TextPart).Text != "real" {
		t.Errorf("GetParts: got %+v", got)
	}
}

func TestMessage_GetParts_Empty(t *testing.T) {
	m := Message{Role: RoleUser}
	if got := m.GetParts(); got != nil {
		t.Errorf("empty must yield nil, got %+v", got)
	}
	if got := m.Text(); got != "" {
		t.Errorf("empty Text must be empty, got %q", got)
	}
}

func TestUnsupportedContentError(t *testing.T) {
	err := &UnsupportedContentError{Provider: "ollama", Model: "phi3", PartType: PartTypeImage}
	if !errors.Is(err, ErrUnsupportedContent) {
		t.Errorf("must errors.Is ErrUnsupportedContent")
	}
	var as *UnsupportedContentError
	if !errors.As(err, &as) {
		t.Errorf("must errors.As *UnsupportedContentError")
	}
	if as.PartType != PartTypeImage {
		t.Errorf("PartType: %s", as.PartType)
	}
}
