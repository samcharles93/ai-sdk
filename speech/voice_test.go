package speech

import (
	"context"
	"errors"
	"testing"
)

type fakeProvider struct{}

func (fakeProvider) Name() string { return "fake" }
func (fakeProvider) GenerateSpeech(context.Context, GenerateSpeechRequest) (GenerateSpeechResponse, error) {
	return GenerateSpeechResponse{}, nil
}

type fakeLister struct {
	fakeProvider

	voices []Voice
}

func (f fakeLister) ListVoices(context.Context, string) ([]Voice, error) {
	return f.voices, nil
}

func TestClientListVoices(t *testing.T) {
	t.Run("nil client", func(t *testing.T) {
		var c *Client
		if _, err := c.ListVoices(context.Background(), ""); !errors.Is(err, ErrNoProvider) {
			t.Fatalf("error = %v, want ErrNoProvider", err)
		}
	})

	t.Run("nil provider", func(t *testing.T) {
		c := NewClient(nil)
		if _, err := c.ListVoices(context.Background(), ""); !errors.Is(err, ErrNoProvider) {
			t.Fatalf("error = %v, want ErrNoProvider", err)
		}
	})

	t.Run("provider without lister", func(t *testing.T) {
		c := NewClient(fakeProvider{})
		if _, err := c.ListVoices(context.Background(), ""); !errors.Is(err, ErrVoiceListingNotSupported) {
			t.Fatalf("error = %v, want ErrVoiceListingNotSupported", err)
		}
	})

	t.Run("delegates to the lister", func(t *testing.T) {
		want := []Voice{{ID: "af_heart", Name: "af_heart"}}
		c := NewClient(fakeLister{voices: want})
		got, err := c.ListVoices(context.Background(), "kokoro")
		if err != nil {
			t.Fatalf("ListVoices: %v", err)
		}
		if len(got) != 1 || got[0] != want[0] {
			t.Fatalf("voices = %+v, want %+v", got, want)
		}
	})
}
