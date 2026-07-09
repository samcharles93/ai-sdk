package uimessage

import (
	"errors"
	"fmt"
	"maps"
)

// StreamProcessor is a stateful reducer that ingests Chunks and mutates
// a single assistant Message in place. It is the Go port of TS
// processUIMessageStream's transform function.
//
// StreamProcessor is NOT safe for concurrent use; drive it from a single
// goroutine.
type StreamProcessor struct {
	Message *Message

	activeText      map[string]int // id -> parts index
	activeReasoning map[string]int
	partialTools    map[string]*partialToolCall

	finishReason FinishReason

	// OnToolCall, if non-nil, is invoked on tool-input-available chunks
	// for non-provider-executed tools. Errors propagate from Apply.
	OnToolCall func(call ToolInputAvailableChunk) error
	// OnData, if non-nil, is invoked on data-* chunks (transient or not).
	OnData func(d DataChunk)
	// OnError, if non-nil, is invoked on error chunks.
	OnError func(err error)
}

type partialToolCall struct {
	text     string
	toolName string
	dynamic  bool
	title    string
}

// NewStreamProcessor seeds a processor with an existing assistant
// message (e.g. for resumption) or a fresh one with the given id.
func NewStreamProcessor(last *Message, messageID string) *StreamProcessor {
	var msg *Message
	if last != nil && last.Role == RoleAssistant {
		msg = last
	} else {
		msg = &Message{ID: messageID, Role: RoleAssistant, Parts: MessageParts{}}
	}
	return &StreamProcessor{
		Message:         msg,
		activeText:      map[string]int{},
		activeReasoning: map[string]int{},
		partialTools:    map[string]*partialToolCall{},
	}
}

// FinishReason returns the last seen finish reason ("" until a finish
// chunk arrives).
func (s *StreamProcessor) FinishReason() FinishReason { return s.finishReason }

// Apply ingests one Chunk and updates state.
func (s *StreamProcessor) Apply(c Chunk) error {
	switch v := c.(type) {
	case TextStartChunk:
		s.Message.Parts = append(s.Message.Parts, TextUIPart{
			State: PartStateStreaming, ProviderMetadata: v.ProviderMetadata,
		})
		s.activeText[v.ID] = len(s.Message.Parts) - 1
	case TextDeltaChunk:
		idx, ok := s.activeText[v.ID]
		if !ok {
			return fmt.Errorf("uimessage: text-delta for unknown id %q", v.ID)
		}
		t := s.Message.Parts[idx].(TextUIPart)
		t.Text += v.Delta
		if v.ProviderMetadata != nil {
			t.ProviderMetadata = v.ProviderMetadata
		}
		s.Message.Parts[idx] = t
	case TextEndChunk:
		idx, ok := s.activeText[v.ID]
		if !ok {
			return fmt.Errorf("uimessage: text-end for unknown id %q", v.ID)
		}
		t := s.Message.Parts[idx].(TextUIPart)
		t.State = PartStateDone
		if v.ProviderMetadata != nil {
			t.ProviderMetadata = v.ProviderMetadata
		}
		s.Message.Parts[idx] = t
		delete(s.activeText, v.ID)
	case ReasoningStartChunk:
		s.Message.Parts = append(s.Message.Parts, ReasoningUIPart{
			State: PartStateStreaming, ProviderMetadata: v.ProviderMetadata,
		})
		s.activeReasoning[v.ID] = len(s.Message.Parts) - 1
	case ReasoningDeltaChunk:
		idx, ok := s.activeReasoning[v.ID]
		if !ok {
			return fmt.Errorf("uimessage: reasoning-delta for unknown id %q", v.ID)
		}
		r := s.Message.Parts[idx].(ReasoningUIPart)
		r.Text += v.Delta
		if v.ProviderMetadata != nil {
			r.ProviderMetadata = v.ProviderMetadata
		}
		s.Message.Parts[idx] = r
	case ReasoningEndChunk:
		idx, ok := s.activeReasoning[v.ID]
		if !ok {
			return fmt.Errorf("uimessage: reasoning-end for unknown id %q", v.ID)
		}
		r := s.Message.Parts[idx].(ReasoningUIPart)
		r.State = PartStateDone
		if v.ProviderMetadata != nil {
			r.ProviderMetadata = v.ProviderMetadata
		}
		s.Message.Parts[idx] = r
		delete(s.activeReasoning, v.ID)
	case CustomChunk:
		s.Message.Parts = append(s.Message.Parts, CustomContentUIPart(v))
	case FileChunk:
		s.Message.Parts = append(s.Message.Parts, FileUIPart{
			URL: v.URL, MediaType: v.MediaType, ProviderMetadata: v.ProviderMetadata,
		})
	case ReasoningFileChunk:
		s.Message.Parts = append(s.Message.Parts, ReasoningFileUIPart{
			URL: v.URL, MediaType: v.MediaType, ProviderMetadata: v.ProviderMetadata,
		})
	case SourceURLChunk:
		s.Message.Parts = append(s.Message.Parts, SourceURLUIPart(v))
	case SourceDocumentChunk:
		s.Message.Parts = append(s.Message.Parts, SourceDocumentUIPart(v))
	case ToolInputStartChunk:
		dyn := v.Dynamic != nil && *v.Dynamic
		s.partialTools[v.ToolCallID] = &partialToolCall{
			toolName: v.ToolName, dynamic: dyn, title: v.Title,
		}
		s.upsertTool(v.ToolCallID, v.ToolName, dyn, ToolStateInputStreaming, toolPatch{
			Title:                v.Title,
			ProviderExecuted:     v.ProviderExecuted,
			CallProviderMetadata: v.ProviderMetadata,
		})
	case ToolInputDeltaChunk:
		pt := s.partialTools[v.ToolCallID]
		if pt == nil {
			return fmt.Errorf("uimessage: tool-input-delta for unknown call %q", v.ToolCallID)
		}
		pt.text += v.InputTextDelta
		s.upsertTool(v.ToolCallID, pt.toolName, pt.dynamic, ToolStateInputStreaming, toolPatch{
			Title: pt.title,
			Input: pt.text,
		})
	case ToolInputAvailableChunk:
		dyn := v.Dynamic != nil && *v.Dynamic
		s.upsertTool(v.ToolCallID, v.ToolName, dyn, ToolStateInputAvailable, toolPatch{
			Title:                v.Title,
			Input:                v.Input,
			ProviderExecuted:     v.ProviderExecuted,
			CallProviderMetadata: v.ProviderMetadata,
		})
		if s.OnToolCall != nil && (v.ProviderExecuted == nil || !*v.ProviderExecuted) {
			if err := s.OnToolCall(v); err != nil {
				return err
			}
		}
	case ToolInputErrorChunk:
		dyn := s.toolIsDynamic(v.ToolCallID, v.Dynamic != nil && *v.Dynamic)
		patch := toolPatch{
			ErrorText:              v.ErrorText,
			ProviderExecuted:       v.ProviderExecuted,
			ResultProviderMetadata: v.ProviderMetadata,
			Title:                  v.Title,
		}
		if dyn {
			patch.Input = v.Input
		} else {
			patch.RawInput = v.Input
		}
		s.upsertTool(v.ToolCallID, v.ToolName, dyn, ToolStateOutputError, patch)
	case ToolApprovalRequestChunk:
		if err := s.mutateTool(v.ToolCallID, func(t *ToolUIPart, d *DynamicToolUIPart) error {
			a := &ToolApproval{ID: v.ApprovalID}
			if v.IsAutomatic != nil && *v.IsAutomatic {
				a.IsAutomatic = v.IsAutomatic
			}
			if t != nil {
				t.State = ToolStateApprovalRequested
				t.Approval = a
			}
			if d != nil {
				d.State = ToolStateApprovalRequested
				d.Approval = a
			}
			return nil
		}); err != nil {
			return err
		}
	case ToolApprovalResponseChunk:
		if err := s.mutateToolByApproval(v.ApprovalID, func(t *ToolUIPart, d *DynamicToolUIPart) error {
			approved := v.Approved
			a := &ToolApproval{ID: v.ApprovalID, Approved: &approved, Reason: v.Reason}
			if t != nil {
				if t.Approval != nil && t.Approval.IsAutomatic != nil && *t.Approval.IsAutomatic {
					a.IsAutomatic = t.Approval.IsAutomatic
				}
				t.Approval = a
				t.State = ToolStateApprovalResponded
				if v.ProviderExecuted != nil {
					t.ProviderExecuted = v.ProviderExecuted
				}
				if v.ProviderMetadata != nil {
					t.CallProviderMetadata = v.ProviderMetadata
				}
			}
			if d != nil {
				if d.Approval != nil && d.Approval.IsAutomatic != nil && *d.Approval.IsAutomatic {
					a.IsAutomatic = d.Approval.IsAutomatic
				}
				d.Approval = a
				d.State = ToolStateApprovalResponded
				if v.ProviderExecuted != nil {
					d.ProviderExecuted = v.ProviderExecuted
				}
				if v.ProviderMetadata != nil {
					d.CallProviderMetadata = v.ProviderMetadata
				}
			}
			return nil
		}); err != nil {
			return err
		}
	case ToolOutputDeniedChunk:
		if err := s.mutateTool(v.ToolCallID, func(t *ToolUIPart, d *DynamicToolUIPart) error {
			if t != nil {
				t.State = ToolStateOutputDenied
			}
			if d != nil {
				d.State = ToolStateOutputDenied
			}
			return nil
		}); err != nil {
			return err
		}
	case ToolOutputAvailableChunk:
		dyn, name, input, title := s.toolKnownFields(v.ToolCallID)
		s.upsertTool(v.ToolCallID, name, dyn, ToolStateOutputAvailable, toolPatch{
			Title:                  title,
			Input:                  input,
			Output:                 v.Output,
			ProviderExecuted:       v.ProviderExecuted,
			Preliminary:            v.Preliminary,
			ResultProviderMetadata: v.ProviderMetadata,
		})
	case ToolOutputErrorChunk:
		dyn, name, input, title := s.toolKnownFields(v.ToolCallID)
		s.upsertTool(v.ToolCallID, name, dyn, ToolStateOutputError, toolPatch{
			Title:                  title,
			Input:                  input,
			ErrorText:              v.ErrorText,
			ProviderExecuted:       v.ProviderExecuted,
			ResultProviderMetadata: v.ProviderMetadata,
		})
	case StartStepChunk:
		s.Message.Parts = append(s.Message.Parts, StepStartUIPart{})
	case FinishStepChunk:
		s.activeText = map[string]int{}
		s.activeReasoning = map[string]int{}
	case StartChunk:
		if v.MessageID != "" {
			s.Message.ID = v.MessageID
		}
		if v.MessageMetadata != nil {
			s.Message.Metadata = mergeMetadata(s.Message.Metadata, v.MessageMetadata)
		}
	case FinishChunk:
		if v.FinishReason != "" {
			s.finishReason = v.FinishReason
		}
		if v.MessageMetadata != nil {
			s.Message.Metadata = mergeMetadata(s.Message.Metadata, v.MessageMetadata)
		}
	case AbortChunk:
		// No structural mutation.
	case MessageMetadataChunk:
		s.Message.Metadata = mergeMetadata(s.Message.Metadata, v.MessageMetadata)
	case ErrorChunk:
		if s.OnError != nil {
			s.OnError(errors.New(v.ErrorText))
		}
	case DataChunk:
		s.applyData(v)
	default:
		return fmt.Errorf("uimessage: unhandled chunk type %q", c.TypeName())
	}
	return nil
}

func (s *StreamProcessor) applyData(d DataChunk) {
	transient := d.Transient != nil && *d.Transient
	if !transient {
		updated := false
		if d.ID != "" {
			for i, p := range s.Message.Parts {
				if dp, ok := p.(DataUIPart); ok && dp.Name == d.Name && dp.ID == d.ID {
					dp.Data = d.Data
					s.Message.Parts[i] = dp
					updated = true
					break
				}
			}
		}
		if !updated {
			s.Message.Parts = append(s.Message.Parts, DataUIPart{Name: d.Name, ID: d.ID, Data: d.Data})
		}
	}
	if s.OnData != nil {
		s.OnData(d)
	}
}

// --- tool helpers --------------------------------------------------------

type toolPatch struct {
	Title                  string
	Input                  any
	Output                 any
	RawInput               any
	ErrorText              string
	Preliminary            *bool
	ProviderExecuted       *bool
	CallProviderMetadata   ProviderMetadata
	ResultProviderMetadata ProviderMetadata
}

func (s *StreamProcessor) upsertTool(callID, toolName string, dynamic bool, state ToolPartState, patch toolPatch) {
	for i, p := range s.Message.Parts {
		if t, ok := p.(ToolUIPart); ok && t.ToolCallID == callID {
			applyToolPatch(&t, state, patch)
			s.Message.Parts[i] = t
			return
		}
		if d, ok := p.(DynamicToolUIPart); ok && d.ToolCallID == callID {
			applyDynamicToolPatch(&d, state, patch)
			s.Message.Parts[i] = d
			return
		}
	}
	if dynamic {
		d := DynamicToolUIPart{ToolName: toolName, ToolCallID: callID, State: state}
		applyDynamicToolPatch(&d, state, patch)
		s.Message.Parts = append(s.Message.Parts, d)
	} else {
		t := ToolUIPart{ToolName: toolName, ToolCallID: callID, State: state}
		applyToolPatch(&t, state, patch)
		s.Message.Parts = append(s.Message.Parts, t)
	}
}

func applyToolPatch(t *ToolUIPart, state ToolPartState, p toolPatch) {
	t.State = state
	if p.Title != "" {
		t.Title = p.Title
	}
	if p.Input != nil {
		t.Input = p.Input
	}
	if p.Output != nil {
		t.Output = p.Output
	}
	if p.RawInput != nil {
		t.RawInput = p.RawInput
	}
	if p.ErrorText != "" {
		t.ErrorText = p.ErrorText
	}
	if p.Preliminary != nil {
		t.Preliminary = p.Preliminary
	}
	if p.ProviderExecuted != nil {
		t.ProviderExecuted = p.ProviderExecuted
	}
	if p.CallProviderMetadata != nil {
		t.CallProviderMetadata = p.CallProviderMetadata
	}
	if p.ResultProviderMetadata != nil {
		t.ResultProviderMetadata = p.ResultProviderMetadata
	}
}

func applyDynamicToolPatch(d *DynamicToolUIPart, state ToolPartState, p toolPatch) {
	d.State = state
	if p.Title != "" {
		d.Title = p.Title
	}
	if p.Input != nil {
		d.Input = p.Input
	}
	if p.Output != nil {
		d.Output = p.Output
	}
	if p.ErrorText != "" {
		d.ErrorText = p.ErrorText
	}
	if p.Preliminary != nil {
		d.Preliminary = p.Preliminary
	}
	if p.ProviderExecuted != nil {
		d.ProviderExecuted = p.ProviderExecuted
	}
	if p.CallProviderMetadata != nil {
		d.CallProviderMetadata = p.CallProviderMetadata
	}
	if p.ResultProviderMetadata != nil {
		d.ResultProviderMetadata = p.ResultProviderMetadata
	}
}

func (s *StreamProcessor) mutateTool(callID string, fn func(*ToolUIPart, *DynamicToolUIPart) error) error {
	for i, p := range s.Message.Parts {
		if t, ok := p.(ToolUIPart); ok && t.ToolCallID == callID {
			if err := fn(&t, nil); err != nil {
				return err
			}
			s.Message.Parts[i] = t
			return nil
		}
		if d, ok := p.(DynamicToolUIPart); ok && d.ToolCallID == callID {
			if err := fn(nil, &d); err != nil {
				return err
			}
			s.Message.Parts[i] = d
			return nil
		}
	}
	return fmt.Errorf("uimessage: no tool invocation for call id %q", callID)
}

func (s *StreamProcessor) mutateToolByApproval(approvalID string, fn func(*ToolUIPart, *DynamicToolUIPart) error) error {
	for i, p := range s.Message.Parts {
		if t, ok := p.(ToolUIPart); ok && t.Approval != nil && t.Approval.ID == approvalID {
			if err := fn(&t, nil); err != nil {
				return err
			}
			s.Message.Parts[i] = t
			return nil
		}
		if d, ok := p.(DynamicToolUIPart); ok && d.Approval != nil && d.Approval.ID == approvalID {
			if err := fn(nil, &d); err != nil {
				return err
			}
			s.Message.Parts[i] = d
			return nil
		}
	}
	return fmt.Errorf("uimessage: no tool invocation for approval id %q", approvalID)
}

func (s *StreamProcessor) toolIsDynamic(callID string, fallback bool) bool {
	for _, p := range s.Message.Parts {
		switch v := p.(type) {
		case ToolUIPart:
			if v.ToolCallID == callID {
				return false
			}
		case DynamicToolUIPart:
			if v.ToolCallID == callID {
				return true
			}
		}
	}
	return fallback
}

func (s *StreamProcessor) toolKnownFields(callID string) (dyn bool, name string, input any, title string) {
	for _, p := range s.Message.Parts {
		switch v := p.(type) {
		case ToolUIPart:
			if v.ToolCallID == callID {
				return false, v.ToolName, v.Input, v.Title
			}
		case DynamicToolUIPart:
			if v.ToolCallID == callID {
				return true, v.ToolName, v.Input, v.Title
			}
		}
	}
	return false, "", nil, ""
}

func mergeMetadata(a, b any) any {
	if a == nil {
		return b
	}
	am, aok := a.(map[string]any)
	bm, bok := b.(map[string]any)
	if !aok || !bok {
		return b
	}
	out := make(map[string]any, len(am)+len(bm))
	maps.Copy(out, am)
	maps.Copy(out, bm)
	return out
}
