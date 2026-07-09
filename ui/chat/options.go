package chat

// OnToolCall is invoked when the assistant emits a tool call. The
// implementation is responsible for executing the tool and calling
// [Chat.AddToolOutput] with the result.
type OnToolCall func(call ToolCall)

// ToolCall is a request to invoke a tool received during streaming.
type ToolCall struct {
	ToolCallID string
	ToolName   string
	Input      any
}

// FinishEvent is delivered to [OnFinish] when an assistant turn ends.
type FinishEvent struct {
	Message      Message
	Messages     []Message
	IsAbort      bool
	IsError      bool
	FinishReason FinishReason
}

// OnFinish is invoked when an assistant turn completes successfully.
type OnFinish func(ev FinishEvent)

// OnError is invoked when an unrecoverable error occurs during a turn.
type OnError func(err error)

// Options configures a [Chat].
type Options struct {
	// Transport delivers messages to a backend. Defaults to a
	// [DefaultTransport] pointing at "/api/chat".
	Transport Transport

	// ID is a unique identifier for the chat. Auto-generated if empty.
	ID string

	// InitialMessages seeds the chat with existing messages.
	InitialMessages []Message

	// OnToolCall is invoked when a tool call is received.
	OnToolCall OnToolCall

	// OnFinish is invoked when an assistant turn completes.
	OnFinish OnFinish

	// OnError is invoked when an error occurs.
	OnError OnError

	// SendAutomaticallyWhen, if non-nil, is consulted after a tool
	// output is recorded; a true return triggers an automatic resend.
	SendAutomaticallyWhen func(messages []Message) bool
}

// SendOptions carries per-request overrides.
type SendOptions struct {
	Headers  map[string]string
	Body     map[string]any
	Metadata any
}

// AddToolOutputOptions carries the result of a tool execution.
type AddToolOutputOptions struct {
	Tool       string
	ToolCallID string
	Output     any
	IsError    bool
}

// AddToolApprovalOptions carries the response to a tool approval
// request.
type AddToolApprovalOptions struct {
	ID       string
	Approved bool
	Reason   string
}
