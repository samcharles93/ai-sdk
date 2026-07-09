package sse

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/samcharles93/ai-sdk/core"
	"github.com/samcharles93/ai-sdk/uimessage"
)

// FromTextStream adapts a [core.StreamResult] into a channel of
// [uimessage.Chunk] events suitable for [Pipe].
//
// FromTextStream emits a leading "start" chunk with messageID, opens
// and closes text/reasoning blocks around delta runs, translates tool
// calls and results into the corresponding tool chunks, and concludes
// with "finish" (or "error") and closes the channel.
//
// The returned channel is closed when stream.FullStream is closed or
// ctx is cancelled. Any error is delivered as an [uimessage.ErrorChunk]
// before the channel closes.
func FromTextStream(ctx context.Context, stream *core.StreamResult, messageID string) <-chan uimessage.Chunk {
	out := make(chan uimessage.Chunk, 8)
	go func() {
		defer close(out)
		send := func(c uimessage.Chunk) bool {
			select {
			case <-ctx.Done():
				return false
			case out <- c:
				return true
			}
		}
		if !send(uimessage.StartChunk{MessageID: messageID}) {
			return
		}

		var (
			textID        string
			textOpen      bool
			reasoningID   string
			reasoningOpen bool
			stepIdx       int
			stepOpen      bool
		)
		closeText := func() bool {
			if !textOpen {
				return true
			}
			textOpen = false
			return send(uimessage.TextEndChunk{ID: textID})
		}
		closeReasoning := func() bool {
			if !reasoningOpen {
				return true
			}
			reasoningOpen = false
			return send(uimessage.ReasoningEndChunk{ID: reasoningID})
		}
		openStep := func() bool {
			if stepOpen {
				return true
			}
			stepOpen = true
			return send(uimessage.StartStepChunk{})
		}

		for {
			select {
			case <-ctx.Done():
				_ = send(uimessage.AbortChunk{})
				return
			case part, ok := <-stream.FullStream:
				if !ok {
					_ = closeText()
					_ = closeReasoning()
					if stepOpen {
						_ = send(uimessage.FinishStepChunk{})
					}
					_ = send(uimessage.FinishChunk{})
					return
				}
				switch part.Type {
				case core.StreamPartStartStep:
					if !openStep() {
						return
					}
				case core.StreamPartFinishStep:
					if !closeText() || !closeReasoning() {
						return
					}
					if stepOpen {
						stepOpen = false
						if !send(uimessage.FinishStepChunk{}) {
							return
						}
					}
					stepIdx++
				case core.StreamPartTextDelta:
					if !openStep() {
						return
					}
					if !textOpen {
						textID = fmt.Sprintf("%s-text-%d", messageID, stepIdx)
						textOpen = true
						if !send(uimessage.TextStartChunk{ID: textID}) {
							return
						}
					}
					if !send(uimessage.TextDeltaChunk{ID: textID, Delta: part.TextDelta}) {
						return
					}
				case core.StreamPartReasoningDelta:
					if !openStep() {
						return
					}
					if !reasoningOpen {
						reasoningID = fmt.Sprintf("%s-reasoning-%d", messageID, stepIdx)
						reasoningOpen = true
						if !send(uimessage.ReasoningStartChunk{ID: reasoningID}) {
							return
						}
					}
					if !send(uimessage.ReasoningDeltaChunk{ID: reasoningID, Delta: part.ReasoningDelta}) {
						return
					}
				case core.StreamPartToolCall:
					if part.ToolCall == nil {
						continue
					}
					if !openStep() {
						return
					}
					if !closeText() {
						return
					}
					var input any
					if part.ToolCall.Input != "" {
						if err := json.Unmarshal([]byte(part.ToolCall.Input), &input); err != nil {
							input = part.ToolCall.Input
						}
					}
					if !send(uimessage.ToolInputAvailableChunk{
						ToolCallID: part.ToolCall.ToolCallID,
						ToolName:   part.ToolCall.ToolName,
						Input:      input,
					}) {
						return
					}
				case core.StreamPartToolResult:
					if part.ToolResult == nil {
						continue
					}
					if part.ToolResult.Error != "" {
						if !send(uimessage.ToolOutputErrorChunk{
							ToolCallID: part.ToolResult.ToolCallID,
							ErrorText:  part.ToolResult.Error,
						}) {
							return
						}
						continue
					}
					var output any
					if part.ToolResult.Output != "" {
						if err := json.Unmarshal([]byte(part.ToolResult.Output), &output); err != nil {
							output = part.ToolResult.Output
						}
					}
					if !send(uimessage.ToolOutputAvailableChunk{
						ToolCallID: part.ToolResult.ToolCallID,
						Output:     output,
					}) {
						return
					}
				case core.StreamPartWarning:
					// Warnings are not part of the protocol; surface as
					// a custom chunk so consumers can still see them.
					if part.Warning != nil {
						_ = send(uimessage.DataChunk{Name: "warning", Data: part.Warning})
					}
				case core.StreamPartError:
					msg := "stream error"
					if part.Error != nil {
						msg = part.Error.Error()
					} else if part.ErrorString != "" {
						msg = part.ErrorString
					}
					_ = send(uimessage.ErrorChunk{ErrorText: msg})
				case core.StreamPartAbort:
					_ = send(uimessage.AbortChunk{})
				case core.StreamPartFinish:
					_ = closeText()
					_ = closeReasoning()
					if stepOpen {
						stepOpen = false
						_ = send(uimessage.FinishStepChunk{})
					}
					_ = send(uimessage.FinishChunk{FinishReason: uimessage.FinishReason(part.FinishReason)})
					return
				}
			}
		}
	}()
	return out
}
