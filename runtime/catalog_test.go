package runtime

import (
	"testing"
)

func TestParseCatalogProviders(t *testing.T) {
	data := []byte(`{
		"openai": {
			"id": "openai",
			"npm": "@ai-sdk/openai",
			"api": "https://api.openai.com/v1",
			"env": ["OPENAI_API_KEY"],
			"models": {
				"gpt-5.4": {
					"id": "openai/gpt-5.4",
					"name": "GPT-5.4",
					"tool_call": true,
					"limit": {"context": 1000000, "output": 128000},
					"cost": {"input": 5, "output": 15}
				}
			}
		},
		"ignored-vendor": {"id": "ignored-vendor"}
	}`)

	providers, err := parseCatalogProviders(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 {
		t.Fatalf("providers = %d, want 1", len(providers))
	}

	p, ok := providers["openai"]
	if !ok {
		t.Fatal("openai provider missing")
	}
	if p.NPM != "@ai-sdk/openai" {
		t.Fatalf("npm = %q, want @ai-sdk/openai", p.NPM)
	}
	if len(p.Env) != 1 || p.Env[0] != "OPENAI_API_KEY" {
		t.Fatalf("env = %v, want [OPENAI_API_KEY]", p.Env)
	}

	m, ok := p.Models["gpt-5.4"]
	if !ok {
		t.Fatal("gpt-5.4 model missing")
	}
	if m.ID != "openai/gpt-5.4" {
		t.Fatalf("model id = %q", m.ID)
	}
	if !m.ToolCall {
		t.Fatal("expected tool_call")
	}
	if m.Limit.Context != 1000000 {
		t.Fatalf("context = %d", m.Limit.Context)
	}
	if m.Cost.Output != 15 {
		t.Fatalf("output cost = %f", m.Cost.Output)
	}
}

func TestCatalogProviderAPIKeyEnv(t *testing.T) {
	c := NewCatalog(CatalogOptions{})
	if err := c.LoadFromJSON([]byte(`{
		"anthropic": {
			"id": "anthropic",
			"npm": "@ai-sdk/anthropic",
			"api": "https://api.anthropic.com",
			"env": ["ANTHROPIC_API_KEY", "CLAUDE_API_KEY"],
			"models": {
				"claude-3-5-sonnet": {"id": "claude-3-5-sonnet"}
			}
		}
	}`)); err != nil {
		t.Fatal(err)
	}

	env, ok := c.APIKeyEnv("anthropic")
	if !ok {
		t.Fatal("expected api key env")
	}
	if env != "ANTHROPIC_API_KEY" {
		t.Fatalf("env = %q, want ANTHROPIC_API_KEY", env)
	}
}

func TestCatalogMergeProviders(t *testing.T) {
	c := NewCatalog(CatalogOptions{})
	if err := c.LoadFromJSON([]byte(`{
		"openai": {"id": "openai", "npm": "@ai-sdk/openai"}
	}`)); err != nil {
		t.Fatal(err)
	}

	c.MergeProviders(map[string]CatalogProvider{
		"openai": {
			API: "https://custom.example.com/v1",
			Models: map[string]CatalogModel{
				"custom-model": {ID: "custom-model"},
			},
		},
		"custom-vendor": {
			ID:  "custom-vendor",
			NPM: "@ai-sdk/openai-compatible",
			API: "https://custom-vendor.example.com",
		},
	})

	p, ok := c.Provider("openai")
	if !ok {
		t.Fatal("openai missing")
	}
	if p.API != "https://custom.example.com/v1" {
		t.Fatalf("api = %q", p.API)
	}
	if len(p.Models) != 1 {
		t.Fatalf("models = %d", len(p.Models))
	}

	if _, ok := c.Provider("custom-vendor"); !ok {
		t.Fatal("custom-vendor missing")
	}
}

func TestCatalogModelsDeterministicOrder(t *testing.T) {
	c := NewCatalog(CatalogOptions{})
	if err := c.LoadFromJSON([]byte(`{
		"openai": {
			"models": {
				"z-model": {"id": "z-model"},
				"a-model": {"id": "a-model"},
				"m-model": {"id": "m-model"}
			}
		}
	}`)); err != nil {
		t.Fatal(err)
	}

	models, err := c.Models("openai")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a-model", "m-model", "z-model"}
	if len(models) != len(want) {
		t.Fatalf("models = %d", len(models))
	}
	for i, m := range models {
		if m.ID != want[i] {
			t.Fatalf("models[%d] = %q, want %q", i, m.ID, want[i])
		}
	}
}
