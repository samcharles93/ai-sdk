package handlers

import (
	"context"
	"net/http"
)

// ChatService defines the chat operations the handler needs.
// Following Go convention, the handler defines this interface rather
// than importing a service definition.
type ChatService interface {
	// TODO: define methods needed by the handler
}

// ChatHandler handles HTTP requests for the chat API endpoint.
// It processes incoming messages and streams responses via SSE (Datastar).
type ChatHandler struct {
	service ChatService
}

// NewChatHandler creates a ChatHandler with the given chat service.
func NewChatHandler(svc ChatService) *ChatHandler {
	return &ChatHandler{service: svc}
}

// ServeHTTP implements http.Handler for the chat endpoint.
func (h *ChatHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// TODO: parse incoming messages, call service, stream SSE response
	_ = context.Background()
	w.WriteHeader(http.StatusNotImplemented)
}
