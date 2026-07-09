package core

// StopCondition is a predicate that determines whether generation should
// stop after processing the current step.
type StopCondition func(steps []StepResult) bool

// StepCountIs returns a StopCondition that stops after maxSteps steps.
func StepCountIs(maxSteps int) StopCondition {
	return func(steps []StepResult) bool {
		return len(steps) >= maxSteps
	}
}

// HasToolCall returns a StopCondition that stops when the named tool has
// been called in any step.
func HasToolCall(toolName string) StopCondition {
	return func(steps []StepResult) bool {
		for _, s := range steps {
			for _, tc := range s.ToolCalls {
				if tc.ToolName == toolName {
					return true
				}
			}
		}
		return false
	}
}

// AnyCondition returns a StopCondition that stops when any of the given
// conditions is met.
func AnyCondition(conditions ...StopCondition) StopCondition {
	return func(steps []StepResult) bool {
		for _, c := range conditions {
			if c(steps) {
				return true
			}
		}
		return false
	}
}
