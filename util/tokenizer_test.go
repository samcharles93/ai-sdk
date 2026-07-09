package util

import (
	"testing"

	"github.com/samcharles93/ai-sdk/chat"
)

func TestCountTokens(t *testing.T) {
	n := CountTokens("hello world, this is a test.")
	if n <= 0 {
		t.Fatalf("expected >0 tokens, got %d", n)
	}
}

func TestCountMessageTokens(t *testing.T) {
	m := chat.Message{Role: chat.RoleUser, Content: "hi there"}
	if CountMessageTokens(m) == 0 {
		t.Fatal("expected tokens for message")
	}
}

func TestCountRequestTokens(t *testing.T) {
	req := chat.Request{Model: "gpt-test", Messages: []chat.Message{{Role: chat.RoleUser, Content: "hello"}}}
	if CountRequestTokens(req) == 0 {
		t.Fatal("expected tokens for request")
	}
}
