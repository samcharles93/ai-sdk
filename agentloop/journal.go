package agentloop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/samcharles93/ai-sdk/chat"
)

// EntryKind identifies what a journal [Entry] records.
type EntryKind string

const (
	// EntryModel is one agent-loop model call: the assistant message, the
	// finish reason and the usage of that call.
	EntryModel EntryKind = "model"
	// EntryTool is one tool call: the output, the error, or an unknown
	// outcome.
	EntryTool EntryKind = "tool"
	// EntryCompact is one history-compaction call: the summary that
	// replaced the older turns.
	EntryCompact EntryKind = "compact"
)

// Typed errors a checkpointed run can stop with. Callers use errors.Is.
var (
	// ErrJournalRejected means the journal refused to record an entry (for
	// example the caller lost its lease on the run). The loop stops at once
	// and makes no further model or tool calls.
	ErrJournalRejected = errors.New("agentloop: journal rejected the record")
	// ErrJournalDiverged means a replayed journal entry does not match the
	// call the loop is about to make (kind, tool name or digest). The loop
	// stops rather than feed the model the wrong history.
	ErrJournalDiverged = errors.New("agentloop: journal entry diverges from the replayed call")
	// ErrModelHookPanic means a ModelHook panicked. Unlike a tool panic the
	// loop stops with this error.
	ErrModelHookPanic = errors.New("agentloop: model hook panicked")
)

// DivergenceError reports the journal position whose entry did not match the
// call being replayed, with both sides of the mismatch. It unwraps to
// [ErrJournalDiverged], so errors.Is keeps working while a run engine can read
// the position with errors.As and record why the run stopped.
type DivergenceError struct {
	// Position is the journal position that failed the check.
	Position int64
	// Held is the entry the journal holds there.
	Held         EntryKind
	HeldToolName string
	HeldDigest   string
	// Want is the call replay expected at that position.
	Want         EntryKind
	WantToolName string
	WantDigest   string
}

func (e *DivergenceError) Error() string {
	if e.HeldToolName != "" || e.WantToolName != "" {
		return fmt.Sprintf("%v: position %d holds kind %q tool %q with digest %s, replay expects kind %q tool %q with digest %s",
			ErrJournalDiverged, e.Position, e.Held, e.HeldToolName, e.HeldDigest, e.Want, e.WantToolName, e.WantDigest)
	}
	return fmt.Sprintf("%v: position %d holds kind %q with digest %s, replay expects kind %q with digest %s",
		ErrJournalDiverged, e.Position, e.Held, e.HeldDigest, e.Want, e.WantDigest)
}

// Unwrap reports the sentinel callers match with errors.Is.
func (e *DivergenceError) Unwrap() error { return ErrJournalDiverged }

// JournalRejectedError reports the position whose record the journal refused,
// alongside the journal's own error.
type JournalRejectedError struct {
	// Position is the journal position the loop tried to record.
	Position int64
	// Err is the error the journal returned.
	Err error
}

func (e *JournalRejectedError) Error() string {
	return fmt.Sprintf("%v: position %d: %v", ErrJournalRejected, e.Position, e.Err)
}

// Unwrap exposes both the sentinel and the journal's own error to errors.Is.
func (e *JournalRejectedError) Unwrap() []error { return []error{ErrJournalRejected, e.Err} }

// Stop reasons reported in LoopResult.StopReason in addition to the ones
// shared with Run.
const (
	StopOutcomeUnknown  = "outcome_unknown"
	StopJournalRejected = "journal_rejected"
	StopJournalDiverged = "journal_diverged"
	StopModelHookPanic  = "model_hook_panic"
	StopConditionFired  = "stop_condition"
)

// Entry is one journalled step of a run: a model call, a tool call, or a
// compaction. Positions are assigned by the loop in call order, starting at
// 0, so the same history always produces the same positions on replay.
//
// Entries round-trip losslessly through encoding/json: the assistant message
// keeps its text, reasoning, parts and tool calls (chat.Parts carries a
// concrete codec), and usage and finish reason decode back to exactly what
// was encoded.
type Entry struct {
	// Position is the sequence number of this call in the run.
	Position int64 `json:"position"`
	// Kind is what this entry records.
	Kind EntryKind `json:"kind"`
	// Digest is a SHA-256 of the request (model entries) or of the tool
	// name and input (tool entries). A mismatch on replay stops the loop.
	Digest string `json:"digest"`
	// ToolName and ToolCallID identify the call for tool entries.
	ToolName   string `json:"tool_name,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	// Input is the tool call's JSON arguments, as the model sent them.
	Input string `json:"input,omitempty"`
	// Message is the assistant turn for model and compact entries.
	Message *chat.Message `json:"message,omitempty"`
	// FinishReason is the wire finish reason of a model entry.
	FinishReason string `json:"finish_reason,omitempty"`
	// Usage is the token usage of this call (model and compact entries).
	Usage chat.Usage `json:"usage"`
	// Output and Error are the tool result for tool entries.
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
	// Unknown marks a tool entry whose outcome is unknown: recorded, never
	// retried, and it stops the run.
	Unknown bool `json:"unknown,omitempty"`
}

// response rebuilds the chat response a model entry stands for.
func (e *Entry) response() chat.Response {
	resp := chat.Response{FinishReason: e.FinishReason, Usage: e.Usage}
	if e.Message != nil {
		resp.Content = e.Message.Content
		resp.Parts = e.Message.Parts
		resp.ToolCalls = e.Message.ToolCalls
		resp.ProviderMetadata = e.Message.ProviderOptions
	}
	return resp
}

// Journal is the caller-owned persistence a checkpointed run consults around
// every model call and every tool call. ai-sdk owns no storage: the caller's
// run engine implements this interface, typically over durable storage with
// a lease. Lookup returns (nil, nil) when no entry exists at the position.
type Journal interface {
	// Lookup returns the entry recorded at position, or nil when absent.
	Lookup(ctx context.Context, position int64) (*Entry, error)
	// Record persists entry at its position. A non-nil error (for example a
	// lost lease) stops the loop with ErrJournalRejected.
	Record(ctx context.Context, entry Entry) error
}

// digestRequest hashes the parts of a request that decide what the model
// does: model, messages, tools, tool choice and sampling options. ModelInfo
// carries resolved capabilities filled in by the runtime, not a request
// parameter, so a catalog update between two attempts must not make a resumed
// run diverge. encoding/json sorts map keys, so the encoding is canonical.
func digestRequest(req chat.Request) (string, error) {
	type wireRequest chat.Request
	r := wireRequest(req)
	r.ModelInfo = chat.ModelInfo{}
	b, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("agentloop: encode request for digest: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// digestToolCall hashes the tool name and the input the model sent.
func digestToolCall(name, input string) string {
	sum := sha256.Sum256([]byte(name + "\x00" + input))
	return hex.EncodeToString(sum[:])
}
