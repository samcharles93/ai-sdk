package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/core"
)

// fakeProvider stands in for a deterministic model: it picks the scripted
// response from the request history (the number of assistant turns), not
// from a call counter, so a resumed run that replays earlier entries still
// gets the same response for the same history. It counts live calls, so a
// replayed run can be shown not to call the provider again.
type fakeProvider struct {
	mu           sync.Mutex
	steps        []chat.Response
	summary      chat.Response
	streamChunks [][]chat.Chunk
	calls        int
	streams      int
	block        chan struct{}
	fail         error
}

func (p *fakeProvider) Name() string { return "fake" }

// scriptIndex maps a request to its script position: -1 marks the compaction
// summarisation call, -2 marks a request whose history was already
// compacted (the script's final response), otherwise the position is the
// assistant-turn count.
func (p *fakeProvider) scriptIndex(req chat.Request) int {
	if len(req.Messages) > 0 &&
		strings.Contains(req.Messages[len(req.Messages)-1].Content, "Compact the conversation history") {
		return -1
	}
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "Conversation summary before auto-compaction") {
			return -2
		}
	}
	n := 0
	for _, m := range req.Messages {
		if m.Role == chat.RoleAssistant {
			n++
		}
	}
	return n
}

func (p *fakeProvider) Chat(ctx context.Context, req chat.Request) (chat.Response, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	if p.block != nil {
		select {
		case <-p.block:
		case <-ctx.Done():
			return chat.Response{}, ctx.Err()
		}
	}
	if p.fail != nil {
		return chat.Response{}, p.fail
	}
	if len(p.streamChunks) > 0 {
		return chat.Response{}, errors.New("fakeProvider: non-stream call not expected")
	}
	n := p.scriptIndex(req)
	if n == -1 {
		if p.summary.Content != "" {
			return p.summary, nil
		}
		return chat.Response{Role: chat.RoleAssistant, Content: "no summary scripted", FinishReason: "stop"}, nil
	}
	if n == -2 {
		return p.steps[len(p.steps)-1], nil
	}
	if n >= len(p.steps) {
		return chat.Response{Role: chat.RoleAssistant, Content: "script exhausted", FinishReason: "stop"}, nil
	}
	return p.steps[n], nil
}

func (p *fakeProvider) ChatStream(ctx context.Context, req chat.Request) (chat.Stream, error) {
	p.mu.Lock()
	p.streams++
	p.mu.Unlock()
	n := p.scriptIndex(req)
	if n < 0 || n >= len(p.streamChunks) {
		return nil, errors.New("fakeProvider: stream script exhausted")
	}
	return &fakeStream{chunks: p.streamChunks[n]}, nil
}

func (p *fakeProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func (p *fakeProvider) streamCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.streams
}

type fakeStream struct {
	chunks []chat.Chunk
	i      int
}

func (s *fakeStream) Next(context.Context) (chat.Chunk, error) {
	if s.i >= len(s.chunks) {
		return chat.Chunk{}, io.EOF
	}
	c := s.chunks[s.i]
	s.i++
	return c, nil
}

func (s *fakeStream) Close() error { return nil }

// memJournal is the in-memory Journal used by the tests. It stores the JSON
// encoding of each entry and decodes on lookup, so every replay test also
// proves the record survives a real serialisation round-trip: a field that
// does not encode would otherwise replay differently from how it was made.
type memJournal struct {
	mu       sync.Mutex
	entries  map[int64]json.RawMessage
	rejectAt map[int64]error
	records  []Entry
}

func newMemJournal() *memJournal {
	return &memJournal{entries: map[int64]json.RawMessage{}, rejectAt: map[int64]error{}}
}

func (j *memJournal) Lookup(_ context.Context, pos int64) (*Entry, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	raw, ok := j.entries[pos]
	if !ok {
		return nil, nil
	}
	var e Entry
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

func (j *memJournal) Record(_ context.Context, e Entry) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err, ok := j.rejectAt[e.Position]; ok {
		return err
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	j.entries[e.Position] = raw
	j.records = append(j.records, e)
	return nil
}

// put overwrites the entry at pos, which is how a test tampers with a
// recorded entry before replaying it.
func (j *memJournal) put(pos int64, e Entry) error {
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.entries[pos] = raw
	return nil
}

// prefix returns a journal holding only the entries recorded before pos,
// which is the state a run killed at pos would resume from.
func (j *memJournal) prefix(pos int64) *memJournal {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := newMemJournal()
	for p, raw := range j.entries {
		if p < pos {
			out.entries[p] = append(json.RawMessage(nil), raw...)
		}
	}
	return out
}

func (j *memJournal) recordCount() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.entries)
}

func (j *memJournal) entry(pos int64) (Entry, bool) {
	e, err := j.Lookup(context.Background(), pos)
	if err != nil || e == nil {
		return Entry{}, false
	}
	return *e, true
}

func usage(p, c int) chat.Usage {
	return chat.Usage{PromptTokens: p, CompletionTokens: c, TotalTokens: p + c}
}

func loopToolStep(id, name, args string) chat.Response {
	return chat.Response{
		Role:         chat.RoleAssistant,
		ToolCalls:    []chat.ToolCall{{ID: id, Name: name, Arguments: args}},
		FinishReason: "tool_calls",
		Usage:        usage(10, 10),
	}
}

func loopStopStep(text string) chat.Response {
	return chat.Response{Role: chat.RoleAssistant, Content: text, FinishReason: "stop", Usage: usage(5, 5)}
}

// countingTools returns add/mul tools that count their executions.
func countingTools(counts map[string]int) core.ToolSet {
	return core.ToolSet{
		"add": core.NewTool("add", "", nil, func(context.Context, string) (string, error) {
			counts["add"]++
			return `{"sum":3}`, nil
		}),
		"mul": core.NewTool("mul", "", nil, func(context.Context, string) (string, error) {
			counts["mul"]++
			return `{"product":6}`, nil
		}),
	}
}

func loopScript() []chat.Response {
	return []chat.Response{
		loopToolStep("c1", "add", `{"a":1,"b":2}`),
		loopToolStep("c2", "mul", `{"a":2,"b":3}`),
		loopStopStep("done"),
	}
}

func loopCfg(p chat.Provider, j Journal, tools core.ToolSet) LoopConfig {
	return LoopConfig{
		Provider: p,
		Model:    "fake-1",
		System:   "You are a test agent.",
		Prompt:   "do the thing",
		Tools:    tools,
		Journal:  j,
		RunID:    "run-test",
		Budget:   Budget{MaxSteps: 20},
	}
}

func entriesOfKind(j *memJournal, kind EntryKind) []int64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []int64
	for pos, raw := range j.entries {
		var e Entry
		if json.Unmarshal(raw, &e) == nil && e.Kind == kind {
			out = append(out, pos)
		}
	}
	slices.Sort(out)
	return out
}

// TestRunLoopKillAndResumeAtEveryPosition is issue test 23: resuming after
// each position yields the same final transcript, and no tool runs twice.
func TestRunLoopKillAndResumeAtEveryPosition(t *testing.T) {
	ctx := context.Background()
	script := loopScript()

	counts1 := map[string]int{}
	j1 := newMemJournal()
	p1 := &fakeProvider{steps: script}
	res1, err := RunLoop(ctx, loopCfg(p1, j1, countingTools(counts1)))
	if err != nil {
		t.Fatalf("full run: %v", err)
	}
	if res1.Status != StatusCompleted || res1.StopReason != StopFinished {
		t.Fatalf("full run = %s/%s, want completed/finished", res1.Status, res1.StopReason)
	}
	total := int64(j1.recordCount())
	if total == 0 {
		t.Fatal("journal recorded nothing")
	}
	modelEntries := append(entriesOfKind(j1, EntryModel), entriesOfKind(j1, EntryCompact)...)
	toolEntries := entriesOfKind(j1, EntryTool)
	if len(modelEntries)+len(toolEntries) != int(total) {
		t.Fatalf("entries = %d, model+tool = %d", total, len(modelEntries)+len(toolEntries))
	}

	for k := int64(0); k <= total; k++ {
		t.Run(fmt.Sprintf("kill_at_%d", k), func(t *testing.T) {
			p2 := &fakeProvider{steps: script}
			counts2 := map[string]int{}
			j2 := j1.prefix(k)
			res2, err := RunLoop(ctx, loopCfg(p2, j2, countingTools(counts2)))
			if err != nil {
				t.Fatalf("resume: %v", err)
			}
			if res2.Status != res1.Status || res2.StopReason != res1.StopReason {
				t.Fatalf("resume = %s/%s, want %s/%s", res2.Status, res2.StopReason, res1.Status, res1.StopReason)
			}
			if !reflect.DeepEqual(res1.Messages, res2.Messages) {
				t.Fatalf("transcript diverged at kill %d:\nrun1: %+v\nrun2: %+v", k, res1.Messages, res2.Messages)
			}
			if !reflect.DeepEqual(res1.Steps, res2.Steps) {
				t.Fatalf("steps diverged at kill %d", k)
			}
			if res2.ReplayedEntries != int(k) {
				t.Fatalf("replayed = %d, want %d", res2.ReplayedEntries, k)
			}
			wantCalls, wantTools := 0, 0
			for _, p := range modelEntries {
				if p >= k {
					wantCalls++
				}
			}
			for _, p := range toolEntries {
				if p >= k {
					wantTools++
				}
			}
			if got := p2.callCount(); got != wantCalls {
				t.Fatalf("provider calls = %d, want %d", got, wantCalls)
			}
			if got := counts2["add"] + counts2["mul"]; got != wantTools {
				t.Fatalf("tool executions = %d, want %d (recorded tools must not re-run)", got, wantTools)
			}
		})
	}
}

// TestRunLoopDivergenceStopsRun is issue test 24, divergence half: a changed
// tool input or request on replay stops with the divergence error.
func TestRunLoopDivergenceStopsRun(t *testing.T) {
	ctx := context.Background()
	script := loopScript()

	full := func() (*memJournal, LoopResult) {
		j := newMemJournal()
		res, err := RunLoop(ctx, loopCfg(&fakeProvider{steps: script}, j, countingTools(map[string]int{})))
		if err != nil {
			t.Fatalf("full run: %v", err)
		}
		return j, res
	}

	t.Run("tool input changed", func(t *testing.T) {
		j1, _ := full()
		toolPos := entriesOfKind(j1, EntryTool)[0]
		j2 := j1.prefix(int64(j1.recordCount()))
		e, _ := j2.entry(toolPos)
		e.Input = `{"a":9,"b":9}`                         // a different history yields a different call...
		e.Digest = digestToolCall("add", `{"a":9,"b":9}`) // ...and a different digest
		if err := j2.put(toolPos, e); err != nil {
			t.Fatalf("tamper: %v", err)
		}

		p2 := &fakeProvider{steps: script}
		counts2 := map[string]int{}
		res2, err := RunLoop(ctx, loopCfg(p2, j2, countingTools(counts2)))
		var diverged *DivergenceError
		if !errors.As(err, &diverged) {
			t.Fatalf("err = %v, want *DivergenceError", err)
		}
		if diverged.Position != toolPos || diverged.Want != EntryTool || diverged.WantToolName != "add" {
			t.Fatalf("divergence = %+v, want tool add at position %d", diverged, toolPos)
		}
		if res2.StopReason != StopJournalDiverged {
			t.Fatalf("stop_reason = %q", res2.StopReason)
		}
		if p2.callCount() != 0 || counts2["add"]+counts2["mul"] != 0 {
			t.Fatalf("diverged run made live calls: provider=%d tools=%v", p2.callCount(), counts2)
		}
	})

	t.Run("model request diverged", func(t *testing.T) {
		j1, _ := full()
		modelPos := entriesOfKind(j1, EntryModel)[1]
		j2 := j1.prefix(int64(j1.recordCount()))
		e, _ := j2.entry(modelPos)
		e.Digest = "deadbeef"
		if err := j2.put(modelPos, e); err != nil {
			t.Fatalf("tamper: %v", err)
		}

		p2 := &fakeProvider{steps: script}
		res2, err := RunLoop(ctx, loopCfg(p2, j2, countingTools(map[string]int{})))
		var diverged *DivergenceError
		if !errors.As(err, &diverged) || diverged.Position != modelPos || diverged.Want != EntryModel {
			t.Fatalf("err = %v, want *DivergenceError for the model call at position %d", err, modelPos)
		}
		if res2.StopReason != StopJournalDiverged || p2.callCount() != 0 {
			t.Fatalf("stop_reason=%q provider calls=%d", res2.StopReason, p2.callCount())
		}
	})
}

// TestRunLoopRejectedRecordStopsRun is issue test 24, rejection half: a
// rejected record stops the loop with no further calls, and the position
// re-runs on resume with the same idempotency key.
func TestRunLoopRejectedRecordStopsRun(t *testing.T) {
	ctx := context.Background()
	script := loopScript()

	j := newMemJournal()
	j.rejectAt[1] = errors.New("lease lost")
	var keys1 []string
	tools1 := countingTools(map[string]int{})
	tools1["add"] = core.NewTool("add", "", nil, func(ctx context.Context, _ string) (string, error) {
		keys1 = append(keys1, core.IdempotencyKey(ctx))
		return `{"sum":3}`, nil
	})
	p1 := &fakeProvider{steps: script}
	res1, err := RunLoop(ctx, loopCfg(p1, j, tools1))
	var rejected *JournalRejectedError
	if !errors.As(err, &rejected) || rejected.Position != 1 {
		t.Fatalf("err = %v, want *JournalRejectedError at position 1", err)
	}
	if !errors.Is(err, ErrJournalRejected) {
		t.Fatalf("err = %v, want it to match ErrJournalRejected", err)
	}
	if rejected.Err == nil || rejected.Err.Error() != "lease lost" {
		t.Fatalf("rejected cause = %v, want the journal's own error", rejected.Err)
	}
	if res1.StopReason != StopJournalRejected {
		t.Fatalf("stop_reason = %q", res1.StopReason)
	}
	if j.recordCount() != 1 {
		t.Fatalf("records = %d, want 1 (position 0 only)", j.recordCount())
	}
	if p1.callCount() != 1 {
		t.Fatalf("provider calls = %d, want 1 (no call after the rejection)", p1.callCount())
	}
	if len(keys1) != 1 {
		t.Fatalf("tool ran %d times, want 1 (it ran before its record was rejected)", len(keys1))
	}

	// Resume: the rejected position has no entry, so the tool re-runs — with
	// the same idempotency key, so it can deduplicate — and the run completes
	// to the same transcript as an uninterrupted run.
	j.rejectAt = map[int64]error{}
	jFull := newMemJournal()
	resFull, err := RunLoop(ctx, loopCfg(&fakeProvider{steps: script}, jFull, countingTools(map[string]int{})))
	if err != nil {
		t.Fatalf("uninterrupted run: %v", err)
	}
	var keys2 []string
	tools2 := countingTools(map[string]int{})
	tools2["add"] = core.NewTool("add", "", nil, func(ctx context.Context, _ string) (string, error) {
		keys2 = append(keys2, core.IdempotencyKey(ctx))
		return `{"sum":3}`, nil
	})
	res2, err := RunLoop(ctx, loopCfg(&fakeProvider{steps: script}, j, tools2))
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if len(keys2) != 1 || keys2[0] != keys1[0] {
		t.Fatalf("idempotency key changed across replays: first=%q resume=%q", keys1, keys2)
	}
	if !reflect.DeepEqual(resFull.Messages, res2.Messages) {
		t.Fatal("resumed transcript differs from the uninterrupted run")
	}
}

// TestRunLoopOutcomeUnknownStopsAndIsNeverRetried is issue test 24, unknown
// half.
func TestRunLoopOutcomeUnknownStopsAndIsNeverRetried(t *testing.T) {
	ctx := context.Background()
	script := []chat.Response{
		loopToolStep("c1", "maybe", `{}`),
		loopStopStep("done"),
	}
	maybeTool := func(counts map[string]int) *core.Tool {
		return core.NewTool("maybe", "", nil, func(context.Context, string) (string, error) {
			counts["maybe"]++
			return "", fmt.Errorf("write may have landed: %w", core.ErrOutcomeUnknown)
		})
	}

	counts1 := map[string]int{}
	j := newMemJournal()
	tools1 := core.ToolSet{"maybe": maybeTool(counts1)}
	res1, err := RunLoop(ctx, loopCfg(&fakeProvider{steps: script}, j, tools1))
	if !errors.Is(err, core.ErrOutcomeUnknown) {
		t.Fatalf("err = %v, want core.ErrOutcomeUnknown", err)
	}
	if res1.StopReason != StopOutcomeUnknown {
		t.Fatalf("stop_reason = %q", res1.StopReason)
	}
	e, ok := j.entry(1)
	if !ok || !e.Unknown {
		t.Fatalf("entry at tool position = %+v, want Unknown=true", e)
	}
	if counts1["maybe"] != 1 {
		t.Fatalf("tool ran %d times, want 1", counts1["maybe"])
	}

	// Resume from the same journal: the unknown outcome is replayed, the
	// tool never re-runs, and the run stops the same way.
	counts2 := map[string]int{}
	p2 := &fakeProvider{steps: script}
	res2, err := RunLoop(ctx, loopCfg(p2, j, core.ToolSet{"maybe": maybeTool(counts2)}))
	if !errors.Is(err, core.ErrOutcomeUnknown) || res2.StopReason != StopOutcomeUnknown {
		t.Fatalf("resume err=%v stop=%q", err, res2.StopReason)
	}
	if counts2["maybe"] != 0 {
		t.Fatalf("unknown tool re-ran on resume: %d executions", counts2["maybe"])
	}
	if p2.callCount() != 0 {
		t.Fatalf("provider calls on resume = %d, want 0", p2.callCount())
	}
	if !reflect.DeepEqual(res1.Messages, res2.Messages) {
		t.Fatal("transcript diverged between the unknown stop and its replay")
	}
}

// TestRunLoopBudgetSpansResumptions is issue test 25: a run resumed near its
// token or step cap refuses the next model call before it is made.
func TestRunLoopBudgetSpansResumptions(t *testing.T) {
	ctx := context.Background()
	script := loopScript() // model entries carry 20 tokens each

	j1 := newMemJournal()
	if _, err := RunLoop(ctx, loopCfg(&fakeProvider{steps: script}, j1, countingTools(map[string]int{}))); err != nil {
		t.Fatalf("full run: %v", err)
	}

	t.Run("token budget", func(t *testing.T) {
		// Positions 0 (model, 20 tokens) and 2 (model, 20 tokens) recorded:
		// the resumed run replays them, then must refuse the next call.
		j2 := j1.prefix(3)
		p2 := &fakeProvider{steps: script}
		cfg := loopCfg(p2, j2, countingTools(map[string]int{}))
		cfg.Budget.MaxTokens = 40
		res2, err := RunLoop(ctx, cfg)
		if err != nil {
			t.Fatalf("resume: %v", err)
		}
		if res2.StopReason != StopBudgetExhausted {
			t.Fatalf("stop_reason = %q, want budget_exhausted", res2.StopReason)
		}
		if p2.callCount() != 0 {
			t.Fatalf("provider calls = %d, want 0: the cap must refuse the call before it is made", p2.callCount())
		}
		if res2.ReplayedEntries != 3 {
			t.Fatalf("replayed = %d, want 3", res2.ReplayedEntries)
		}
	})

	t.Run("step budget", func(t *testing.T) {
		j2 := j1.prefix(2) // one model entry recorded
		p2 := &fakeProvider{steps: script}
		cfg := loopCfg(p2, j2, countingTools(map[string]int{}))
		cfg.Budget.MaxSteps = 1
		res2, err := RunLoop(ctx, cfg)
		if err != nil {
			t.Fatalf("resume: %v", err)
		}
		if res2.StopReason != StopBudgetExhausted || p2.callCount() != 0 {
			t.Fatalf("stop=%q provider calls=%d, want budget_exhausted/0", res2.StopReason, p2.callCount())
		}
	})
}

// TestEntryJSONRoundTrip is issue item 13: records decode back to exactly
// what was encoded.
func TestEntryJSONRoundTrip(t *testing.T) {
	entries := []Entry{
		{
			Position: 7, Kind: EntryModel, Digest: "abc123",
			Message: &chat.Message{
				Role:    chat.RoleAssistant,
				Content: "hello",
				Parts: chat.Parts{
					chat.ReasoningPart{Text: "thinking", ProviderMetadata: map[string]any{"signature": "xyz"}},
					chat.TextPart{Text: "hello"},
				},
				ToolCalls:       []chat.ToolCall{{ID: "c1", Name: "add", Arguments: `{"a":1}`}},
				ProviderOptions: map[string]any{"anthropic": map[string]any{"cache_control": true}},
			},
			FinishReason: "tool_calls",
			Usage:        chat.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3, CachedTokens: 1, CacheCreationTokens: 2},
		},
		{
			Position: 8, Kind: EntryTool, Digest: "def456",
			ToolName: "add", ToolCallID: "c1", Input: `{"a":1}`,
			Output: `{"sum":1}`,
			Usage:  chat.Usage{},
		},
		{
			Position: 9, Kind: EntryTool, Digest: "789",
			ToolName: "maybe", ToolCallID: "c2", Input: `{}`,
			Error: "write may have landed: agentloop: tool outcome unknown", Unknown: true,
		},
	}
	for i, e := range entries {
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("entry %d marshal: %v", i, err)
		}
		var back Entry
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatalf("entry %d unmarshal: %v", i, err)
		}
		if !reflect.DeepEqual(e, back) {
			t.Fatalf("entry %d round-trip mismatch:\n got %+v\nwant %+v\njson %s", i, back, e, b)
		}
	}
}

// TestRunLoopStreamDrainedAndRecorded is issue item 15: a streamed call is
// drained completely, the assembled response is recorded, and replay does
// not touch the provider.
func TestRunLoopStreamDrainedAndRecorded(t *testing.T) {
	ctx := context.Background()
	chunks := [][]chat.Chunk{
		{
			{Delta: "Hel"},
			{Delta: "lo"},
			{ToolCallDeltas: []chat.ToolCallDelta{{Index: 0, ID: "c1", Name: "add", ArgsDelta: `{"a":`}}},
			{ToolCallDeltas: []chat.ToolCallDelta{{Index: 0, ArgsDelta: `1,"b":2}`}}},
			{FinishReason: "tool_calls", Usage: new(usage(10, 10)), Done: true},
		},
		{
			{Delta: "done"},
			{FinishReason: "stop", Usage: new(usage(5, 5)), Done: true},
		},
	}

	j1 := newMemJournal()
	p1 := &fakeProvider{streamChunks: chunks}
	cfg1 := loopCfg(p1, j1, countingTools(map[string]int{}))
	cfg1.Stream = true
	res1, err := RunLoop(ctx, cfg1)
	if err != nil {
		t.Fatalf("streamed run: %v", err)
	}
	if res1.Status != StatusCompleted {
		t.Fatalf("status = %s/%s", res1.Status, res1.StopReason)
	}
	if p1.callCount() != 0 || p1.streamCount() != 2 {
		t.Fatalf("calls=%d streams=%d, want 0/2", p1.callCount(), p1.streamCount())
	}
	e, ok := j1.entry(0)
	if !ok || e.Kind != EntryModel {
		t.Fatalf("entry 0 = %+v", e)
	}
	if e.Message == nil || len(e.Message.ToolCalls) != 1 || e.Message.ToolCalls[0].Arguments != `{"a":1,"b":2}` {
		t.Fatalf("recorded tool call = %+v, want assembled {\"a\":1,\"b\":2}", e.Message)
	}
	if e.Usage.TotalTokens != 20 {
		t.Fatalf("recorded usage = %+v", e.Usage)
	}

	// Resume after the first (streamed) model call: the stream is not
	// re-opened, and the transcript matches the uninterrupted run.
	j2 := j1.prefix(1)
	p2 := &fakeProvider{streamChunks: chunks}
	cfg2 := loopCfg(p2, j2, countingTools(map[string]int{}))
	cfg2.Stream = true
	res2, err := RunLoop(ctx, cfg2)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if p2.streamCount() != 1 {
		t.Fatalf("streams = %d, want 1 (only the fresh model call)", p2.streamCount())
	}
	if !reflect.DeepEqual(res1.Messages, res2.Messages) {
		t.Fatal("transcript diverged after replaying a streamed call")
	}
}

// TestRunLoopHooksKnowAboutReplay is issue items 21 and 8: hooks fire for
// real and replayed calls alike, replayed ones are flagged through the
// context, and tool calls see a position-derived idempotency key.
func TestRunLoopHooksKnowAboutReplay(t *testing.T) {
	ctx := context.Background()
	script := loopScript()

	var modelReplay []bool
	var toolReplay []bool
	var keys []string
	modelHook := core.ModelHookFuncs(nil, func(ctx context.Context, _ chat.Request, _ *chat.Response, _ error, _ time.Duration, _ *chat.Usage) {
		modelReplay = append(modelReplay, core.Replayed(ctx))
	})
	toolHook := core.ToolHookFuncs(func(ctx context.Context, _ *core.ToolCall) (*core.Skip, error) {
		toolReplay = append(toolReplay, core.Replayed(ctx))
		keys = append(keys, core.IdempotencyKey(ctx))
		return nil, nil
	}, nil)

	j1 := newMemJournal()
	cfg1 := loopCfg(&fakeProvider{steps: script}, j1, countingTools(map[string]int{}))
	cfg1.ModelHooks = []core.ModelHook{modelHook}
	cfg1.ToolHooks = []core.ToolHook{toolHook}
	if _, err := RunLoop(ctx, cfg1); err != nil {
		t.Fatalf("full run: %v", err)
	}
	if !reflect.DeepEqual(modelReplay, []bool{false, false, false}) {
		t.Fatalf("live model replay flags = %v, want all false", modelReplay)
	}
	if !reflect.DeepEqual(toolReplay, []bool{false, false}) || !reflect.DeepEqual(keys, []string{"run-test:1", "run-test:3"}) {
		t.Fatalf("live tool flags=%v keys=%v", toolReplay, keys)
	}

	modelReplay, toolReplay, keys = nil, nil, nil
	j2 := j1.prefix(2) // replay model pos0 and tool pos1
	cfg2 := loopCfg(&fakeProvider{steps: script}, j2, countingTools(map[string]int{}))
	cfg2.ModelHooks = []core.ModelHook{modelHook}
	cfg2.ToolHooks = []core.ToolHook{toolHook}
	if _, err := RunLoop(ctx, cfg2); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !reflect.DeepEqual(modelReplay, []bool{true, false, false}) {
		t.Fatalf("resumed model replay flags = %v, want [true false false]", modelReplay)
	}
	if !reflect.DeepEqual(toolReplay, []bool{true, false}) {
		t.Fatalf("resumed tool replay flags = %v, want [true false]", toolReplay)
	}
	if !reflect.DeepEqual(keys, []string{"run-test:1", "run-test:3"}) {
		t.Fatalf("resumed keys = %v, want the same position-derived keys [run-test:1 run-test:3]", keys)
	}
}

// TestRunLoopToolPanicIsRecorded is issue item 12, tool half: a panicking
// tool is contained and recorded as a failed tool call.
func TestRunLoopToolPanicIsRecorded(t *testing.T) {
	ctx := context.Background()
	script := []chat.Response{
		loopToolStep("c1", "boom", `{}`),
		loopStopStep("recovered"),
	}
	counts := map[string]int{}
	tools := core.ToolSet{
		"boom": core.NewTool("boom", "", nil, func(context.Context, string) (string, error) {
			counts["boom"]++
			panic("kaboom")
		}),
	}

	j1 := newMemJournal()
	res1, err := RunLoop(ctx, loopCfg(&fakeProvider{steps: script}, j1, tools))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res1.Status != StatusCompleted {
		t.Fatalf("status = %s/%s", res1.Status, res1.StopReason)
	}
	e, ok := j1.entry(1)
	if !ok || e.Kind != EntryTool || e.Error == "" {
		t.Fatalf("tool entry = %+v, want a recorded failure", e)
	}
	if !strings.Contains(e.Error, "kaboom") || !strings.Contains(e.Error, "panicked") {
		t.Fatalf("recorded error = %q, want the panic text", e.Error)
	}

	// Resume: the panic is replayed from the journal, the tool never re-runs.
	counts2 := map[string]int{}
	tools2 := core.ToolSet{
		"boom": core.NewTool("boom", "", nil, func(context.Context, string) (string, error) {
			counts2["boom"]++
			panic("kaboom")
		}),
	}
	j2 := j1.prefix(2) // model pos0 and the panicked tool pos1 are recorded
	res2, err := RunLoop(ctx, loopCfg(&fakeProvider{steps: script}, j2, tools2))
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if counts2["boom"] != 0 {
		t.Fatalf("panicking tool re-ran on resume: %d executions", counts2["boom"])
	}
	if !reflect.DeepEqual(res1.Messages, res2.Messages) {
		t.Fatal("transcript diverged after replaying a panicked tool call")
	}
}

// TestRunLoopModelHookPanicStopsLoop is issue item 12, hook half.
func TestRunLoopModelHookPanicStopsLoop(t *testing.T) {
	ctx := context.Background()
	hook := core.ModelHookFuncs(func(context.Context, chat.Request) { panic("hook boom") }, nil)
	cfg := loopCfg(&fakeProvider{steps: loopScript()}, newMemJournal(), countingTools(map[string]int{}))
	cfg.ModelHooks = []core.ModelHook{hook}
	p := cfg.Provider.(*fakeProvider)
	res, err := RunLoop(ctx, cfg)
	if !errors.Is(err, ErrModelHookPanic) {
		t.Fatalf("err = %v, want ErrModelHookPanic", err)
	}
	if res.StopReason != StopModelHookPanic {
		t.Fatalf("stop_reason = %q", res.StopReason)
	}
	if p.callCount() != 0 {
		t.Fatalf("provider calls = %d, want 0: the hook panicked before the call", p.callCount())
	}
}

// TestRunLoopCancellationRecordsNothing is issue item 11: a call cancelled
// mid-flight leaves no entry, and the position re-runs on resume.
func TestRunLoopCancellationRecordsNothing(t *testing.T) {
	ctx := context.Background()
	j := newMemJournal()
	block := make(chan struct{})
	p1 := &fakeProvider{steps: loopScript(), block: block}
	cfg := loopCfg(p1, j, countingTools(map[string]int{}))

	cctx, cancel := context.WithCancel(ctx)
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	res, err := RunLoop(cctx, cfg)
	if err != nil {
		t.Fatalf("cancelled run returned infrastructure error: %v", err)
	}
	if res.StopReason != StopCanceled {
		t.Fatalf("stop_reason = %q, want canceled", res.StopReason)
	}
	if j.recordCount() != 0 {
		t.Fatalf("records = %d, want 0: cancellation records nothing", j.recordCount())
	}

	// Resume from the empty journal: the position re-runs and completes.
	res2, err := RunLoop(ctx, loopCfg(&fakeProvider{steps: loopScript()}, j, countingTools(map[string]int{})))
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if res2.Status != StatusCompleted {
		t.Fatalf("resumed status = %s/%s", res2.Status, res2.StopReason)
	}
}

// TestRunLoopCompactionIsJournalled is issue item 16: the compaction summary
// is a recorded entry, and replay rebuilds the compacted history without
// calling the model again.
func TestRunLoopCompactionIsJournalled(t *testing.T) {
	ctx := context.Background()
	// The window is sized so the preserved prefix (system + prompt) sits
	// just under the compaction threshold: four tool turns push the history
	// over it, and one compaction brings it back under.
	script := []chat.Response{
		loopToolStep("c1", "add", `{"a":1,"b":2}`),
		loopToolStep("c2", "mul", `{"a":2,"b":3}`),
		loopToolStep("c3", "add", `{"a":4,"b":5}`),
		loopToolStep("c4", "mul", `{"a":6,"b":7}`),
		loopStopStep("done"),
	}
	summary := chat.Response{Role: chat.RoleAssistant, Content: "SUMMARY", FinishReason: "stop", Usage: usage(10, 10)}
	cfgFor := func(p chat.Provider, j Journal) LoopConfig {
		cfg := loopCfg(p, j, countingTools(map[string]int{}))
		cfg.Prompt = strings.Repeat("x", 2740)
		cfg.Compact = CompactionConfig{Enabled: true, ContextWindow: 1000}
		return cfg
	}

	j1 := newMemJournal()
	res1, err := RunLoop(ctx, cfgFor(&fakeProvider{steps: script, summary: summary}, j1))
	if err != nil {
		t.Fatalf("full run: %v", err)
	}
	compactPos := entriesOfKind(j1, EntryCompact)
	if len(compactPos) != 1 {
		t.Fatalf("compact entries = %v, want exactly one", compactPos)
	}
	e, _ := j1.entry(compactPos[0])
	if e.Message == nil || e.Message.Content != "SUMMARY" {
		t.Fatalf("compact entry message = %+v", e.Message)
	}

	// Resume with everything except the final model call: the compaction is
	// replayed from the journal, so only one live model call remains.
	total := int64(j1.recordCount())
	j2 := j1.prefix(total - 1)
	p2 := &fakeProvider{steps: script, summary: summary}
	res2, err := RunLoop(ctx, cfgFor(p2, j2))
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if p2.callCount() != 1 {
		t.Fatalf("provider calls = %d, want 1 (the compaction must not be re-called)", p2.callCount())
	}
	for _, r := range j2.records {
		if r.Kind == EntryCompact {
			t.Fatalf("resume recorded a fresh compaction: %+v", r)
		}
	}
	if !reflect.DeepEqual(res1.Messages, res2.Messages) {
		t.Fatal("transcript diverged after replaying a compaction")
	}

	// Resume from before the compaction: it is re-made (one live summary
	// call plus the remaining model call) and the transcript still matches.
	j3 := j1.prefix(compactPos[0])
	p3 := &fakeProvider{steps: script, summary: summary}
	res3, err := RunLoop(ctx, cfgFor(p3, j3))
	if err != nil {
		t.Fatalf("resume before compaction: %v", err)
	}
	if p3.callCount() != 2 {
		t.Fatalf("provider calls = %d, want 2 (summary + final model call)", p3.callCount())
	}
	if !reflect.DeepEqual(res1.Messages, res3.Messages) {
		t.Fatal("transcript diverged when the compaction was re-made")
	}
}

// TestRunLoopCancellationRecordsTools covers what a cancelled tool call leaves
// in the journal. A model call has no side effect, so cancelling one records
// nothing and the position re-runs; a tool does, so leaving no entry would run
// the side effect twice.
func TestRunLoopCancellationRecordsTools(t *testing.T) {
	ctx := context.Background()
	script := []chat.Response{
		loopToolStep("c1", "slow", `{}`),
		loopStopStep("done"),
	}

	t.Run("returned result is recorded and not repeated", func(t *testing.T) {
		runCtx, cancel := context.WithCancel(ctx)
		counts := map[string]int{}
		tools := core.ToolSet{
			"slow": core.NewTool("slow", "", nil, func(context.Context, string) (string, error) {
				counts["slow"]++
				cancel() // the tool finishes, then the run is cancelled before its record
				return `{"ok":true}`, nil
			}),
		}
		j := newMemJournal()
		res, err := RunLoop(runCtx, loopCfg(&fakeProvider{steps: script}, j, tools))
		if err != nil {
			t.Fatalf("cancelled run returned infrastructure error: %v", err)
		}
		if res.StopReason != StopCanceled {
			t.Fatalf("stop_reason = %q, want canceled", res.StopReason)
		}
		e, ok := j.entry(1)
		if !ok || e.Kind != EntryTool || e.Unknown {
			t.Fatalf("entry 1 = %+v, want the tool's known result", e)
		}
		if e.Output != `{"ok":true}` {
			t.Fatalf("recorded output = %q, want the tool's output", e.Output)
		}

		res2, err := RunLoop(ctx, loopCfg(&fakeProvider{steps: script}, j, tools))
		if err != nil {
			t.Fatalf("resume: %v", err)
		}
		if counts["slow"] != 1 {
			t.Fatalf("tool ran %d times, want 1: a recorded tool must not run again", counts["slow"])
		}
		if res2.Status != StatusCompleted {
			t.Fatalf("resumed status = %s/%s", res2.Status, res2.StopReason)
		}
	})

	t.Run("interrupted tool is recorded as unknown", func(t *testing.T) {
		runCtx, cancel := context.WithCancel(ctx)
		counts := map[string]int{}
		tools := core.ToolSet{
			"slow": core.NewTool("slow", "", nil, func(ctx context.Context, _ string) (string, error) {
				counts["slow"]++
				cancel()
				<-ctx.Done()
				return "", ctx.Err()
			}),
		}
		j := newMemJournal()
		res, err := RunLoop(runCtx, loopCfg(&fakeProvider{steps: script}, j, tools))
		if !errors.Is(err, core.ErrOutcomeUnknown) {
			t.Fatalf("err = %v, want core.ErrOutcomeUnknown", err)
		}
		if res.StopReason != StopOutcomeUnknown {
			t.Fatalf("stop_reason = %q, want %q", res.StopReason, StopOutcomeUnknown)
		}
		e, ok := j.entry(1)
		if !ok || !e.Unknown {
			t.Fatalf("entry 1 = %+v, want an unknown outcome", e)
		}

		res2, err := RunLoop(ctx, loopCfg(&fakeProvider{steps: script}, j, tools))
		if !errors.Is(err, core.ErrOutcomeUnknown) || res2.StopReason != StopOutcomeUnknown {
			t.Fatalf("resume = %v/%s, want an unknown outcome", err, res2.StopReason)
		}
		if counts["slow"] != 1 {
			t.Fatalf("tool ran %d times, want 1: an unknown outcome is never retried", counts["slow"])
		}
	})
}

// TestRunLoopJournalContracts pins the checkpoint rules that a kill-and-resume
// run does not exercise: what is deliberately left unrecorded, what a request
// digest must not depend on, and what a journal requires of its caller.
func TestRunLoopJournalContracts(t *testing.T) {
	ctx := context.Background()

	t.Run("a failed model call records nothing and re-runs on resume", func(t *testing.T) {
		j := newMemJournal()
		outage := errors.New("provider unavailable")
		res, err := RunLoop(ctx, loopCfg(&fakeProvider{steps: loopScript(), fail: outage}, j, countingTools(map[string]int{})))
		if !errors.Is(err, outage) {
			t.Fatalf("err = %v, want the provider failure", err)
		}
		if res.StopReason != StopProviderError {
			t.Fatalf("stop_reason = %q, want %q", res.StopReason, StopProviderError)
		}
		if j.recordCount() != 0 {
			t.Fatalf("records = %d, want 0: a failed model call has no side effect, so an outage must not poison the run", j.recordCount())
		}

		res2, err := RunLoop(ctx, loopCfg(&fakeProvider{steps: loopScript()}, j, countingTools(map[string]int{})))
		if err != nil {
			t.Fatalf("resume after the outage: %v", err)
		}
		if res2.Status != StatusCompleted {
			t.Fatalf("resumed status = %s/%s, want completed", res2.Status, res2.StopReason)
		}
	})

	t.Run("model info is not part of the request digest", func(t *testing.T) {
		j := newMemJournal()
		if _, err := RunLoop(ctx, loopCfg(&fakeProvider{steps: loopScript()}, j, countingTools(map[string]int{}))); err != nil {
			t.Fatalf("full run: %v", err)
		}
		cfg := loopCfg(&fakeProvider{steps: loopScript()}, j.prefix(int64(j.recordCount())), countingTools(map[string]int{}))
		// Security patch or catalog refresh between attempts: resolved
		// capabilities change, the requests do not.
		cfg.ModelInfo = chat.ModelInfo{Reasoning: true}
		res, err := RunLoop(ctx, cfg)
		if err != nil {
			t.Fatalf("resume with changed model info: %v", err)
		}
		if res.Status != StatusCompleted {
			t.Fatalf("resumed status = %s/%s, want completed", res.Status, res.StopReason)
		}
	})

	t.Run("an unencodable request is an error, not a panic", func(t *testing.T) {
		j := newMemJournal()
		cfg := loopCfg(&fakeProvider{steps: loopScript()}, j, countingTools(map[string]int{}))
		cfg.ProviderOptions = map[string]any{"callback": func() {}}
		if _, err := RunLoop(ctx, cfg); err == nil {
			t.Fatal("err = nil, want an encoding error for a non-JSON provider option")
		}
		if j.recordCount() != 0 {
			t.Fatalf("records = %d, want 0", j.recordCount())
		}
	})

	t.Run("RunID is required with a journal", func(t *testing.T) {
		cfg := loopCfg(&fakeProvider{steps: loopScript()}, newMemJournal(), countingTools(map[string]int{}))
		cfg.RunID = ""
		if _, err := RunLoop(ctx, cfg); err == nil {
			t.Fatal("err = nil, want an error: idempotency keys would all be run:0, run:1, ...")
		}

		// Without a journal there is no replay, so the prefix is unused.
		cfg.Journal = nil
		if _, err := RunLoop(ctx, cfg); err != nil {
			t.Fatalf("run without a journal: %v", err)
		}
	})
}
