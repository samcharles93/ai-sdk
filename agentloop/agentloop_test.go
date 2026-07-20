package agentloop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samcharles93/ai-sdk/chat"
)

// scriptProvider replays a fixed sequence of assistant responses,
// standing in for a model. Each entry is one step; once the script is
// exhausted it answers with plain text (ending the loop).
type scriptProvider struct {
	steps []chat.Response
	i     int
	delay time.Duration
}

func (p *scriptProvider) Name() string { return "script" }

func (p *scriptProvider) Chat(ctx context.Context, req chat.Request) (chat.Response, error) {
	if p.delay > 0 {
		select {
		case <-ctx.Done():
			return chat.Response{}, ctx.Err()
		case <-time.After(p.delay):
		}
	}
	if p.i >= len(p.steps) {
		return chat.Response{Role: chat.RoleAssistant, Content: "script exhausted", FinishReason: "stop"}, nil
	}
	r := p.steps[p.i]
	p.i++
	return r, nil
}

func (p *scriptProvider) ChatStream(ctx context.Context, req chat.Request) (chat.Stream, error) {
	return nil, errors.New("streaming not supported by scriptProvider")
}

func toolStep(name, args string) chat.Response {
	return chat.Response{
		Role:         chat.RoleAssistant,
		ToolCalls:    []chat.ToolCall{{ID: "c1", Name: name, Arguments: args}},
		FinishReason: "tool_calls",
		Usage:        chat.Usage{PromptTokens: 10, CompletionTokens: 10, TotalTokens: 20},
	}
}

func runScript(t *testing.T, dir string, cfg Config, steps ...chat.Response) Result {
	t.Helper()
	cfg.Provider = &scriptProvider{steps: steps}
	cfg.Model = "script-1"
	cfg.WorkDir = dir
	if cfg.Budget.MaxSteps == 0 {
		cfg.Budget.MaxSteps = 20
	}
	res, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned infrastructure error: %v", err)
	}
	return res
}

func TestFinishPassedWithGreenGate(t *testing.T) {
	dir := t.TempDir()
	res := runScript(
		t, dir,
		Config{Gate: GateConfig{Commands: []GateCommand{{Name: "ok", Argv: []string{"true"}}}}},
		toolStep("write", `{"path":"a.txt","content":"hello"}`),
		toolStep("finish", `{"status":"passed","summary":"wrote a.txt"}`),
	)
	if res.Status != StatusPassed || res.StopReason != StopFinished {
		t.Fatalf("got %s/%s, want passed/finished (detail: %s)", res.Status, res.StopReason, res.Detail)
	}
	if res.Summary != "wrote a.txt" {
		t.Fatalf("summary = %q", res.Summary)
	}
	if len(res.Changes) != 1 || res.Changes[0] != "a.txt" {
		t.Fatalf("changes = %v", res.Changes)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatalf("file not written: %v", err)
	}
}

func TestGateParksAfterConsecutiveFailures(t *testing.T) {
	dir := t.TempDir()
	res := runScript(
		t, dir,
		Config{Gate: GateConfig{
			Commands:               []GateCommand{{Name: "always-red", Argv: []string{"false"}}},
			MaxConsecutiveFailures: 2,
		}},
		toolStep("write", `{"path":"a.txt","content":"one"}`),
		toolStep("write", `{"path":"b.txt","content":"two"}`),
		toolStep("write", `{"path":"c.txt","content":"never reached"}`),
	)
	if res.Status != StatusParked || res.StopReason != StopGateParked {
		t.Fatalf("got %s/%s, want parked/gate_parked", res.Status, res.StopReason)
	}
	if res.Detail == "" {
		t.Fatal("parked result must carry the gate failure output")
	}
	if _, err := os.Stat(filepath.Join(dir, "c.txt")); err == nil {
		t.Fatal("run should have stopped before the third write")
	}
}

func TestExpectFailureGate(t *testing.T) {
	// TDD repro stage: the gate requires the command to FAIL. A passing
	// command (no failing repro test yet) keeps the gate red.
	dir := t.TempDir()
	res := runScript(
		t, dir,
		Config{Gate: GateConfig{
			Commands:               []GateCommand{{Name: "repro-must-fail", Argv: []string{"true"}, ExpectFailure: true}},
			MaxConsecutiveFailures: 1,
		}},
		toolStep("write", `{"path":"x_test.go","content":"package x"}`),
	)
	if res.Status != StatusParked || res.StopReason != StopGateParked {
		t.Fatalf("got %s/%s, want parked/gate_parked", res.Status, res.StopReason)
	}
	if !strings.Contains(res.Detail, "expected this command to FAIL") {
		t.Fatalf("detail should explain ExpectFailure semantics: %q", res.Detail)
	}
}

func TestFinalGateOverridesFinish(t *testing.T) {
	// The model claims success but the gate is red at the end: the gate
	// is the authority. Gate command checks a file the model never
	// wrote, so cycles during the run also fail — use a gate that
	// passes per-cycle but fails at the finish check via a command that
	// flips: first run creates a marker, second run fails on it.
	dir := t.TempDir()
	script := filepath.Join(dir, "flip.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nif [ -f marker ]; then exit 1; fi\ntouch marker\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	res := runScript(
		t, dir,
		Config{Gate: GateConfig{Commands: []GateCommand{{Name: "flip", Argv: []string{"sh", "flip.sh"}}}}},
		toolStep("write", `{"path":"a.txt","content":"hello"}`),    // gate cycle 1: passes, creates marker
		toolStep("finish", `{"status":"passed","summary":"done"}`), // final gate: fails on marker
	)
	if res.Status != StatusParked || res.StopReason != StopGateFailedFinal {
		t.Fatalf("got %s/%s, want parked/gate_failed_final", res.Status, res.StopReason)
	}
}

func TestModelBlockedParks(t *testing.T) {
	res := runScript(
		t, t.TempDir(), Config{},
		toolStep("finish", `{"status":"blocked","summary":"missing credentials for X"}`),
	)
	if res.Status != StatusParked || res.StopReason != StopModelBlocked {
		t.Fatalf("got %s/%s, want parked/model_blocked", res.Status, res.StopReason)
	}
	if res.Summary != "missing credentials for X" {
		t.Fatalf("summary = %q", res.Summary)
	}
}

func TestIdleWhenNothingChanges(t *testing.T) {
	res := runScript(
		t, t.TempDir(), Config{},
		chat.Response{Role: chat.RoleAssistant, Content: "I looked around and have nothing to do.", FinishReason: "stop"},
	)
	if res.Status != StatusIdle || res.StopReason != StopIdle {
		t.Fatalf("got %s/%s, want idle/idle", res.Status, res.StopReason)
	}
}

func TestTokenBudgetParks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := runScript(
		t, dir,
		Config{Budget: Budget{MaxTokens: 30, MaxSteps: 20}},
		toolStep("write", `{"path":"a.txt","content":"1"}`), // 20 tokens
		toolStep("read", `{"path":"f.txt"}`),                // 40 total -> budget hit
		toolStep("read", `{"path":"f.txt"}`),
	)
	if res.Status != StatusParked || res.StopReason != StopBudgetExhausted {
		t.Fatalf("got %s/%s, want parked/budget_exhausted", res.Status, res.StopReason)
	}
}

func TestEndedWithoutFinishParks(t *testing.T) {
	res := runScript(
		t, t.TempDir(), Config{},
		toolStep("write", `{"path":"a.txt","content":"1"}`),
		chat.Response{Role: chat.RoleAssistant, Content: "I think that's done.", FinishReason: "stop"},
	)
	if res.Status != StatusParked || res.StopReason != StopEndedWithoutFinish {
		t.Fatalf("got %s/%s, want parked/ended_without_finish", res.Status, res.StopReason)
	}
}

func TestLoopBreakerHardStops(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Mutate first so the run isn't classified idle, then hammer an
	// identical read: 3 pass, then 3 unjustified blocks -> hard stop.
	steps := []chat.Response{toolStep("write", `{"path":"a.txt","content":"1"}`)}
	for range 7 {
		steps = append(steps, toolStep("read", `{"path":"f.txt"}`))
	}
	res := runScript(t, dir, Config{}, steps...)
	if res.Status != StatusParked || res.StopReason != StopLoopBreak {
		t.Fatalf("got %s/%s, want parked/loop_break", res.Status, res.StopReason)
	}
	if !strings.Contains(res.Detail, "identical arguments") {
		t.Fatalf("detail = %q", res.Detail)
	}
}

func TestLoopBreakerJustificationAllowsRepeats(t *testing.T) {
	l := &loopState{}
	args := `{"path":"f.txt"}`
	justified := `{"path":"f.txt","repeat_justification":"polling for state change"}`
	for range 3 {
		if msg, blocked := l.check("read", args); blocked {
			t.Fatalf("blocked within soft threshold: %s", msg)
		}
	}
	if _, blocked := l.check("read", args); !blocked {
		t.Fatal("4th identical call must be blocked")
	}
	if msg, blocked := l.check("read", justified); blocked {
		t.Fatalf("justified repeat must pass: %s", msg)
	}
	if l.hardStop {
		t.Fatal("justified repeats must never hard-stop")
	}
}

func TestWallClockTimesOut(t *testing.T) {
	res, err := Run(context.Background(), Config{
		Provider: &scriptProvider{delay: 5 * time.Second},
		Model:    "script-1",
		WorkDir:  t.TempDir(),
		Budget:   Budget{WallClock: 50 * time.Millisecond, MaxSteps: 5},
	})
	if err != nil {
		t.Fatalf("timeout must not be an infrastructure error: %v", err)
	}
	if res.Status != StatusParked || res.StopReason != StopTimedOut {
		t.Fatalf("got %s/%s, want parked/timed_out", res.Status, res.StopReason)
	}
}

func TestWriteNoteRequiresVerifiedBy(t *testing.T) {
	dir := t.TempDir()
	notes := FileNotesStore{Path: filepath.Join(dir, "NOTES.md")}
	res := runScript(
		t, dir,
		Config{Notes: notes},
		toolStep("write_note", `{"note":"unverified claim"}`),
		toolStep("write_note", `{"note":"go vet is clean at HEAD","verified_by":"go vet ./..."}`),
		toolStep("finish", `{"status":"blocked","summary":"nothing to do"}`),
	)
	if res.Status != StatusParked {
		t.Fatalf("unexpected status %s", res.Status)
	}
	content, err := notes.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(content, "unverified claim") {
		t.Fatal("note without verified_by must be rejected")
	}
	if !strings.Contains(content, "go vet is clean at HEAD") || !strings.Contains(content, "verified_by: go vet ./...") {
		t.Fatalf("verified note missing: %q", content)
	}
}

func TestProtectPathsBlocksWrites(t *testing.T) {
	dir := t.TempDir()
	res := runScript(
		t, dir,
		Config{ProtectPaths: func(p string) bool { return strings.HasSuffix(p, "_test.go") }},
		toolStep("write", `{"path":"x_test.go","content":"nope"}`),
		toolStep("write", `{"path":"x.go","content":"package x"}`),
		toolStep("finish", `{"status":"passed","summary":"done"}`),
	)
	if _, err := os.Stat(filepath.Join(dir, "x_test.go")); err == nil {
		t.Fatal("protected path must not be written")
	}
	if _, err := os.Stat(filepath.Join(dir, "x.go")); err != nil {
		t.Fatal("unprotected path must be written")
	}
	if len(res.Changes) != 1 || res.Changes[0] != "x.go" {
		t.Fatalf("changes = %v", res.Changes)
	}
}

func TestReadOnlyStripsWriteTools(t *testing.T) {
	dir := t.TempDir()
	res := runScript(
		t, dir,
		Config{ReadOnly: true},
		toolStep("write", `{"path":"a.txt","content":"nope"}`),
		toolStep("finish", `{"status":"passed","summary":"analysis done"}`),
	)
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err == nil {
		t.Fatal("write must not exist in read-only mode")
	}
	// The unknown-tool error is fed back in-band; the model then
	// finishes cleanly with no changes.
	if res.Status != StatusPassed {
		t.Fatalf("status = %s (detail %s)", res.Status, res.Detail)
	}
}

func TestGrepBlockedWhileGateDirty(t *testing.T) {
	dir := t.TempDir()
	res := runScript(
		t, dir,
		Config{Gate: GateConfig{
			Commands:               []GateCommand{{Name: "red", Argv: []string{"false"}}},
			MaxConsecutiveFailures: 5,
		}},
		toolStep("write", `{"path":"a.txt","content":"1"}`), // gate goes dirty
		toolStep("grep", `{"pattern":"anything"}`),          // must be blocked
		toolStep("finish", `{"status":"blocked","summary":"stuck"}`),
	)
	if res.Status != StatusParked || res.StopReason != StopModelBlocked {
		t.Fatalf("got %s/%s", res.Status, res.StopReason)
	}
}
