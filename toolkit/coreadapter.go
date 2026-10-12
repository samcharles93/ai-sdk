package toolkit

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/samcharles93/ai-sdk/core"
)

// CoreToolSet adapts every registered tool to ai-sdk's core.ToolSet,
// binding ui as the bridge for each execution. Failures, including results
// marked IsError, return a Go error so wrappers can tell a refused call from
// a successful one; the generation loop still feeds the error text back to
// the model in-band.
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
					return "", err
				}
				if res.IsError {
					return "", errors.New(res.Content)
				}
				return res.Content, nil
			},
		)
	}
	return set
}
