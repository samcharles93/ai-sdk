package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/core"
)

// fakeChatProvider is a chat.Provider that records its request and returns a
// canned response, standing in for the sub-agent's model.
type fakeChatProvider struct {
	resp chat.Response
	err  error
	req  chat.Request
}

func (p *fakeChatProvider) Name() string { return "fake" }
func (p *fakeChatProvider) Chat(_ context.Context, req chat.Request) (chat.Response, error) {
	p.req = req
	return p.resp, p.err
}

func (p *fakeChatProvider) ChatStream(context.Context, chat.Request) (chat.Stream, error) {
	return nil, errors.New("streaming not supported")
}

func TestSubagentRunReturnsFinalText(t *testing.T) {
	p := &fakeChatProvider{resp: chat.Response{Role: chat.RoleAssistant, Content: "the answer", FinishReason: "stop"}}
	s := Subagent{Provider: p, Model: "m", System: "be terse"}

	out, err := s.Run(context.Background(), "solve it")
	if err != nil {
		t.Fatal(err)
	}
	if out != "the answer" {
		t.Fatalf("out = %q, want %q", out, "the answer")
	}
}

// TestSubagentRunUsesFreshContext pins the isolation contract: the nested
// generation gets only the sub-agent's system prompt and the subtask, never
// the parent's history.
func TestSubagentRunUsesFreshContext(t *testing.T) {
	p := &fakeChatProvider{resp: chat.Response{Content: "ok", FinishReason: "stop"}}
	s := Subagent{Provider: p, Model: "m", System: "sub system"}

	if _, err := s.Run(context.Background(), "the subtask"); err != nil {
		t.Fatal(err)
	}

	msgs := p.req.Messages
	if len(msgs) != 2 {
		t.Fatalf("request messages = %d, want 2 (system + subtask): %+v", len(msgs), msgs)
	}
	if msgs[0].Role != chat.RoleSystem || msgs[0].Content != "sub system" {
		t.Errorf("messages[0] = %+v, want the sub-agent system prompt", msgs[0])
	}
	if msgs[1].Role != chat.RoleUser || msgs[1].Content != "the subtask" {
		t.Errorf("messages[1] = %+v, want the subtask prompt", msgs[1])
	}
}

func TestSubagentRunPropagatesProviderError(t *testing.T) {
	sentinel := errors.New("provider down")
	p := &fakeChatProvider{err: sentinel}
	s := Subagent{Provider: p, Model: "m"}

	_, err := s.Run(context.Background(), "x")
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the provider error", err)
	}
}

func TestSubagentRunNilProvider(t *testing.T) {
	s := Subagent{Model: "m"}
	_, err := s.Run(context.Background(), "x")
	if !errors.Is(err, core.ErrNoProvider) {
		t.Fatalf("err = %v, want core.ErrNoProvider", err)
	}
}

// A sub-agent whose loop ends on the step cap with a pending tool call has no
// conclusion; Run must error rather than return ("", nil).
func TestSubagentRunUnfinishedToolCall(t *testing.T) {
	p := &fakeChatProvider{resp: chat.Response{FinishReason: "tool_calls"}}
	s := Subagent{Provider: p, Model: "m", MaxSteps: 1}

	_, err := s.Run(context.Background(), "x")
	if err == nil {
		t.Fatal("expected an error for a tool-call-only finish")
	}
	if !strings.Contains(err.Error(), "step limit") {
		t.Fatalf("err = %v, want a step-limit error", err)
	}
}

func TestSubagentRunTruncated(t *testing.T) {
	p := &fakeChatProvider{resp: chat.Response{Content: "partial", FinishReason: "length"}}
	s := Subagent{Provider: p, Model: "m"}

	_, err := s.Run(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("err = %v, want a truncation error", err)
	}
}

// Even a natural stop with empty text is a failure to surface: the sub-agent
// produced no conclusion.
func TestSubagentRunNoConclusion(t *testing.T) {
	p := &fakeChatProvider{resp: chat.Response{Content: "", FinishReason: "stop"}}
	s := Subagent{Provider: p, Model: "m"}

	_, err := s.Run(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "no conclusion") {
		t.Fatalf("err = %v, want a no-conclusion error", err)
	}
}

func TestSubagentRunRecursionLimit(t *testing.T) {
	s := Subagent{Provider: &fakeChatProvider{}, Model: "m"}
	ctx := context.WithValue(context.Background(), subagentDepthKey{}, maxSubagentDepth)

	_, err := s.Run(ctx, "x")
	if err == nil || !strings.Contains(err.Error(), "depth") {
		t.Fatalf("err = %v, want a depth-limit error", err)
	}
}

func TestSubagentToolDelegates(t *testing.T) {
	p := &fakeChatProvider{resp: chat.Response{Content: "delegated result", FinishReason: "stop"}}
	s := Subagent{Provider: p, Model: "m"}

	out, err := s.Tool().Execute(context.Background(), `{"prompt":"subtask"}`)
	if err != nil {
		t.Fatalf("tool returned a Go error: %v", err)
	}
	if out != "delegated result" {
		t.Fatalf("out = %q, want %q", out, "delegated result")
	}
}

func TestSubagentToolRejectsInvalidJSON(t *testing.T) {
	s := Subagent{Provider: &fakeChatProvider{}, Model: "m"}
	out, err := s.Tool().Execute(context.Background(), `not json`)
	if err != nil {
		t.Fatalf("expected an in-band rejection, got a Go error: %v", err)
	}
	if !strings.HasPrefix(out, "subagent rejected:") {
		t.Fatalf("out = %q, want the 'subagent rejected:' prefix", out)
	}
}

// A whitespace-only prompt must be rejected, not launched as a real nested
// generation.
func TestSubagentToolRejectsWhitespacePrompt(t *testing.T) {
	p := &fakeChatProvider{resp: chat.Response{Content: "x", FinishReason: "stop"}}
	s := Subagent{Provider: p, Model: "m"}

	out, err := s.Tool().Execute(context.Background(), `{"prompt":"   "}`)
	if err != nil {
		t.Fatalf("expected an in-band rejection, got a Go error: %v", err)
	}
	if !strings.HasPrefix(out, "subagent rejected:") {
		t.Fatalf("out = %q, want the 'subagent rejected:' prefix", out)
	}
	if p.req.Model != "" {
		t.Fatalf("a whitespace prompt must not launch a nested generation")
	}
}

func TestSubagentToolCustomName(t *testing.T) {
	s := Subagent{Provider: &fakeChatProvider{}, Model: "m", Name: "research"}
	if got := s.Tool().Name; got != "research" {
		t.Fatalf("tool name = %q, want %q", got, "research")
	}
}
