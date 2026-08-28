package core

import (
	"context"
	"encoding/json"
	"testing"
)

type weatherInput struct {
	City  string `json:"city"`
	Units string `json:"units,omitempty" jsonschema:"enum=celsius|fahrenheit"`
}

func TestNewTypedTool_DerivesSchema(t *testing.T) {
	tool := NewTypedTool("get_weather", "look up the weather", func(_ context.Context, in weatherInput) (string, error) {
		return "sunny in " + in.City, nil
	})

	var schema map[string]any
	if err := json.Unmarshal(tool.Parameters, &schema); err != nil {
		t.Fatalf("unmarshal derived schema: %v", err)
	}
	props := schema["properties"].(map[string]any)
	if _, ok := props["city"]; !ok {
		t.Fatal("derived schema missing 'city' property")
	}
	required, _ := schema["required"].([]any)
	if len(required) != 1 || required[0] != "city" {
		t.Errorf("required = %v, want [city] (units has omitempty)", required)
	}
}

func TestNewTypedTool_DecodesInputBeforeCallingFn(t *testing.T) {
	var gotCity string
	tool := NewTypedTool("get_weather", "", func(_ context.Context, in weatherInput) (string, error) {
		gotCity = in.City
		return "ok", nil
	})

	out, err := tool.Execute(context.Background(), `{"city":"London","units":"celsius"}`)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if out != "ok" {
		t.Errorf("out = %q, want ok", out)
	}
	if gotCity != "London" {
		t.Errorf("gotCity = %q, want London", gotCity)
	}
}

func TestNewTypedTool_InvalidJSONReturnsError(t *testing.T) {
	tool := NewTypedTool("get_weather", "", func(_ context.Context, in weatherInput) (string, error) {
		return "unreachable", nil
	})

	_, err := tool.Execute(context.Background(), `{not json`)
	if err == nil {
		t.Fatal("expected an error for malformed JSON input")
	}
}
