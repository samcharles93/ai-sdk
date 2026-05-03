package chat

import (
	"encoding/json"
	"fmt"
)

// AssembleToolCalls reconstructs complete [ToolCall]s from an ordered
// sequence of [ToolCallDelta]s as emitted by a streaming provider.
//
// Deltas with the same Index are folded into a single ToolCall: the
// first delta carrying ID/Name sets those fields, and ArgsDelta is
// concatenated across all deltas for that Index. Indices that never
// receive an ID are assigned a synthetic id of "call_<index>" so that
// downstream tool execution can still correlate results back to calls
// when the upstream provider does not supply IDs (e.g. Ollama).
//
// The returned slice is sorted by Index ascending.
func AssembleToolCalls(deltas []ToolCallDelta) []ToolCall {
	if len(deltas) == 0 {
		return nil
	}
	type acc struct {
		id   string
		name string
		args []byte
	}
	byIdx := make(map[int]*acc)
	var order []int
	for _, d := range deltas {
		a, ok := byIdx[d.Index]
		if !ok {
			a = &acc{}
			byIdx[d.Index] = a
			order = append(order, d.Index)
		}
		if d.ID != "" {
			a.id = d.ID
		}
		if d.Name != "" {
			a.name = d.Name
		}
		if d.ArgsDelta != "" {
			a.args = append(a.args, d.ArgsDelta...)
		}
	}
	// stable sort by Index ascending — order may be insertion order, which
	// is not guaranteed to be sorted.
	sortInts(order)
	out := make([]ToolCall, 0, len(order))
	for _, idx := range order {
		a := byIdx[idx]
		id := a.id
		if id == "" {
			id = fmt.Sprintf("call_%d", idx)
		}
		out = append(out, ToolCall{ID: id, Name: a.name, Arguments: string(a.args)})
	}
	return out
}

// ValidateToolCallArguments reports whether the given Arguments string
// is parseable as a JSON object. It is a best-effort sanity check;
// providers may emit pre-validated JSON, or fragments that need
// concatenation upstream of this call.
func ValidateToolCallArguments(args string) error {
	if args == "" {
		return nil
	}
	var probe map[string]any
	if err := json.Unmarshal([]byte(args), &probe); err != nil {
		return fmt.Errorf("chat: tool call arguments are not valid JSON: %w", err)
	}
	return nil
}

// sortInts sorts a slice of ints ascending in-place. Tiny insertion sort
// — slices are at most a handful of elements (parallel tool calls).
func sortInts(s []int) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
