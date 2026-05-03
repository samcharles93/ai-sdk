package uimessage

import (
	"errors"
	"fmt"
)

// ErrInvalidMessage is returned by Validate for structurally invalid
// messages.
var ErrInvalidMessage = errors.New("uimessage: invalid message")

// Validate performs structural checks on a slice of Messages.
func Validate(msgs []Message) error {
	for i, m := range msgs {
		if err := validateOne(m); err != nil {
			return fmt.Errorf("%w: index %d (%s): %v", ErrInvalidMessage, i, m.ID, err)
		}
	}
	return nil
}

func validateOne(m Message) error {
	if m.ID == "" {
		return errors.New("missing id")
	}
	switch m.Role {
	case RoleSystem, RoleUser, RoleAssistant:
	default:
		return fmt.Errorf("unknown role %q", m.Role)
	}
	if m.Parts == nil {
		return errors.New("parts is nil")
	}
	for j, p := range m.Parts {
		if err := validatePart(m.Role, p); err != nil {
			return fmt.Errorf("part %d (%s): %w", j, p.PartType(), err)
		}
	}
	return nil
}

func validatePart(role Role, p MessagePart) error {
	switch v := p.(type) {
	case ReasoningUIPart:
		if role != RoleAssistant {
			return errors.New("reasoning only allowed on assistant messages")
		}
	case StepStartUIPart:
		if role != RoleAssistant {
			return errors.New("step-start only allowed on assistant messages")
		}
	case ToolUIPart:
		if role != RoleAssistant {
			return errors.New("tool parts only allowed on assistant messages")
		}
		if v.ToolCallID == "" {
			return errors.New("tool part missing toolCallId")
		}
		if v.ToolName == "" {
			return errors.New("tool part missing tool name")
		}
	case DynamicToolUIPart:
		if role != RoleAssistant {
			return errors.New("dynamic tool parts only allowed on assistant messages")
		}
		if v.ToolCallID == "" {
			return errors.New("dynamic tool part missing toolCallId")
		}
		if v.ToolName == "" {
			return errors.New("dynamic tool part missing tool name")
		}
	}
	return nil
}
