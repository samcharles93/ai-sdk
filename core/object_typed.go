package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"

	"github.com/samcharles93/ai-sdk/object"
	"github.com/samcharles93/ai-sdk/schema"
	"github.com/samcharles93/ai-sdk/util"
)

// GenerateTypedObject performs a non-streaming object generation call and
// strictly decodes the result into T, deriving req.Schema from T
// automatically (via schema.FromType) when the caller hasn't already set
// one. It builds on GenerateObject, so it inherits the same provider-nil
// and context-cancellation handling; a decode or unknown-field mismatch
// between the provider's response and T surfaces as an error wrapping
// ErrObjectDecode.
func GenerateTypedObject[T any](ctx context.Context, provider object.Provider, req object.Request) (T, error) {
	var zero T
	if req.Schema == nil {
		req.Schema = schema.FromType(reflect.TypeFor[T]()).Build()
	}

	result, err := GenerateObject(ctx, provider, req)
	if err != nil {
		return zero, err
	}

	raw, err := objectResultJSON(result)
	if err != nil {
		return zero, fmt.Errorf("%w: %w", ErrObjectDecode, err)
	}

	var out T
	if err := decodeStrict(raw, &out); err != nil {
		return zero, fmt.Errorf("%w: %w", ErrObjectDecode, err)
	}
	return out, nil
}

// StreamObjectResult is the result of a StreamTypedObject call.
type StreamObjectResult[T any] struct {
	// PartialStream delivers a best-effort decoded *T after every chunk
	// that advances the accumulated JSON text far enough to parse.
	// Fields the provider hasn't streamed yet keep T's zero value. The
	// channel closes once the underlying stream finishes, whether or not
	// it ever produced a partial value.
	PartialStream <-chan *T
	// Final resolves to the fully-decoded, strictly-validated object
	// once the stream completes. It blocks until PartialStream closes.
	Final func() (T, error)
}

// StreamTypedObject performs a streaming object generation call and
// progressively decodes the accumulated raw JSON text into T as chunks
// arrive, alongside a Final future carrying the strictly-decoded result
// (or the stream's error). It derives req.Schema from T the same way
// GenerateTypedObject does. The underlying object.ObjectStream is closed
// internally once the stream finishes.
func StreamTypedObject[T any](ctx context.Context, provider object.Provider, req object.Request) (StreamObjectResult[T], error) {
	if req.Schema == nil {
		req.Schema = schema.FromType(reflect.TypeFor[T]()).Build()
	}

	stream, err := StreamObject(ctx, provider, req)
	if err != nil {
		return StreamObjectResult[T]{}, err
	}

	partial := make(chan *T)
	final := make(chan struct {
		v   T
		err error
	}, 1)

	go func() {
		defer close(partial)
		defer stream.Close()

		var buf bytes.Buffer
		var finalErr error
	streamLoop:
		for {
			chunk, err := stream.Next(ctx)
			if err != nil {
				if !errors.Is(err, io.EOF) {
					finalErr = err
				}
				break
			}
			buf.WriteString(chunk.Delta)

			if repaired := util.RepairPartialJSON(buf.String()); repaired != "" {
				var partialVal T
				if json.Unmarshal([]byte(repaired), &partialVal) == nil {
					select {
					case partial <- &partialVal:
					case <-ctx.Done():
						finalErr = ctx.Err()
						break streamLoop
					}
				}
			}
			if chunk.Done {
				break
			}
		}

		var out T
		if finalErr == nil {
			if err := decodeStrict(buf.Bytes(), &out); err != nil {
				finalErr = fmt.Errorf("%w: %w", ErrObjectDecode, err)
			}
		}
		final <- struct {
			v   T
			err error
		}{out, finalErr}
	}()

	return StreamObjectResult[T]{
		PartialStream: partial,
		Final: func() (T, error) {
			r := <-final
			return r.v, r.err
		},
	}, nil
}

// objectResultJSON extracts the raw JSON text a provider's ObjectResult
// carries. Providers may return an object.Object (Content holds the JSON
// text), a string/[]byte/json.RawMessage directly, or any other value
// that round-trips through json.Marshal.
func objectResultJSON(result object.ObjectResult) ([]byte, error) {
	switch v := result.(type) {
	case object.Object:
		return []byte(v.Content), nil
	case *object.Object:
		return []byte(v.Content), nil
	case json.RawMessage:
		return v, nil
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	default:
		return json.Marshal(result)
	}
}

// decodeStrict decodes raw into out, rejecting fields out doesn't declare
// so schema drift between a provider's response and T is caught here
// instead of silently discarding data.
func decodeStrict(raw []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(out)
}
