package agentloop

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/core"
)

// summariseProvider records Chat requests and returns a fixed summary. It
// stands in for the model during compaction tests.
type summariseProvider struct {
	calls []chat.Request
}

func (p *summariseProvider) Name() string { return "summarise" }
func (p *summariseProvider) Chat(_ context.Context, req chat.Request) (chat.Response, error) {
	p.calls = append(p.calls, req)
	return chat.Response{Role: chat.RoleAssistant, Content: "a faithful summary"}, nil
}

func (p *summariseProvider) ChatStream(context.Context, chat.Request) (chat.Stream, error) {
	return nil, errors.New("streaming not supported")
}

func TestEstimateRequestTokensMonotonic(t *testing.T) {
	short := estimateRequestTokens([]chat.Message{{Role: chat.RoleUser, Content: "hi"}}, nil)
	long := estimateRequestTokens([]chat.Message{{Role: chat.RoleUser, Content: strings.Repeat("x", 1000)}}, nil)
	if long <= short {
		t.Fatalf("longer content must estimate more tokens: short=%d long=%d", short, long)
	}
}

func TestSplitCompactionHistoryPreservesSystem(t *testing.T) {
	system, current, history := splitCompactionHistory([]chat.Message{
		{Role: chat.RoleSystem, Content: "s"},
		{Role: chat.RoleUser, Content: "mission"},
		{Role: chat.RoleAssistant, Content: "work"},
	})
	if len(system) != 1 || system[0].Content != "s" {
		t.Fatalf("system = %+v, want the leading system message", system)
	}
	if current != nil {
		t.Fatalf("current = %+v, want nil (last message is assistant)", current)
	}
	if len(history) != 2 {
		t.Fatalf("history = %d messages, want 2", len(history))
	}
}

func TestSplitCompactionHistoryHoldsTrailingUser(t *testing.T) {
	system, current, history := splitCompactionHistory([]chat.Message{
		{Role: chat.RoleSystem, Content: "s"},
		{Role: chat.RoleUser, Content: "a"},
		{Role: chat.RoleAssistant, Content: "b"},
		{Role: chat.RoleUser, Content: "c"},
	})
	if current == nil || current.Content != "c" {
		t.Fatalf("current = %+v, want the trailing user message", current)
	}
	if len(history) != 2 || history[0].Content != "a" {
		t.Fatalf("history = %+v, want [a, b]", history)
	}
	if len(system) != 1 {
		t.Fatalf("system = %+v, want 1 message", system)
	}
}

func TestCompactionInstruction(t *testing.T) {
	if got := compactionInstruction(0); !strings.Contains(got, "dense continuation summary") {
		t.Fatalf("unbounded instruction = %q", got)
	}
	if got := compactionInstruction(500); !strings.Contains(got, "500 tokens") {
		t.Fatalf("bounded instruction = %q", got)
	}
}

func TestCompactorOverThresholdSummarizes(t *testing.T) {
	p := &summariseProvider{}
	c := &compactor{
		provider: p,
		model:    "m",
		cfg:      CompactionConfig{Enabled: true, ContextWindow: 200}.normalised(),
	}
	msgs := []chat.Message{
		{Role: chat.RoleSystem, Content: "you are an agent"},
		{Role: chat.RoleUser, Content: strings.Repeat("x", 4000)},
		{Role: chat.RoleAssistant, Content: "working"},
		{Role: chat.RoleTool, Content: "result"},
	}

	got, err := c.onStep(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.calls) != 1 {
		t.Fatalf("expected 1 summarisation call, got %d", len(p.calls))
	}
	if len(got) != 2 {
		t.Fatalf("replacement = %d messages, want 2: %+v", len(got), got)
	}
	if got[0].Role != chat.RoleSystem || got[0].Content != "you are an agent" {
		t.Errorf("system message not preserved: %+v", got[0])
	}
	if !strings.Contains(got[1].Content, "a faithful summary") {
		t.Errorf("summary not included: %q", got[1].Content)
	}
}

func TestCompactorUnderThresholdNoop(t *testing.T) {
	p := &summariseProvider{}
	c := &compactor{
		provider: p,
		model:    "m",
		cfg:      CompactionConfig{Enabled: true, ContextWindow: 1_000_000}.normalised(),
	}
	msgs := []chat.Message{{Role: chat.RoleUser, Content: "hello"}}
	got, err := c.onStep(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
	if len(p.calls) != 0 {
		t.Fatalf("expected no summarisation call, got %d", len(p.calls))
	}
}

func TestCompactorDisabledNoop(t *testing.T) {
	p := &summariseProvider{}
	c := &compactor{
		provider: p,
		model:    "m",
		cfg:      CompactionConfig{}.normalised(), // Enabled is false
	}
	msgs := []chat.Message{{Role: chat.RoleUser, Content: strings.Repeat("x", 4000)}}
	got, err := c.onStep(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected nil when disabled, got %+v", got)
	}
	if len(p.calls) != 0 {
		t.Fatalf("expected no summarisation call when disabled, got %d", len(p.calls))
	}
}

// compactRunProvider drives a Run whose first step grows the history past the
// compaction threshold, then ends the loop, while recording any summarisation
// calls it receives.
type compactRunProvider struct {
	summaries int
	mainCalls int
	args      string
}

func (p *compactRunProvider) Name() string { return "compact-run" }
func (p *compactRunProvider) Chat(_ context.Context, req chat.Request) (chat.Response, error) {
	if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "conversation summariser") {
		p.summaries++
		return chat.Response{Role: chat.RoleAssistant, Content: "the agent worked on X", FinishReason: "stop"}, nil
	}
	p.mainCalls++
	if p.mainCalls == 1 {
		return chat.Response{
			Role:         chat.RoleAssistant,
			ToolCalls:    []chat.ToolCall{{ID: "c1", Name: "count", Arguments: p.args}},
			FinishReason: "tool_calls",
		}, nil
	}
	return chat.Response{Role: chat.RoleAssistant, Content: "done", FinishReason: "stop"}, nil
}

func (p *compactRunProvider) ChatStream(context.Context, chat.Request) (chat.Stream, error) {
	return nil, errors.New("streaming not supported")
}

// TestRunCompactsHistory wires the whole path: a Run with compaction enabled
// and a small context window must compact at least once before ending.
func TestRunCompactsHistory(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("x", 1000)
	p := &compactRunProvider{args: `{"data":"` + long + `"}`}

	cfg := Config{
		WorkDir: dir,
		Mission: "do a thing",
		Budget:  Budget{MaxSteps: 20},
		Compact: CompactionConfig{Enabled: true, ContextWindow: 200},
		Extra: core.ToolSet{"count": core.NewTool("count", "", nil,
			func(context.Context, string) (string, error) { return "ok", nil })},
	}
	cfg.Provider = p
	cfg.Model = "m"

	_, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if p.summaries == 0 {
		t.Fatal("expected at least one compaction, got none")
	}
	if p.mainCalls < 2 {
		t.Fatalf("expected the loop to continue after compaction, main calls = %d", p.mainCalls)
	}
}
