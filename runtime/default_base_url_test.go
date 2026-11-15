package runtime

import "testing"

func TestDefaultBaseURL(t *testing.T) {
	cases := []struct {
		class   string
		baseURL string
		ok      bool
	}{
		{class: "anthropic", baseURL: "https://api.anthropic.com", ok: true},
		{class: "cohere", baseURL: "https://api.cohere.com/v1", ok: true},
		{class: "deepseek", baseURL: "https://api.deepseek.com", ok: true},
		{class: "gemini", baseURL: "https://generativelanguage.googleapis.com", ok: true},
		{class: "gemini-interactions", baseURL: "https://generativelanguage.googleapis.com", ok: true},
		{class: "groq", baseURL: "https://api.groq.com/openai/v1", ok: true},
		{class: "minimax", baseURL: "https://api.minimax.io", ok: true},
		{class: "mistral", baseURL: "https://api.mistral.ai/v1", ok: true},
		{class: "ollama", baseURL: "http://localhost:11434", ok: true},
		{class: "openai", baseURL: "https://api.openai.com", ok: true},
		{class: "openaiobject", baseURL: "https://api.openai.com", ok: true},
		{class: "perplexity", baseURL: "https://api.perplexity.ai", ok: true},
		{class: "togetherai", baseURL: "https://api.together.xyz/v1", ok: true},
		{class: "xai", baseURL: "https://api.x.ai", ok: true},
		// Classes that require an explicit endpoint have no default.
		{class: "openai-compatible"},
		{class: "azure"},
		{class: "not-a-class"},
	}
	for _, c := range cases {
		baseURL, ok := DefaultBaseURL(c.class)
		if ok != c.ok || baseURL != c.baseURL {
			t.Errorf("DefaultBaseURL(%q) = %q, %v; want %q, %v", c.class, baseURL, ok, c.baseURL, c.ok)
		}
	}
}

// TestDefaultBaseURLClassesRegistered pins every mapped class to a built-in
// registration so the table cannot drift from RegisterBuiltinClasses.
func TestDefaultBaseURLClassesRegistered(t *testing.T) {
	RegisterBuiltinClasses()
	for class := range classDefaultBaseURL {
		if _, ok := GetClass(class); !ok {
			t.Errorf("class %q has a default base URL but is not registered", class)
		}
	}
}
