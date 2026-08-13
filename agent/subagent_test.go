package agent

import (
	"context"
	"errors"
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
	p := &fakeChatProvider{err: errors.New("provider down")}
	s := Subagent{Provider: p, Model: "m"}

	_, err := s.Run(context.Background(), "x")
	if err == nil {
		t.Fatal("expected the provider error to propagate")
	}
}

func TestSubagentRunNilProvider(t *testing.T) {
	s := Subagent{Model: "m"}
	_, err := s.Run(context.Background(), "x")
	if !errors.Is(err, core.ErrNoProvider) {
		t.Fatalf("err = %v, want core.ErrNoProvider", err)
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
	if out == "" {
		t.Fatal("expected an in-band rejection message")
	}
}

func TestSubagentToolRejectsEmptyPrompt(t *testing.T) {
	s := Subagent{Provider: &fakeChatProvider{}, Model: "m"}
	out, err := s.Tool().Execute(context.Background(), `{"prompt":""}`)
	if err != nil {
		t.Fatalf("expected an in-band rejection, got a Go error: %v", err)
	}
	if out == "" {
		t.Fatal("expected an in-band rejection message")
	}
}
