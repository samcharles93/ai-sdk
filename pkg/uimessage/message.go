package uimessage

// Role identifies the author of a Message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is a single message in a chat conversation.
//
// It is the Go equivalent of TS UIMessage<METADATA, DATA, TOOLS>; Go uses
// any for Metadata, and data parts carry their type as DataUIPart.Name.
type Message struct {
	ID       string       `json:"id"`
	Role     Role         `json:"role"`
	Parts    MessageParts `json:"parts"`
	Metadata any          `json:"metadata,omitempty"`
}

// Text concatenates the text of every TextUIPart in the message.
func (m Message) Text() string {
	var s string
	for _, p := range m.Parts {
		if t, ok := p.(TextUIPart); ok {
			s += t.Text
		}
	}
	return s
}
