package core

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/samcharles93/ai-sdk/schema"
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
	// It receives the JSON-encoded input arguments and returns the tool's
	// output verbatim, or an error.
	Execute func(ctx context.Context, input string) (output string, err error) `json:"-"`
	// Concurrency reports how a call with this input may overlap other
	// calls in the same step when parallel tool calls are enabled. Nil runs
	// every call exclusively, so a tool is parallel only by declaration.
	Concurrency func(input string) Concurrency `json:"-"`
}

// Concurrency is how one tool call may overlap others in a parallel batch.
// The zero value is exclusive: the call waits for every earlier call and
// every later call waits for it.
type Concurrency struct {
	// Parallel lets the call overlap other parallel calls.
	Parallel bool
	// Key orders parallel calls that touch the same resource, such as a
	// file path: calls sharing a key run one at a time in call order.
	Key string
}

// RunParallel is a Concurrency function for tools that are safe to run
// alongside anything.
func RunParallel(string) Concurrency { return Concurrency{Parallel: true} }

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

// NewTypedTool creates a Tool whose JSON Schema parameters are derived
// automatically from In via reflection (see schema.FromType), and whose
// Execute function decodes the model's raw JSON input into In before
// calling fn. This removes the class of bugs where a hand-written schema
// drifts from the struct a raw NewTool handler decodes into.
//
// Use NewTool instead when there is no static Go input type to derive a
// schema from (for example, tools proxied from an MCP server).
func NewTypedTool[In any](name, description string, fn func(ctx context.Context, input In) (string, error)) *Tool {
	params := schema.FromType(reflect.TypeFor[In]()).Build()

	return &Tool{
		Name:        name,
		Description: description,
		Parameters:  params,
		Execute: func(ctx context.Context, raw string) (string, error) {
			var in In
			if raw != "" {
				if err := json.Unmarshal([]byte(raw), &in); err != nil {
					return "", fmt.Errorf("%s: invalid input: %w", name, err)
				}
			}
			return fn(ctx, in)
		},
	}
}
