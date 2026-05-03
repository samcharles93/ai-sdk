package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/samcharles93/ai-sdk/pkg/chat"
)

// executeToolCalls runs each tool call from a model step against the
// provided [ToolSet], returning [ToolResult]s in the same order as the
// input calls and the [chat.Message]s that should be appended to the
// conversation before the next step.
//
// Errors from individual tool executions are recorded on the
// corresponding ToolResult (Error field) and surfaced as the message
// content fed back to the model — the loop does not abort on tool
// errors. Models are expected to react to error outputs the same way
// they react to ordinary tool outputs.
//
// A missing tool yields a ToolResult with Error set to a wrapped
// [ErrToolNotFound]; the conversation continues so the model can
// recover.
func executeToolCalls(ctx context.Context, calls []ToolCall, set ToolSet) ([]ToolResult, []chat.Message) {
	if len(calls) == 0 {
		return nil, nil
	}
	results := make([]ToolResult, len(calls))
	msgs := make([]chat.Message, len(calls))
	for i, call := range calls {
		res := ToolResult{ToolCallID: call.ToolCallID, ToolName: call.ToolName}
		tool, ok := set[call.ToolName]
		switch {
		case !ok || tool == nil:
			res.Error = fmt.Errorf("%w: %q", ErrToolNotFound, call.ToolName).Error()
		case tool.Execute == nil:
			res.Error = fmt.Errorf("%w: tool %q has no Execute function", ErrToolExecutionFailed, call.ToolName).Error()
		default:
			out, err := tool.Execute(ctx, call.Input)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return results[:i], msgs[:i]
				}
				res.Error = err.Error()
			} else {
				res.Output = out
			}
		}
		results[i] = res

		// Feed the tool's output (or error string) back to the model as
		// the message content. Per-provider mapping (Gemini's role:"user"
		// quirk, Ollama's positional matching) lives inside the provider.
		content := res.Output
		if res.Error != "" {
			content = res.Error
		}
		msgs[i] = chat.Message{
			Role:       chat.RoleTool,
			Content:    content,
			Name:       call.ToolName,
			ToolCallID: call.ToolCallID,
		}
	}
	return results, msgs
}

// assistantMessageFromResponse builds the [chat.Message] to append to the
// conversation representing the assistant turn that just completed. It
// preserves content, multimodal Parts, and any tool calls so that
// subsequent provider calls see the complete history — this is essential
// for providers that require opaque replay tokens (Anthropic thinking
// signatures, OpenAI o1 reasoning) to be sent back unchanged.
func assistantMessageFromResponse(resp chat.Response) chat.Message {
	return chat.Message{
		Role:      chat.RoleAssistant,
		Content:   resp.Content,
		Parts:     resp.Parts,
		ToolCalls: resp.ToolCalls,
	}
}

// assistantMessageFromCalls builds an assistant [chat.Message] for the
// streaming path, where the assembled text and the assembled tool calls
// are computed separately rather than coming from a [chat.Response].
// reasoning, when non-empty, is preserved as a leading [chat.ReasoningPart]
// so providers like Anthropic can replay thinking blocks on subsequent
// turns.
func assistantMessageFromCalls(text string, reasoning string, calls []chat.ToolCall) chat.Message {
	m := chat.Message{
		Role:      chat.RoleAssistant,
		Content:   text,
		ToolCalls: calls,
	}
	if reasoning != "" {
		// Build canonical Parts: reasoning first, then text.
		parts := make(chat.Parts, 0, 2)
		parts = append(parts, chat.ReasoningPart{Text: reasoning})
		if text != "" {
			parts = append(parts, chat.TextPart{Text: text})
		}
		m.Parts = parts
	}
	return m
}
