package catalog

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
	c := New(Options{})
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
	c := New(Options{})
	if err := c.LoadFromJSON([]byte(`{
		"openai": {"id": "openai", "npm": "@ai-sdk/openai"}
	}`)); err != nil {
		t.Fatal(err)
	}

	c.MergeProviders(map[string]Provider{
		"openai": {
			API: "https://custom.example.com/v1",
			Models: map[string]Model{
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
	c := New(Options{})
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

func TestMergeCatalogModelKeepsModalities(t *testing.T) {
	var base Model
	base.ID = "m"
	base.Modalities.Input = []string{"text"}
	base.Modalities.Output = []string{"audio"}

	var override Model
	override.ID = "m"

	merged := mergeCatalogModel(base, override)
	if len(merged.Modalities.Output) != 1 || merged.Modalities.Output[0] != "audio" {
		t.Fatalf("merged output modalities = %v, want [audio]", merged.Modalities.Output)
	}

	var replacement Model
	replacement.ID = "m"
	replacement.Modalities.Output = []string{"text"}

	replaced := mergeCatalogModel(base, replacement)
	if len(replaced.Modalities.Output) != 1 || replaced.Modalities.Output[0] != "text" {
		t.Fatalf("replaced output modalities = %v, want [text]", replaced.Modalities.Output)
	}
}

// TestCatalogTemperatureTriState guards the distinction between an absent
// temperature flag (unknown, nil) and an explicit false (unsupported).
func TestCatalogTemperatureTriState(t *testing.T) {
	c := New(Options{})
	if err := c.LoadFromJSON([]byte(`{
		"openai": {
			"models": {
				"no-flag": {"id": "no-flag"},
				"unsupported": {"id": "unsupported", "temperature": false},
				"supported": {"id": "supported", "temperature": true}
			}
		}
	}`)); err != nil {
		t.Fatal(err)
	}

	if m, ok := c.Model("openai", "no-flag"); !ok || m.Temperature != nil {
		t.Errorf("no-flag temperature = %v, want nil", m.Temperature)
	}
	if m, ok := c.Model("openai", "unsupported"); !ok || m.Temperature == nil || *m.Temperature {
		t.Errorf("unsupported temperature = %v, want pointer to false", m.Temperature)
	}
	if m, ok := c.Model("openai", "supported"); !ok || m.Temperature == nil || !*m.Temperature {
		t.Errorf("supported temperature = %v, want pointer to true", m.Temperature)
	}

	// An override may explicitly flip a catalog true to false.
	supported := true
	unsupported := false
	base := Model{ID: "m", Temperature: &supported}
	if merged := mergeCatalogModel(base, Model{ID: "m", Temperature: &unsupported}); merged.Temperature == nil || *merged.Temperature {
		t.Errorf("merged temperature = %v, want pointer to false", merged.Temperature)
	}
	// An absent override leaves the catalog value intact.
	if merged := mergeCatalogModel(base, Model{ID: "m"}); merged.Temperature == nil || !*merged.Temperature {
		t.Errorf("merged temperature = %v, want pointer to true", merged.Temperature)
	}
}
