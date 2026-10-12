# object

```tree
object/
  client.go           Thin Client facade with nil-guard
  doc.go              Package-level documentation
  errors.go           Sentinel errors
  provider.go         Provider interface (GenerateObject + StreamObject)
  provider_options.go Provider-specific options helpers
  stream.go           ObjectStream iterator (Next/Close), ObjectChunk
  types.go            Request, Response, Object, ObjectResult
```

**Key types:**

- `Provider` interface — `GenerateObject(ctx, req)` + `StreamObject(ctx, req)`
- `Request` — Model, Prompt, MaxTokens, Schema, ProviderOptions
- `ObjectResult` — named `any` type; providers return `object.Object` (or a
  richer shape). `core.GenerateTypedObject[T]`/`StreamTypedObject[T]` derive the
  schema from `T` and strictly decode.

**Usage via core:**

```go
result, err := core.GenerateObject(ctx, provider, objRequest)
value, err := core.GenerateTypedObject[MyType](ctx, provider, objRequest)
```

**Trap:** A provider package must not import `core` (output would be an onion
violation). Validate `GenerateTypedObject` integration in a throwaway test
package and delete it, keeping the provider→core boundary clean.
