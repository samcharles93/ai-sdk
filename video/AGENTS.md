# video

```tree
video/
  client.go           Thin Client facade with nil-guard
  doc.go              Package-level documentation
  errors.go           Sentinel errors
  provider.go         Provider interface (GenerateVideo)
  types.go            GenerateVideoRequest, GenerateVideoResponse, VideoMode
```

**Key types:**

- `GenerateVideoRequest` — Model, Prompt, Duration, Resolution, FrameRate,
  plus typed `Mode` (`VideoModeTextToVideo`/`EditVideo`/`ExtendVideo`/
  `ReferenceToVideo`), `SourceVideo`, `ReferenceImages`, `Ratio`.
- `VideoResult` — Data, URL, MediaType. Async providers (e.g. xAI) poll
  internally inside `GenerateVideo`, so the call blocks until the video is ready
  or fails.

**Current xAI mapping:** Providers read typed mode/source/reference
fields first and fall back to legacy `ProviderOptions["xai"]` keys only when the
typed fields are unset. Do not add a competing `Duration` int-seconds field
alongside the existing string field — it creates ambiguity.
