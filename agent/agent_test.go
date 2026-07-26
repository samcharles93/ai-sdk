package agent

import (
	"context"
	"io"
	"testing"

	"github.com/samcharles93/ai-sdk/chat"
)

type captureProvider struct {
	request chat.Request
}

func (*captureProvider) Name() string { return "capture" }

func (*captureProvider) Chat(context.Context, chat.Request) (chat.Response, error) {
	return chat.Response{}, nil
}

func (p *captureProvider) ChatStream(_ context.Context, request chat.Request) (chat.Stream, error) {
	p.request = request
	return &singleChunkStream{chunk: chat.Chunk{FinishReason: "stop", Done: true}}, nil
}

type singleChunkStream struct {
	chunk chat.Chunk
	read  bool
}

func (s *singleChunkStream) Next(context.Context) (chat.Chunk, error) {
	if s.read {
		return chat.Chunk{}, io.EOF
	}
	s.read = true
	return s.chunk, nil
}

func (*singleChunkStream) Close() error { return nil }

func TestAgentRunForwardsProviderOptions(t *testing.T) {
	provider := &captureProvider{}
	providerOptions := map[string]any{
		"openai": map[string]any{"reasoning_effort": "none"},
	}
	a := Agent{
		Provider:        provider,
		Model:           "gpt-5.6-terra",
		ProviderOptions: providerOptions,
	}

	events, err := a.Run(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	for range events {
	}

	openaiOptions, ok := provider.request.ProviderOptions["openai"].(map[string]any)
	if !ok || openaiOptions["reasoning_effort"] != "none" {
		t.Fatalf("provider options = %#v", provider.request.ProviderOptions)
	}
}
