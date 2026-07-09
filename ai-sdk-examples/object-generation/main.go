// Command object-generation demonstrates how to use core.GenerateObject
// (or object.Provider.GenerateObject) to get structured JSON output from
// a model.
//
// This example shows the API pattern for object generation. It currently
// prints a placeholder because no provider in this SDK implements
// object.Provider. Provider authors can implement the interface to enable
// the pattern shown here.
//
//	Usage:
//	  go run ./ai-sdk-examples/object-generation/
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/samcharles93/ai-sdk/core"
	"github.com/samcharles93/ai-sdk/object"
)

func main() {
	if err := run(); err != nil {
		fmt.Printf("error: %v\n", err)
		fmt.Println()
		fmt.Println("Note: No provider currently implements object.Provider in this SDK.")
		fmt.Println("This example demonstrates the API pattern only.")
	}
}

func run() error {
	ctx := context.Background()

	// Define the schema for the object to generate. The schema follows
	// JSON Schema conventions and describes what structured output the
	// model should produce.
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {
				"type": "string",
				"description": "The person's full name"
			},
			"age": {
				"type": "integer",
				"description": "Age in years"
			},
			"hobbies": {
				"type": "array",
				"items": {"type": "string"},
				"description": "List of hobbies"
			}
		},
		"required": ["name", "age", "hobbies"]
	}`)

	req := object.Request{
		Model:  "gpt-5.4",
		Prompt: "Generate a profile for a fictional character named Alice who is a software engineer.",
	}

	_ = schema // schema would be passed via ProviderOptions or as a tool parameter.

	// GenerateObject is the high-level orchestration function that calls
	// through to an object.Provider. It validates the provider, respects
	// context cancellation, and wraps provider errors.
	//
	// When a real provider is available:
	//
	//   result, err := core.GenerateObject(ctx, provider, req)
	//   if err != nil { ... }
	//   // result is an object.ObjectResult (type alias for any)
	//   switch v := result.(type) {
	//   case object.Object:
	//       fmt.Printf("Generated: %s\n", v.Content)
	//   default:
	//       fmt.Printf("Result: %+v\n", v)
	//   }

	_ = core.GenerateObject // suppresses unused import
	_ = ctx
	_ = req

	fmt.Println("Object generation API:")
	fmt.Println("  1. Create an object.Provider implementation")
	fmt.Println("  2. Call core.GenerateObject(ctx, provider, req)")
	fmt.Println("  3. The result is an object.ObjectResult (alias for any)")
	fmt.Println()
	fmt.Println("The object package (pkg/object/) provides:")
	fmt.Println("  - Provider interface with GenerateObject method")
	fmt.Println("  - Request / Response types")
	fmt.Println("  - A thin Client facade with nil-guard")
	fmt.Println("  - Sentinel errors (ErrNoProvider, ErrInvalidRequest)")

	return nil
}
