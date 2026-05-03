package uimessage

// LastAssistantMessageIsCompleteWithToolCalls reports whether the last
// message is an assistant message that has at least one tool part and
// every tool part is in a terminal state.
func LastAssistantMessageIsCompleteWithToolCalls(msgs []Message) bool {
	if len(msgs) == 0 {
		return false
	}
	last := msgs[len(msgs)-1]
	if last.Role != RoleAssistant {
		return false
	}
	hasTool := false
	for _, p := range last.Parts {
		switch v := p.(type) {
		case ToolUIPart:
			hasTool = true
			if !isTerminalToolState(v.State) {
				return false
			}
		case DynamicToolUIPart:
			hasTool = true
			if !isTerminalToolState(v.State) {
				return false
			}
		}
	}
	return hasTool
}

// LastAssistantMessageIsCompleteWithApprovalResponses reports whether
// the last assistant message has at least one tool part awaiting
// approval and every such part has an approval response.
func LastAssistantMessageIsCompleteWithApprovalResponses(msgs []Message) bool {
	if len(msgs) == 0 {
		return false
	}
	last := msgs[len(msgs)-1]
	if last.Role != RoleAssistant {
		return false
	}
	sawApproval := false
	for _, p := range last.Parts {
		var state ToolPartState
		var hasApproval bool
		switch v := p.(type) {
		case ToolUIPart:
			state = v.State
			hasApproval = v.Approval != nil
		case DynamicToolUIPart:
			state = v.State
			hasApproval = v.Approval != nil
		default:
			continue
		}
		if !hasApproval && state != ToolStateApprovalRequested {
			continue
		}
		sawApproval = true
		if state == ToolStateApprovalRequested {
			return false
		}
	}
	return sawApproval
}

func isTerminalToolState(s ToolPartState) bool {
	return s == ToolStateOutputAvailable || s == ToolStateOutputError || s == ToolStateOutputDenied
}
