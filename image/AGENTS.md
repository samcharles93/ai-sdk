# image

```tree
image/
  client.go           Thin Client facade (GenerateImage + EditImage) with nil-guard
  doc.go              Package-level documentation
  errors.go           Sentinel errors (ErrNoProvider, ErrEditNotSupported, ...)
  provider.go         Provider interface (GenerateImage) + optional Editor interface
  provider_options.go Provider-specific options helpers
  types.go            GenerateImageRequest/Response, EditImageRequest/Response
```

**Key interfaces:**

- `Provider` — `GenerateImage(ctx, req)` (required)
- `Editor` — `EditImage(ctx, req)` (optional capability; type-assert to use, or
  call `image.Client.EditImage` which returns `ErrEditNotSupported` when the
  provider can't edit).

**Trap:** Keep image editing on the optional `Editor` interface, not on
`Provider`. Widening `Provider` forces every provider (azure, togetherai, xai)
and every middleware wrapper to implement the method, breaking the build for
non-editing backends.
