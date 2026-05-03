package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/samcharles93/ai-sdk/pkg/chat"
	"github.com/samcharles93/ai-sdk/pkg/core"
	"github.com/samcharles93/ai-sdk/pkg/registry"
	uichat "github.com/samcharles93/ai-sdk/pkg/ui/chat"
	"github.com/samcharles93/ai-sdk/pkg/uimessage"
	"github.com/samcharles93/ai-sdk/pkg/uimessage/sse"
	"github.com/samcharles93/ai-sdk/pkg/util"
)

// ChatHandler handles HTTP requests for the chat API endpoint.
// It processes incoming messages and streams responses via SSE.
type ChatHandler struct {
	reg             *registry.Registry
	logger          *slog.Logger
	defaultProvider string
	defaultModel    string
}

// NewChatHandler creates a ChatHandler with the given registry and
// default provider configuration.
func NewChatHandler(reg *registry.Registry, defaultProvider, defaultModel string) *ChatHandler {
	return &ChatHandler{
		reg:             reg,
		logger:          slog.Default(),
		defaultProvider: defaultProvider,
		defaultModel:    defaultModel,
	}
}

// SetLogger replaces the default logger.
func (h *ChatHandler) SetLogger(logger *slog.Logger) {
	h.logger = logger
}

// ServeHTTP implements http.Handler for the chat endpoint.
// POST → handleSend (create message, run turn, return assistant message).
// GET  → handleStream (SSE streaming response).
func (h *ChatHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.handleSend(w, r)
	case http.MethodGet:
		h.handleStream(w, r)
	default:
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed: "+r.Method)
	}
}

// ---------------------------------------------------------------------------
// POST /chat — send a message and get back the assistant response
// ---------------------------------------------------------------------------

// sendRequest is the JSON body for POST /chat.
type sendRequest struct {
	Text     string `json:"text"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

func (h *ChatHandler) handleSend(w http.ResponseWriter, r *http.Request) {
	var req sendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "decode request: "+err.Error())
		return
	}
	if req.Text == "" {
		h.writeError(w, http.StatusBadRequest, "text is required")
		return
	}

	providerName := req.Provider
	if providerName == "" {
		providerName = h.defaultProvider
	}
	model := req.Model
	if model == "" {
		model = h.defaultModel
	}

	provider, err := h.resolveProvider(providerName)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	transport := uichat.NewDirectTransport(provider, core.GenerateOptions{Model: model})
	c := uichat.New(uichat.Options{Transport: transport})

	msg := uichat.CreateMessage{Text: req.Text}
	if err := c.Send(r.Context(), msg); err != nil {
		h.logger.Error("send failed", "error", err)
		h.writeError(w, http.StatusInternalServerError, "send message: "+err.Error())
		return
	}

	msgs := c.Messages()
	// Expect at least user + assistant messages.
	var assistant uichat.Message
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == uichat.RoleAssistant {
			assistant = msgs[i]
			break
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(assistant); err != nil {
		h.logger.Error("encode response", "error", err)
	}
}

// ---------------------------------------------------------------------------
// GET /chat/stream — SSE streaming endpoint
// ---------------------------------------------------------------------------

func (h *ChatHandler) handleStream(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	prompt := q.Get("prompt")
	sessionID := q.Get("sessionID")
	providerName := q.Get("provider")
	model := q.Get("model")

	if prompt == "" {
		h.writeError(w, http.StatusBadRequest, "prompt query param is required")
		return
	}
	if providerName == "" {
		providerName = h.defaultProvider
	}
	if model == "" {
		model = h.defaultModel
	}
	if sessionID == "" {
		sessionID = util.GenerateID("session", 16)
	}

	provider, err := h.resolveProvider(providerName)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	userMsg := uichat.Message{
		ID:   util.GenerateID("msg", 16),
		Role: uichat.RoleUser,
		Parts: uimessage.MessageParts{
			uimessage.TextUIPart{Text: prompt, State: uimessage.PartStateDone},
		},
	}

	transport := uichat.NewDirectTransport(provider, core.GenerateOptions{Model: model})
	chunks, err := transport.SendMessages(r.Context(), sessionID, []uichat.Message{userMsg}, uichat.SendOptions{})
	if err != nil {
		h.logger.Error("stream send failed", "error", err)
		h.writeError(w, http.StatusInternalServerError, "start stream: "+err.Error())
		return
	}

	// Use the SSE writer to handle headers, status, and flushing.
	sw := sse.NewWriter(w)
	if err := sse.Pipe(r.Context(), chunks, sw); err != nil {
		// The client may have disconnected — that's not an error we can report.
		if !isClientGone(err) {
			h.logger.Error("sse pipe", "error", err)
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func (h *ChatHandler) resolveProvider(name string) (chat.Provider, error) {
	client, err := h.reg.Chat(name)
	if err != nil {
		return nil, fmt.Errorf("resolve provider %q: %w", name, err)
	}
	return client.Provider(), nil
}

// writeError writes a JSON error response.
func (h *ChatHandler) writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// isClientGone reports whether err indicates the client disconnected.
func isClientGone(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "broken pipe") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "context canceled")
}
