package image

import "context"

// Provider is implemented by image generation model backends. Implementations
// translate between the provider-agnostic types defined in this package and
// their underlying API.
type Provider interface {
	// Name returns a short, stable identifier for the provider
	// (for example, "openai", "fal", "stability").
	Name() string

	// GenerateImage creates one or more images from the given prompt.
	GenerateImage(ctx context.Context, req GenerateImageRequest) (GenerateImageResponse, error)
}

// Editor is implemented by image providers that support editing existing
// images. It is an OPTIONAL capability: not every [Provider] will implement
// it, so callers must type-assert a [Provider] to [Editor] before attempting
// an edit (or use [Client.EditImage], which performs this check automatically
// and returns [ErrEditNotSupported] when the provider can't edit).
type Editor interface {
	// Name returns a short, stable identifier for the provider.
	Name() string

	// EditImage edits one or more existing images based on the given request.
	EditImage(ctx context.Context, req EditImageRequest) (EditImageResponse, error)
}
