package core

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestExecuteToolCallsConcurrencyOrder checks the scheduling contract that
// keeps conflicting calls in the order the model listed them.
func TestExecuteToolCallsConcurrencyOrder(t *testing.T) {
	type spec struct {
		mode  Concurrency
		sleep time.Duration
	}
	const slow = 30 * time.Millisecond
	for _, tt := range []struct {
		name    string
		calls   []spec
		before  [][2]int // call a ends before call b starts
		overlap [][2]int // calls a and b run at the same time
	}{
		{
			name:   "exclusive call fences the batch",
			calls:  []spec{{Concurrency{Parallel: true}, slow}, {Concurrency{}, 0}, {Concurrency{Parallel: true}, 0}},
			before: [][2]int{{0, 1}, {1, 2}},
		},
		{
			name:   "same key keeps call order",
			calls:  []spec{{Concurrency{Parallel: true, Key: "a"}, slow}, {Concurrency{Parallel: true, Key: "a"}, 0}},
			before: [][2]int{{0, 1}},
		},
		{
			name:    "different keys overlap",
			calls:   []spec{{Concurrency{Parallel: true, Key: "a"}, slow}, {Concurrency{Parallel: true, Key: "b"}, slow}},
			overlap: [][2]int{{0, 1}},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			start := make([]time.Time, len(tt.calls))
			end := make([]time.Time, len(tt.calls))
			set := ToolSet{}
			calls := make([]ToolCall, len(tt.calls))
			for i, c := range tt.calls {
				name := fmt.Sprintf("t%d", i)
				set[name] = &Tool{
					Concurrency: func(string) Concurrency { return c.mode },
					Execute: func(context.Context, string) (string, error) {
						mu.Lock()
						start[i] = time.Now()
						mu.Unlock()
						time.Sleep(c.sleep)
						mu.Lock()
						end[i] = time.Now()
						mu.Unlock()
						return "", nil
					},
				}
				calls[i] = ToolCall{ToolCallID: name, ToolName: name}
			}
			executeToolCalls(context.Background(), calls, set, nil, 4)
			for _, p := range tt.before {
				if start[p[1]].Before(end[p[0]]) {
					t.Errorf("call %d started before call %d ended", p[1], p[0])
				}
			}
			for _, p := range tt.overlap {
				if !start[p[1]].Before(end[p[0]]) {
					t.Errorf("calls %d and %d did not overlap", p[0], p[1])
				}
			}
		})
	}
}
