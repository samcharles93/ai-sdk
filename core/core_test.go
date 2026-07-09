package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/samcharles93/ai-sdk/chat"
)

// fakeProvider is a scripted [chat.Provider] for tests. It walks a
// per-call queue and produces canned responses or chunk sequences.
type fakeProvider struct {
	name string

	// chatScript[i] is consumed on the i-th Chat call.
	chatScript []chat.Response
	chatErr    []error
	chatCalls  []chat.Request

	// streamScript[i] is consumed on the i-th ChatStream call.
	streamScript [][]chat.Chunk
	streamErr    []error
	streamCalls  []chat.Request

	chatIdx, streamIdx int
}

func (f *fakeProvider) Name() string {
	if f.name == "" {
		return "fake"
	}
	return f.name
}

func (f *fakeProvider) Chat(_ context.Context, req chat.Request) (chat.Response, error) {
	f.chatCalls = append(f.chatCalls, req)
	i := f.chatIdx
	f.chatIdx++
	if i < len(f.chatErr) && f.chatErr[i] != nil {
		return chat.Response{}, f.chatErr[i]
	}
	if i >= len(f.chatScript) {
		return chat.Response{}, fmt.Errorf("fakeProvider: unexpected Chat call %d", i)
	}
	return f.chatScript[i], nil
}

func (f *fakeProvider) ChatStream(_ context.Context, req chat.Request) (chat.Stream, error) {
	f.streamCalls = append(f.streamCalls, req)
	i := f.streamIdx
	f.streamIdx++
	if i < len(f.streamErr) && f.streamErr[i] != nil {
		return nil, f.streamErr[i]
	}
	if i >= len(f.streamScript) {
		return nil, fmt.Errorf("fakeProvider: unexpected ChatStream call %d", i)
	}
	return &fakeStream{chunks: f.streamScript[i]}, nil
}

type fakeStream struct {
	chunks []chat.Chunk
	idx    int
	closed bool
}

func (s *fakeStream) Next(_ context.Context) (chat.Chunk, error) {
	if s.idx >= len(s.chunks) {
		return chat.Chunk{}, io.EOF
	}
	c := s.chunks[s.idx]
	s.idx++
	return c, nil
}

func (s *fakeStream) Close() error {
	s.closed = true
	return nil
}

// ---------------------------------------------------------------------------
// GenerateText
// ---------------------------------------------------------------------------

func TestGenerateText_NoProvider(t *testing.T) {
	_, err := GenerateText(context.Background(), nil, GenerateOptions{})
	if !errors.Is(err, ErrNoProvider) {
		t.Fatalf("want ErrNoProvider, got %v", err)
	}
}

func TestGenerateText_SinglePromptNoTools(t *testing.T) {
	p := &fakeProvider{
		chatScript: []chat.Response{
			{Content: "hello world", FinishReason: "stop", Usage: chat.Usage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7}},
		},
	}
	got, err := GenerateText(context.Background(), p, GenerateOptions{
		Model:  "m",
		System: "be terse",
		Prompt: "say hi",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Text != "hello world" {
		t.Fatalf("text: %q", got.Text)
	}
	if got.FinishReason != FinishReasonStop {
		t.Fatalf("reason: %v", got.FinishReason)
	}
	if len(got.Steps) != 1 {
		t.Fatalf("steps: %d", len(got.Steps))
	}
	if got.TotalUsage.TotalTokens != 7 {
		t.Fatalf("usage: %+v", got.TotalUsage)
	}
	if len(p.chatCalls) != 1 {
		t.Fatalf("calls: %d", len(p.chatCalls))
	}
	msgs := p.chatCalls[0].Messages
	if len(msgs) != 2 || msgs[0].Role != chat.RoleSystem || msgs[1].Role != chat.RoleUser {
		t.Fatalf("msgs: %+v", msgs)
	}
}

func TestGenerateText_ToolLoop(t *testing.T) {
	calc := NewTool("calc", "adds", json.RawMessage(`{}`),
		func(_ context.Context, in string) (string, error) {
			var args struct {
				A int `json:"a"`
				B int `json:"b"`
			}
			if err := json.Unmarshal([]byte(in), &args); err != nil {
				return "", err
			}
			return fmt.Sprintf(`{"sum":%d}`, args.A+args.B), nil
		})

	p := &fakeProvider{
		chatScript: []chat.Response{
			{
				ToolCalls: []chat.ToolCall{
					{ID: "call_1", Name: "calc", Arguments: `{"a":2,"b":3}`},
				},
				FinishReason: "tool_calls",
				Usage:        chat.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
			},
			{
				Content:      "the sum is 5",
				FinishReason: "stop",
				Usage:        chat.Usage{PromptTokens: 20, CompletionTokens: 4, TotalTokens: 24},
			},
		},
	}
	got, err := GenerateText(context.Background(), p, GenerateOptions{
		Model:    "m",
		Prompt:   "what is 2+3?",
		Tools:    ToolSet{"calc": calc},
		MaxSteps: 5,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Text != "the sum is 5" {
		t.Fatalf("text: %q", got.Text)
	}
	if got.FinishReason != FinishReasonStop {
		t.Fatalf("reason: %v", got.FinishReason)
	}
	if len(got.Steps) != 2 {
		t.Fatalf("steps: %d", len(got.Steps))
	}
	if len(got.ToolCalls) != 1 || got.ToolCalls[0].ToolName != "calc" {
		t.Fatalf("tool calls: %+v", got.ToolCalls)
	}
	if len(got.ToolResults) != 1 || got.ToolResults[0].Output != `{"sum":5}` {
		t.Fatalf("tool results: %+v", got.ToolResults)
	}
	if got.TotalUsage.TotalTokens != 39 {
		t.Fatalf("usage: %+v", got.TotalUsage)
	}
	// Second chat call must contain the assistant tool-call message + the
	// tool-result message.
	if len(p.chatCalls) != 2 {
		t.Fatalf("calls: %d", len(p.chatCalls))
	}
	second := p.chatCalls[1].Messages
	var sawAssistant, sawTool bool
	for _, m := range second {
		if m.Role == chat.RoleAssistant && len(m.ToolCalls) > 0 {
			sawAssistant = true
		}
		if m.Role == chat.RoleTool && m.ToolCallID == "call_1" && m.Content == `{"sum":5}` {
			sawTool = true
		}
	}
	if !sawAssistant || !sawTool {
		t.Fatalf("loop messages missing: %+v", second)
	}
}

func TestGenerateText_ToolNotFound(t *testing.T) {
	p := &fakeProvider{
		chatScript: []chat.Response{
			{ToolCalls: []chat.ToolCall{{ID: "x", Name: "missing", Arguments: `{}`}}, FinishReason: "tool_calls"},
			{Content: "sorry", FinishReason: "stop"},
		},
	}
	got, err := GenerateText(context.Background(), p, GenerateOptions{Model: "m", Prompt: "hi", MaxSteps: 5})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got.ToolResults) != 1 || got.ToolResults[0].Error == "" {
		t.Fatalf("expected error result: %+v", got.ToolResults)
	}
	if !strings.Contains(got.ToolResults[0].Error, "tool not found") {
		t.Fatalf("error msg: %q", got.ToolResults[0].Error)
	}
}

func TestGenerateText_StopAtMaxSteps(t *testing.T) {
	calc := NewTool("c", "", nil, func(_ context.Context, _ string) (string, error) { return "ok", nil })
	p := &fakeProvider{
		chatScript: []chat.Response{
			{ToolCalls: []chat.ToolCall{{ID: "1", Name: "c", Arguments: `{}`}}, FinishReason: "tool_calls"},
			{ToolCalls: []chat.ToolCall{{ID: "2", Name: "c", Arguments: `{}`}}, FinishReason: "tool_calls"},
			{ToolCalls: []chat.ToolCall{{ID: "3", Name: "c", Arguments: `{}`}}, FinishReason: "tool_calls"},
		},
	}
	got, err := GenerateText(context.Background(), p, GenerateOptions{
		Model: "m", Prompt: "loop", Tools: ToolSet{"c": calc}, MaxSteps: 2,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got.Steps) != 2 {
		t.Fatalf("expected exactly 2 steps, got %d", len(got.Steps))
	}
}

func TestGenerateText_PropagatesProviderError(t *testing.T) {
	p := &fakeProvider{chatErr: []error{errors.New("boom")}}
	_, err := GenerateText(context.Background(), p, GenerateOptions{Model: "m", Prompt: "x"})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("err: %v", err)
	}
}

// ---------------------------------------------------------------------------
// StreamText
// ---------------------------------------------------------------------------

func collectFull(r StreamResult) []StreamPart {
	var out []StreamPart
	for p := range r.FullStream {
		out = append(out, p)
	}
	return out
}

func TestStreamText_NoProvider(t *testing.T) {
	_, err := StreamText(context.Background(), nil, GenerateOptions{})
	if !errors.Is(err, ErrNoProvider) {
		t.Fatalf("want ErrNoProvider, got %v", err)
	}
}

func TestStreamText_TextOnly(t *testing.T) {
	p := &fakeProvider{
		streamScript: [][]chat.Chunk{{
			{Delta: "hel"},
			{Delta: "lo"},
			{Delta: " world"},
			{FinishReason: "stop", Done: true, Usage: &chat.Usage{TotalTokens: 9}},
		}},
	}
	r, err := StreamText(context.Background(), p, GenerateOptions{Model: "m", Prompt: "hi"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	parts := collectFull(r)

	// Expect: StartStep, TextDelta x3, FinishStep, Finish.
	wantTypes := []StreamPartType{
		StreamPartStartStep,
		StreamPartTextDelta, StreamPartTextDelta, StreamPartTextDelta,
		StreamPartFinishStep,
		StreamPartFinish,
	}
	if len(parts) != len(wantTypes) {
		t.Fatalf("parts: %d, want %d (%+v)", len(parts), len(wantTypes), parts)
	}
	for i, w := range wantTypes {
		if parts[i].Type != w {
			t.Fatalf("part %d type: got %s want %s", i, parts[i].Type, w)
		}
	}

	// Full text reconstruction.
	var got strings.Builder
	for _, p := range parts {
		if p.Type == StreamPartTextDelta {
			got.WriteString(p.TextDelta)
		}
	}
	if got.String() != "hello world" {
		t.Fatalf("text: %q", got.String())
	}

	u, err := r.Usage()
	if err != nil || u.TotalTokens != 9 {
		t.Fatalf("usage: %+v err=%v", u, err)
	}
	fr, err := r.FinishReason()
	if err != nil || fr != FinishReasonStop {
		t.Fatalf("reason: %v err=%v", fr, err)
	}
}

func TestStreamText_TextStreamReceivesDeltas(t *testing.T) {
	p := &fakeProvider{
		streamScript: [][]chat.Chunk{{
			{Delta: "a"},
			{Delta: "b"},
			{FinishReason: "stop", Done: true},
		}},
	}
	r, err := StreamText(context.Background(), p, GenerateOptions{Model: "m", Prompt: "x"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	// Drain text in a separate goroutine; full must be drained too.
	textCh := make(chan string, 8)
	go func() {
		for s := range r.TextStream {
			textCh <- s
		}
		close(textCh)
	}()
	for range r.FullStream {
	}
	var got []string
	for s := range textCh {
		got = append(got, s)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("text deltas: %+v", got)
	}
}

func TestStreamText_ToolLoop(t *testing.T) {
	calc := NewTool("add", "", nil,
		func(_ context.Context, in string) (string, error) {
			return `{"sum":7}`, nil
		})

	p := &fakeProvider{
		streamScript: [][]chat.Chunk{
			// step 0: streams a tool call
			{
				{ToolCallDeltas: []chat.ToolCallDelta{{Index: 0, ID: "call_1", Name: "add"}}},
				{ToolCallDeltas: []chat.ToolCallDelta{{Index: 0, ArgsDelta: `{"a":3,`}}},
				{ToolCallDeltas: []chat.ToolCallDelta{{Index: 0, ArgsDelta: `"b":4}`}}},
				{FinishReason: "tool_calls", Done: true},
			},
			// step 1: final answer
			{
				{Delta: "the sum is 7"},
				{FinishReason: "stop", Done: true, Usage: &chat.Usage{TotalTokens: 12}},
			},
		},
	}

	r, err := StreamText(context.Background(), p, GenerateOptions{
		Model: "m", Prompt: "?", Tools: ToolSet{"add": calc}, MaxSteps: 5,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	parts := collectFull(r)

	// Expected order: StartStep, ToolCall, FinishStep (tool-calls),
	// StartStep, TextDelta, FinishStep (stop), Finish.
	// Note: ToolResult is emitted between ToolCall and FinishStep of step 0.
	var (
		sawToolCall   bool
		sawToolResult bool
		finishCount   int
		finalText     strings.Builder
	)
	for _, p := range parts {
		switch p.Type {
		case StreamPartToolCall:
			if p.ToolCall == nil || p.ToolCall.ToolName != "add" || p.ToolCall.Input != `{"a":3,"b":4}` {
				t.Fatalf("tool call: %+v", p.ToolCall)
			}
			sawToolCall = true
		case StreamPartToolResult:
			if p.ToolResult == nil || p.ToolResult.Output != `{"sum":7}` {
				t.Fatalf("tool result: %+v", p.ToolResult)
			}
			sawToolResult = true
		case StreamPartTextDelta:
			finalText.WriteString(p.TextDelta)
		case StreamPartFinish:
			finishCount++
		}
	}
	if !sawToolCall || !sawToolResult {
		t.Fatalf("missing tool events: parts=%+v", parts)
	}
	if finalText.String() != "the sum is 7" {
		t.Fatalf("final text: %q", finalText.String())
	}
	if finishCount != 1 {
		t.Fatalf("finish count: %d", finishCount)
	}

	// Provider must have been streamed twice.
	if len(p.streamCalls) != 2 {
		t.Fatalf("stream calls: %d", len(p.streamCalls))
	}
	// Second call must include the assistant tool-call message + tool-result.
	second := p.streamCalls[1].Messages
	var assistant, tool bool
	for _, m := range second {
		if m.Role == chat.RoleAssistant && len(m.ToolCalls) == 1 && m.ToolCalls[0].Arguments == `{"a":3,"b":4}` {
			assistant = true
		}
		if m.Role == chat.RoleTool && m.ToolCallID == "call_1" && m.Content == `{"sum":7}` {
			tool = true
		}
	}
	if !assistant || !tool {
		t.Fatalf("loop messages: %+v", second)
	}
}

func TestStreamText_ProviderErrorEmitsErrorPart(t *testing.T) {
	boom := errors.New("kaboom")
	p := &fakeProvider{streamErr: []error{boom}}
	r, err := StreamText(context.Background(), p, GenerateOptions{Model: "m", Prompt: "x"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	parts := collectFull(r)
	var sawErr bool
	for _, pt := range parts {
		if pt.Type == StreamPartError && errors.Is(pt.Error, boom) {
			sawErr = true
		}
	}
	if !sawErr {
		t.Fatalf("expected error part, got %+v", parts)
	}
	_, ferr := r.FinishReason()
	if !errors.Is(ferr, boom) {
		t.Fatalf("future error: %v", ferr)
	}
}

func TestStreamText_ContextCancelEmitsAbort(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &fakeProvider{streamScript: [][]chat.Chunk{{{Delta: "x", Done: true, FinishReason: "stop"}}}}
	r, err := StreamText(ctx, p, GenerateOptions{Model: "m", Prompt: "x"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// drain
	for range r.FullStream {
	}
	if _, ferr := r.FinishReason(); !errors.Is(ferr, ErrAborted) && !errors.Is(ferr, context.Canceled) {
		// The producer may have raced past the ctx check and reported a
		// provider error instead — accept either as long as non-nil.
		if ferr == nil {
			t.Fatalf("expected non-nil future error on cancel")
		}
	}
}

// DirectTransport-style consumer: range over FullStream only and
// translate to a flat event slice. This mirrors what the
// pkg/ui/chat DirectTransport will do once it adopts core.StreamText.
func TestStreamText_DirectTransportConsumerShape(t *testing.T) {
	p := &fakeProvider{
		streamScript: [][]chat.Chunk{{
			{Delta: "ok"},
			{FinishReason: "stop", Done: true, Usage: &chat.Usage{TotalTokens: 3}},
		}},
	}
	r, err := StreamText(context.Background(), p, GenerateOptions{Model: "m", Prompt: "p"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	out := make(chan string, 8)
	go func() {
		defer close(out)
		for part := range r.FullStream {
			switch part.Type {
			case StreamPartTextDelta:
				out <- "text:" + part.TextDelta
			case StreamPartFinish:
				out <- "finish:" + string(part.FinishReason)
			}
		}
	}()
	var got []string
	for s := range out {
		got = append(got, s)
	}
	want := []string{"text:ok", "finish:stop"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v, want %v", got, want)
	}
}
