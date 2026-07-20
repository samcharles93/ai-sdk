package toolkit

import (
	"context"
	"encoding/json"

	"github.com/samcharles93/ai-sdk/core"
)

// CoreToolSet adapts every registered tool to ai-sdk's core.ToolSet,
// binding ui as the bridge for each execution. Tool failures are encoded
// in the returned output rather than surfaced as Go errors, so the
// generation loop always feeds them back to the model; only context
// cancellation propagates as an error and aborts the run.
func (r *Registry) CoreToolSet(ui UIBridge) core.ToolSet {
	tools := r.All()
	set := make(core.ToolSet, len(tools))
	for _, tool := range tools {
		set[tool.Schema.Name] = core.NewTool(
			tool.Schema.Name,
			tool.Schema.Description,
			tool.Schema.Parameters,
			func(ctx context.Context, input string) (string, error) {
				res, err := tool.Execute(ctx, json.RawMessage(input), ui)
				if err != nil {
					if ctx.Err() != nil {
						return "", err
					}
					return "tool error: " + err.Error(), nil
				}
				return res.Content, nil
			},
		)
	}
	return set
}
