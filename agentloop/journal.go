package agentloop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

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
	// ErrOutcomeUnknown is returned by a tool whose side effect may or may
	// not have happened. The loop records it as unknown, never retries it,
	// and stops with StopOutcomeUnknown.
	ErrOutcomeUnknown = errors.New("agentloop: tool outcome unknown")
	// ErrModelHookPanic means a ModelHook panicked. Unlike a tool panic the
	// loop stops with this error.
	ErrModelHookPanic = errors.New("agentloop: model hook panicked")
)

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
	// Output and Error are the tool result for tool entries; Error is also
	// set on model entries whose provider call failed finally.
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

// digestRequest hashes the parts of a request that must be identical on
// replay: model, messages, tools, tool choice, and sampling options.
// encoding/json sorts map keys, so the encoding is canonical.
func digestRequest(req chat.Request) string {
	type wireRequest chat.Request
	sum := sha256.Sum256(mustJSON(wireRequest(req)))
	return hex.EncodeToString(sum[:])
}

// digestToolCall hashes the tool name and the input the model sent.
func digestToolCall(name, input string) string {
	sum := sha256.Sum256([]byte(name + "\x00" + input))
	return hex.EncodeToString(sum[:])
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		// chat types marshal cleanly; a failure here is a programming error.
		panic(err)
	}
	return b
}
