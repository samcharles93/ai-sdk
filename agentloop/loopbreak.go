package agentloop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/samcharles93/ai-sdk/core"
)

// Loop-breaker thresholds, ported from tau's coordinator. A real incident
// had a model call grep with byte-identical arguments 103 times in a row;
// past the soft threshold a call must carry a top-level
// "repeat_justification" argument to proceed, and repeated unjustified
// blocks end the run. There is deliberately no cap on justified repeats
// (re-running the same test, polling for a state change).
const (
	loopSoftThreshold  = 3
	loopHardBlockLimit = 3
)

// loopState tracks consecutive identical tool calls for one run.
type loopState struct {
	lastKey  string
	streak   int
	blockedN int
	hardStop bool
	message  string
}

// loopBrokenToolSet wraps every tool with the repetition breaker. It sits
// outermost so blocked calls never reach the gate or the real tool.
func loopBrokenToolSet(set core.ToolSet, l *loopState) core.ToolSet {
	out := make(core.ToolSet, len(set))
	for name, tool := range set {
		inner := tool.Execute
		wrapped := *tool
		toolName := name
		wrapped.Execute = func(ctx context.Context, input string) (string, error) {
			verdictMsg, blocked := l.check(toolName, input)
			if blocked {
				return verdictMsg, nil
			}
			return inner(ctx, input)
		}
		out[name] = &wrapped
	}
	return out
}

// check applies tau's checkToolLoop semantics: identical (name,
// normalised-args) calls past the soft threshold need a non-empty
// repeat_justification; loopHardBlockLimit consecutive unjustified
// blocks set hardStop, which the run's stop condition observes.
func (l *loopState) check(name, argsJSON string) (message string, blocked bool) {
	key, justification := parseToolCallKey(name, argsJSON)

	if key != l.lastKey {
		l.lastKey = key
		l.streak = 1
		l.blockedN = 0
		return "", false
	}
	l.streak++

	if l.streak <= loopSoftThreshold {
		return "", false
	}
	if justification != "" {
		l.blockedN = 0
		return "", false
	}

	l.blockedN++
	if l.blockedN >= loopHardBlockLimit {
		l.hardStop = true
		l.message = fmt.Sprintf(
			"tool %q was called %d times in a row with identical arguments and blocked %d times without ever being justified - stopping the run to avoid a runaway loop",
			name, l.streak, l.blockedN,
		)
		return l.message, true
	}
	return fmt.Sprintf(
		"This exact %s call has now been made %d times in a row with identical arguments. If repeating it is genuinely necessary (e.g. re-running the same test, polling for a state change), call it again with an added top-level argument %s explaining why - otherwise, try a different approach.",
		name, l.streak, `"repeat_justification": "<short reason>"`,
	), true
}

// parseToolCallKey unmarshals the tool call arguments once and returns
// both the normalised comparison key (name+args, excluding
// repeat_justification) and any non-empty repeat_justification string.
func parseToolCallKey(name, argsJSON string) (key string, justification string) {
	var m map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &m); err != nil {
		return name + "\x00" + argsJSON, ""
	}
	s, _ := m["repeat_justification"].(string)
	justification = strings.TrimSpace(s)
	delete(m, "repeat_justification")
	normalized, err := json.Marshal(m)
	if err != nil {
		return name + "\x00" + argsJSON, justification
	}
	return name + "\x00" + string(normalized), justification
}
