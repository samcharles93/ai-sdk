package runtime

import (
	"context"
	"testing"

	"github.com/samcharles93/ai-sdk/pkg/chat"
)

type fakeProvider struct{}

func (fakeProvider) Name() string { return "fake" }
func (fakeProvider) Chat(ctx context.Context, req chat.Request) (chat.Response, error) {
	return chat.Response{}, nil
}
func (fakeProvider) ChatStream(ctx context.Context, req chat.Request) (chat.Stream, error) {
	return nil, nil
}

type fakeClass struct {
	name string
	caps []Capability
}

func (c fakeClass) Name() string { return c.name }
func (c fakeClass) Supports(cap Capability) bool {
	for _, s := range c.caps {
		if s == cap {
			return true
		}
	}
	return false
}
func (c fakeClass) New(ctx context.Context, cfg ProviderConfig, model ModelInfo) (ProviderSet, error) {
	return ProviderSet{Chat: fakeProvider{}}, nil
}

func TestRegisterAndGetClass(t *testing.T) {
	ClearClasses()
	c := fakeClass{name: "fake-class", caps: []Capability{CapabilityChat}}
	RegisterClass(c)

	got, ok := GetClass("fake-class")
	if !ok {
		t.Fatal("expected fake-class to be registered")
	}
	if got.Name() != "fake-class" {
		t.Fatalf("name = %q, want fake-class", got.Name())
	}
	if !got.Supports(CapabilityChat) {
		t.Fatal("expected CapabilityChat support")
	}
	if got.Supports(CapabilityEmbed) {
		t.Fatal("expected no CapabilityEmbed support")
	}
}

func TestProviderSetHas(t *testing.T) {
	empty := ProviderSet{}
	if empty.Has(CapabilityChat) {
		t.Fatal("empty set should not report chat support")
	}
	full := ProviderSet{Chat: fakeProvider{}}
	if !full.Has(CapabilityChat) {
		t.Fatal("set with chat provider should report chat support")
	}
}

func TestResolveAPIKey(t *testing.T) {
	t.Setenv("TEST_API_KEY", "secret")

	got, err := ResolveAPIKey(ProviderConfig{Auth: AuthConfig{APIKeyEnv: "TEST_API_KEY"}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "secret" {
		t.Fatalf("api key = %q, want secret", got)
	}

	_, err = ResolveAPIKey(ProviderConfig{Auth: AuthConfig{APIKeyEnv: "MISSING"}})
	if err == nil {
		t.Fatal("expected error for missing env var")
	}
}
