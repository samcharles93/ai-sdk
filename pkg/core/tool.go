package core

import (
	"context"
	"encoding/json"
)

// Tool defines a callable tool that a language model can invoke during
// generation. It mirrors the AI SDK's tool type.
type Tool struct {
	// Name is the identifier the model uses to call this tool.
	Name string `json:"name"`
	// Description helps the model decide when to call this tool.
	Description string `json:"description,omitempty"`
	// Parameters is a JSON Schema describing the tool's input.
	Parameters json.RawMessage `json:"parameters,omitempty"`
	// Execute is called when the model requests this tool.
	// It receives the JSON-encoded input arguments and returns the
	// JSON-encoded output or an error.
	Execute func(ctx context.Context, input string) (output string, err error) `json:"-"`
}

// ToolSet is a map of tool name to Tool, used for type-safe tool
// configuration.
type ToolSet map[string]*Tool

// NewTool creates a Tool with the given name, description, JSON Schema
// parameters, and execute function.
func NewTool(name, description string, parameters json.RawMessage, execute func(ctx context.Context, input string) (string, error)) *Tool {
	return &Tool{
		Name:        name,
		Description: description,
		Parameters:  parameters,
		Execute:     execute,
	}
}
