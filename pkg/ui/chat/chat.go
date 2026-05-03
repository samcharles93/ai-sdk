package chat

import (
	"context"
	"sync"

	"github.com/samcharles93/ai-sdk/pkg/uimessage"
	"github.com/samcharles93/ai-sdk/pkg/util"
)

// Chat manages the state of a chat conversation. It is the Go
// equivalent of the AI SDK's useChat() hook, ported to server-side
// state — Datastar SSE patches are produced by an outer renderer
// observing this state, not by Chat itself.
//
// Chat is safe for concurrent use; mutating methods serialise on an
// internal mutex.
type Chat struct {
	mu       sync.RWMutex
	id       string
	messages []Message
	status   Status
	err      error
	cancel   context.CancelFunc

	transport             Transport
	onToolCall            OnToolCall
	onFinish              OnFinish
	onError               OnError
	sendAutomaticallyWhen func(messages []Message) bool
}

// New constructs a [Chat] from opts. A missing transport defaults to
// [DefaultTransport]("/api/chat"); a missing ID is auto-generated.
func New(opts Options) *Chat {
	c := &Chat{
		id:                    opts.ID,
		messages:              opts.InitialMessages,
		status:                StatusReady,
		transport:             opts.Transport,
		onToolCall:            opts.OnToolCall,
		onFinish:              opts.OnFinish,
		onError:               opts.OnError,
		sendAutomaticallyWhen: opts.SendAutomaticallyWhen,
	}
	if c.id == "" {
		c.id = util.GenerateID("chat", 16)
	}
	if c.transport == nil {
		c.transport = NewDefaultTransport("/api/chat")
	}
	if c.messages == nil {
		c.messages = make([]Message, 0)
	}
	return c
}

// ID returns the chat's unique identifier.
func (c *Chat) ID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.id
}

// Messages returns a copy of the current messages.
func (c *Chat) Messages() []Message {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Message, len(c.messages))
	copy(out, c.messages)
	return out
}

// Status reports the chat's current state.
func (c *Chat) Status() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// Error returns the most recent error, if any.
func (c *Chat) Error() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.err
}

// SetMessages replaces the message list without contacting the
// transport.
func (c *Chat) SetMessages(messages []Message) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = make([]Message, len(messages))
	copy(c.messages, messages)
}

// ClearError clears any sticky error state and returns the status to
// [StatusReady] if it was [StatusError].
func (c *Chat) ClearError() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.err = nil
	if c.status == StatusError {
		c.status = StatusReady
	}
}

// Send appends a user message built from msg, then drives the
// transport and applies the resulting chunks to the chat state. It
// blocks until the assistant turn ends, the context is cancelled, or
// [Chat.Stop] is called.
func (c *Chat) Send(ctx context.Context, msg CreateMessage, _ ...SendOptions) error {
	parts := make(MessageParts, 0, 1+len(msg.Files))
	if msg.Text != "" {
		parts = append(parts, uimessage.TextUIPart{Text: msg.Text, State: uimessage.PartStateDone})
	}
	for _, f := range msg.Files {
		parts = append(parts, f)
	}
	id := msg.MessageID
	if id == "" {
		id = util.GenerateID("msg", 16)
	}
	user := Message{ID: id, Role: RoleUser, Parts: parts, Metadata: msg.Metadata}
	return c.send(ctx, user)
}

// Stop cancels the in-flight transport call, if any.
func (c *Chat) Stop() {
	c.mu.Lock()
	cancel := c.cancel
	c.cancel = nil
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Regenerate truncates messages back to (and including) messageID and
// re-runs the assistant turn.
func (c *Chat) Regenerate(ctx context.Context, messageID string) error {
	c.mu.Lock()
	idx := -1
	for i, m := range c.messages {
		if m.ID == messageID {
			idx = i
			break
		}
	}
	if idx < 0 {
		c.mu.Unlock()
		return nil
	}
	// Drop the target message and everything after it.
	c.messages = c.messages[:idx]
	c.mu.Unlock()
	return c.send(ctx, Message{})
}

// AddToolOutput records the outcome of a tool execution on the last
// assistant message and, if SendAutomaticallyWhen reports true, kicks
// off another turn.
func (c *Chat) AddToolOutput(ctx context.Context, opts AddToolOutputOptions) error {
	c.mu.Lock()
	if n := len(c.messages); n > 0 && c.messages[n-1].Role == RoleAssistant {
		mutateToolOutput(&c.messages[n-1], opts)
	}
	shouldSend := c.sendAutomaticallyWhen != nil && c.sendAutomaticallyWhen(c.messages)
	c.mu.Unlock()
	if shouldSend {
		return c.send(ctx, Message{})
	}
	return nil
}

// AddToolApprovalResponse records an approval-response on the last
// assistant message. If a SendAutomaticallyWhen predicate is
// configured and reports true, another turn is run.
func (c *Chat) AddToolApprovalResponse(ctx context.Context, opts AddToolApprovalOptions) error {
	c.mu.Lock()
	if n := len(c.messages); n > 0 && c.messages[n-1].Role == RoleAssistant {
		mutateToolApproval(&c.messages[n-1], opts)
	}
	shouldSend := c.sendAutomaticallyWhen != nil && c.sendAutomaticallyWhen(c.messages)
	c.mu.Unlock()
	if shouldSend {
		return c.send(ctx, Message{})
	}
	return nil
}

// send is the shared implementation behind Send and Regenerate. If
// user has a non-empty role, it is appended before the turn runs.
func (c *Chat) send(ctx context.Context, user Message) error {
	c.mu.Lock()
	if user.Role != "" {
		c.messages = append(c.messages, user)
	}
	c.status = StatusSubmitted
	c.err = nil
	snapshot := make([]Message, len(c.messages))
	copy(snapshot, c.messages)
	streamCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	transport := c.transport
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		if c.cancel != nil {
			// only clear if it's still ours
			c.cancel = nil
		}
		c.mu.Unlock()
		cancel()
	}()

	chunks, err := transport.SendMessages(streamCtx, c.id, snapshot, SendOptions{})
	if err != nil {
		c.fail(err)
		return err
	}

	assistantID := util.GenerateID("msg", 16)
	processor := uimessage.NewStreamProcessor(nil, assistantID)
	processor.OnToolCall = func(call uimessage.ToolInputAvailableChunk) error {
		if c.onToolCall != nil {
			c.onToolCall(ToolCall{
				ToolCallID: call.ToolCallID,
				ToolName:   call.ToolName,
				Input:      call.Input,
			})
		}
		return nil
	}
	if c.onError != nil {
		processor.OnError = c.onError
	}

	// Insert assistant placeholder; we mutate it in place as chunks arrive.
	c.mu.Lock()
	c.messages = append(c.messages, *processor.Message)
	asstIdx := len(c.messages) - 1
	c.status = StatusStreaming
	c.mu.Unlock()

	for chunk := range chunks {
		if err := processor.Apply(chunk); err != nil {
			c.fail(err)
			return err
		}
		c.mu.Lock()
		c.messages[asstIdx] = *processor.Message
		c.mu.Unlock()
	}

	c.mu.Lock()
	c.messages[asstIdx] = *processor.Message
	final := *processor.Message
	c.status = StatusReady
	c.mu.Unlock()

	if c.onFinish != nil {
		c.onFinish(FinishEvent{
			Message:      final,
			Messages:     c.Messages(),
			FinishReason: processor.FinishReason(),
		})
	}
	return nil
}

func (c *Chat) fail(err error) {
	c.mu.Lock()
	c.err = err
	c.status = StatusError
	c.mu.Unlock()
	if c.onError != nil {
		c.onError(err)
	}
}

// mutateToolOutput updates an existing tool part on msg to reflect the
// supplied output, or appends a new dynamic-tool part if no matching
// call is found.
func mutateToolOutput(msg *Message, opts AddToolOutputOptions) {
	for i, p := range msg.Parts {
		if t, ok := p.(uimessage.ToolUIPart); ok && t.ToolCallID == opts.ToolCallID {
			t.Output = opts.Output
			if opts.IsError {
				t.State = uimessage.ToolStateOutputError
				t.ErrorText = stringOf(opts.Output)
			} else {
				t.State = uimessage.ToolStateOutputAvailable
			}
			msg.Parts[i] = t
			return
		}
		if t, ok := p.(uimessage.DynamicToolUIPart); ok && t.ToolCallID == opts.ToolCallID {
			t.Output = opts.Output
			if opts.IsError {
				t.State = uimessage.ToolStateOutputError
				t.ErrorText = stringOf(opts.Output)
			} else {
				t.State = uimessage.ToolStateOutputAvailable
			}
			msg.Parts[i] = t
			return
		}
	}
}

// mutateToolApproval records approval state on the matching tool part.
func mutateToolApproval(msg *Message, opts AddToolApprovalOptions) {
	for i, p := range msg.Parts {
		t, ok := p.(uimessage.ToolUIPart)
		if !ok || t.Approval == nil || t.Approval.ID != opts.ID {
			continue
		}
		approved := opts.Approved
		t.Approval.Approved = &approved
		t.Approval.Reason = opts.Reason
		t.State = uimessage.ToolStateApprovalResponded
		msg.Parts[i] = t
		return
	}
}

func stringOf(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
