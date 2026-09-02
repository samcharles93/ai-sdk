package runtime

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/embed"
	"github.com/samcharles93/ai-sdk/image"
	"github.com/samcharles93/ai-sdk/object"
	"github.com/samcharles93/ai-sdk/rerank"
	"github.com/samcharles93/ai-sdk/video"
)

// --- fakes for the generation interfaces ---------------------------------

type fakeImageProvider struct{}

func (fakeImageProvider) Name() string { return "fakeimage" }
func (fakeImageProvider) GenerateImage(ctx context.Context, req image.GenerateImageRequest) (image.GenerateImageResponse, error) {
	return image.GenerateImageResponse{}, nil
}

type fakeVideoProvider struct{}

func (fakeVideoProvider) Name() string { return "fakevideo" }
func (fakeVideoProvider) GenerateVideo(ctx context.Context, req video.GenerateVideoRequest) (video.GenerateVideoResponse, error) {
	return video.GenerateVideoResponse{}, nil
}

type fakeObjectProvider struct{}

func (fakeObjectProvider) Name() string { return "fakeobject" }
func (fakeObjectProvider) GenerateObject(ctx context.Context, req object.Request) (object.ObjectResult, error) {
	return nil, nil
}
func (fakeObjectProvider) StreamObject(ctx context.Context, req object.Request) (object.ObjectStream, error) {
	return dummyObjectStream{}, nil
}

type dummyObjectStream struct{}

func (dummyObjectStream) Next(ctx context.Context) (object.ObjectChunk, error) {
	return object.ObjectChunk{}, io.EOF
}
func (dummyObjectStream) Close() error { return nil }

type fakeRerankProvider struct{}

func (fakeRerankProvider) Name() string { return "fakererank" }
func (fakeRerankProvider) Rerank(ctx context.Context, req rerank.Request) (rerank.Response, error) {
	return rerank.Response{}, nil
}

// genCombined implements chat+embed+image+rerank so a class built through the
// combined builder path can surface them via the type-assertion in simpleClass.New.
type genCombined struct{}

func (genCombined) Name() string { return "gencombined" }
func (genCombined) Chat(ctx context.Context, req chat.Request) (chat.Response, error) {
	return chat.Response{}, nil
}
func (genCombined) ChatStream(ctx context.Context, req chat.Request) (chat.Stream, error) {
	return nil, nil
}
func (genCombined) Embed(ctx context.Context, req embed.Request) (embed.Response, error) {
	return embed.Response{}, nil
}
func (genCombined) GenerateImage(ctx context.Context, req image.GenerateImageRequest) (image.GenerateImageResponse, error) {
	return image.GenerateImageResponse{}, nil
}
func (genCombined) Rerank(ctx context.Context, req rerank.Request) (rerank.Response, error) {
	return rerank.Response{}, nil
}

var _ providerSetBuilder = genCombined{} // verify it satisfies the combined builder contract

// --- tests ---------------------------------------------------------------

// A combined builder that also satisfies image/rerank must surface them on
// the resulting ProviderSet when (and only when) the class advertises the
// capability. This is the azure/cohere wiring path.
func TestProviderSetFromCombinedBuilderSurfacesGeneration(t *testing.T) {
	cfg := ProviderConfig{ID: "gen", Class: "gen", Auth: AuthConfig{Type: AuthTypeNone}}
	model := ModelInfo{ID: "gpt-x"}

	cls := simpleClass{
		name: "gen",
		caps: []Capability{CapabilityChat, CapabilityEmbed, CapabilityImage, CapabilityRerank},
		build: func(apiKey, baseURL string, httpClient *http.Client) (providerSetBuilder, error) {
			return genCombined{}, nil
		},
	}

	set, err := cls.New(context.Background(), cfg, model)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if set.Chat == nil || set.Embed == nil {
		t.Fatal("expected chat and embed providers to be wired")
	}
	if set.Image == nil {
		t.Fatal("expected image provider to be surfaced from the combined builder")
	}
	if set.Rerank == nil {
		t.Fatal("expected rerank provider to be surfaced from the combined builder")
	}
	if !set.Has(CapabilityImage) || !set.Has(CapabilityRerank) {
		t.Fatal("expected the set to report image/rerank support")
	}
}

// A class that advertises a generation capability but whose builder does not
// actually return a provider satisfying that interface must leave the field
// nil rather than half-wiring the set.
func TestProviderSetFromCombinedBuilderSkipsUnsupported(t *testing.T) {
	cfg := ProviderConfig{ID: "gen", Class: "gen", Auth: AuthConfig{Type: AuthTypeNone}}
	model := ModelInfo{ID: "gpt-x"}

	// Advertise video/object but build a provider that does not implement them.
	cls := simpleClass{
		name: "gen",
		caps: []Capability{CapabilityChat, CapabilityImage, CapabilityVideo, CapabilityObject},
		build: func(apiKey, baseURL string, httpClient *http.Client) (providerSetBuilder, error) {
			return genCombined{}, nil
		},
	}

	set, err := cls.New(context.Background(), cfg, model)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if set.Image == nil {
		t.Fatal("genCombined does implement image; expected it to be wired")
	}
	if set.Video != nil || set.Object != nil {
		t.Fatal("expected missing video/object providers to remain nil (capability advertised but not implemented)")
	}
}

// The granular builder path (xai/togetherai) must wire image/video/rerank
// from their own build functions and leave embed alone (xai has no embed).
func TestProviderSetFromGranularBuilders(t *testing.T) {
	cfg := ProviderConfig{ID: "xai", Class: "xai", Auth: AuthConfig{Type: AuthTypeNone}}
	model := ModelInfo{ID: "grok-video"}

	cls := simpleClass{
		name: "xai",
		caps: []Capability{CapabilityChat, CapabilityImage, CapabilityVideo},
		buildChat: func(apiKey, baseURL string, httpClient *http.Client) (chat.Provider, error) {
			return fakeProvider{}, nil
		},
		buildImage: func(apiKey, baseURL string, httpClient *http.Client) (image.Provider, error) {
			return fakeImageProvider{}, nil
		},
		buildVideo: func(apiKey, baseURL string, httpClient *http.Client) (video.Provider, error) {
			return fakeVideoProvider{}, nil
		},
	}

	set, err := cls.New(context.Background(), cfg, model)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if set.Chat == nil || set.Image == nil || set.Video == nil {
		t.Fatal("expected chat, image, and video to be wired via granular builders")
	}
	if set.Embed != nil {
		t.Fatal("xai has no embed provider; expected embed to remain nil")
	}
	if !set.Has(CapabilityImage) || !set.Has(CapabilityVideo) {
		t.Fatal("expected image/video support reported")
	}
}

// ProviderSet.Has must report the four new capabilities.
func TestProviderSetHasGenerationCapabilities(t *testing.T) {
	empty := ProviderSet{}
	for _, cap := range []Capability{CapabilityImage, CapabilityVideo, CapabilityObject, CapabilityRerank} {
		if empty.Has(cap) {
			t.Fatalf("empty set should not report %s", cap)
		}
	}

	full := ProviderSet{
		Image:  fakeImageProvider{},
		Video:  fakeVideoProvider{},
		Object: fakeObjectProvider{},
		Rerank: fakeRerankProvider{},
	}
	for _, cap := range []Capability{CapabilityImage, CapabilityVideo, CapabilityObject, CapabilityRerank} {
		if !full.Has(cap) {
			t.Fatalf("set with the provider should report %s", cap)
		}
	}
}

// The openaiobject class advertises the object capability and builds a
// provider through the granular buildObject path.
func TestOpenAIObjectClassAdvertisesObject(t *testing.T) {
	cls := openaiobjectClass()
	if !cls.Supports(CapabilityObject) {
		t.Fatal("openaiobjectClass should support CapabilityObject")
	}
	if cls.Supports(CapabilityChat) {
		t.Fatal("openaiobjectClass should not support chat")
	}

	// A non-default base URL lets openaiobject.New accept an empty key, so
	// the class New() constructs a provider without an HTTP round trip.
	cfg := ProviderConfig{ID: "openaiobject", Class: "openaiobject", BaseURL: "https://example.invalid", Auth: AuthConfig{Type: AuthTypeNone}}
	model := ModelInfo{ID: "gpt-4o-mini"}
	set, err := cls.New(context.Background(), cfg, model)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if set.Object == nil {
		t.Fatal("expected object provider to be wired")
	}
	if !set.Has(CapabilityObject) {
		t.Fatal("expected the set to report object support")
	}
}
