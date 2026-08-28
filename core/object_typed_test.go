package core

import (
	"context"
	"errors"
	"testing"

	"github.com/samcharles93/ai-sdk/object"
)

type profile struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

// typedFakeObjectProvider is a controllable object.Provider for
// GenerateTypedObject/StreamTypedObject tests, distinct from
// fakeObjectProvider (object_impl_test.go) which only scripts streaming.
type typedFakeObjectProvider struct {
	generateResult object.ObjectResult
	generateErr    error
	streamChunks   []object.ObjectChunk
	streamErr      error
	lastRequest    object.Request
}

func (p *typedFakeObjectProvider) Name() string { return "typed-fake" }

func (p *typedFakeObjectProvider) GenerateObject(_ context.Context, req object.Request) (object.ObjectResult, error) {
	p.lastRequest = req
	return p.generateResult, p.generateErr
}

func (p *typedFakeObjectProvider) StreamObject(_ context.Context, req object.Request) (object.ObjectStream, error) {
	p.lastRequest = req
	if p.streamErr != nil {
		return nil, p.streamErr
	}
	return &fakeObjectStream{chunks: p.streamChunks}, nil
}

func TestGenerateTypedObject_DecodesObjectContent(t *testing.T) {
	p := &typedFakeObjectProvider{
		generateResult: object.Object{Content: `{"name":"Alice","age":30}`},
	}

	got, err := GenerateTypedObject[profile](context.Background(), p, object.Request{Model: "m"})
	if err != nil {
		t.Fatalf("GenerateTypedObject() error = %v", err)
	}
	if got.Name != "Alice" || got.Age != 30 {
		t.Errorf("got = %+v, want {Alice 30}", got)
	}
}

func TestGenerateTypedObject_DerivesSchemaWhenUnset(t *testing.T) {
	p := &typedFakeObjectProvider{
		generateResult: object.Object{Content: `{"name":"Bob","age":1}`},
	}

	if _, err := GenerateTypedObject[profile](context.Background(), p, object.Request{Model: "m"}); err != nil {
		t.Fatalf("GenerateTypedObject() error = %v", err)
	}
	if len(p.lastRequest.Schema) == 0 {
		t.Fatal("expected req.Schema to be populated automatically")
	}
}

func TestGenerateTypedObject_UnknownFieldIsDecodeError(t *testing.T) {
	p := &typedFakeObjectProvider{
		generateResult: object.Object{Content: `{"name":"Alice","age":30,"extra":true}`},
	}

	_, err := GenerateTypedObject[profile](context.Background(), p, object.Request{Model: "m"})
	if !errors.Is(err, ErrObjectDecode) {
		t.Fatalf("err = %v, want wrapping ErrObjectDecode", err)
	}
}

func TestGenerateTypedObject_ProviderErrorPropagates(t *testing.T) {
	wantErr := errors.New("boom")
	p := &typedFakeObjectProvider{generateErr: wantErr}

	_, err := GenerateTypedObject[profile](context.Background(), p, object.Request{Model: "m"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want wrapping %v", err, wantErr)
	}
}

func TestStreamTypedObject_PartialThenFinal(t *testing.T) {
	p := &typedFakeObjectProvider{
		streamChunks: []object.ObjectChunk{
			{Delta: `{"name":"Ali`},
			{Delta: `ce","age":30}`, Done: true},
		},
	}

	result, err := StreamTypedObject[profile](context.Background(), p, object.Request{Model: "m"})
	if err != nil {
		t.Fatalf("StreamTypedObject() error = %v", err)
	}

	var sawPartial bool
	for partial := range result.PartialStream {
		sawPartial = true
		if partial.Name == "" {
			t.Error("partial value should have a non-empty Name once streamed")
		}
	}
	if !sawPartial {
		t.Error("expected at least one partial value before the stream finished")
	}

	final, err := result.Final()
	if err != nil {
		t.Fatalf("Final() error = %v", err)
	}
	if final.Name != "Alice" || final.Age != 30 {
		t.Errorf("final = %+v, want {Alice 30}", final)
	}
}

func TestStreamTypedObject_FinalPropagatesDecodeError(t *testing.T) {
	p := &typedFakeObjectProvider{
		streamChunks: []object.ObjectChunk{
			{Delta: `{"name":"Alice","age":30,"extra":1}`, Done: true},
		},
	}

	result, err := StreamTypedObject[profile](context.Background(), p, object.Request{Model: "m"})
	if err != nil {
		t.Fatalf("StreamTypedObject() error = %v", err)
	}
	for range result.PartialStream {
	}
	if _, err := result.Final(); !errors.Is(err, ErrObjectDecode) {
		t.Fatalf("Final() err = %v, want wrapping ErrObjectDecode", err)
	}
}

func TestStreamTypedObject_ProviderErrorPropagates(t *testing.T) {
	wantErr := errors.New("stream boom")
	p := &typedFakeObjectProvider{streamErr: wantErr}

	_, err := StreamTypedObject[profile](context.Background(), p, object.Request{Model: "m"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want wrapping %v", err, wantErr)
	}
}
