# Phase 4 Fixes Plan

## Status
Slop-Remover agents completed Phase 1-3 analysis across 7 files.
**No code was modified by slop-removal agents** — they operated read-only per Prometheus constraints.
All identified issues are PRE-EXISTING bugs introduced during implementation, not AI slop.

## Real Bugs Found (3 items)

### Bug 1: Redundant assignment in `xai/image.go:95`
- **File**: `pkg/provider/xai/image.go:95`
- **Issue**: `body.SyncMode = true` should be `body.SyncMode = opts.SyncMode` for consistency, though the boolean result is the same.
- **Current Code**: `if opts.SyncMode { body.SyncMode = true }`
- **Fix Required**: `if opts.SyncMode { body.SyncMode = opts.SyncMode }`

### Bug 2: Endpoint selection could be clearer in `xai/video.go:126-131`
- **File**: `pkg/provider/xai/video.go:126-131`
- **Issue**: `edit-video` and `extend-video` modes set `body["video"]` identically. The `opts.Mode` field determines the endpoint separately. No actual bug, but structure is fragile.
- **Current Code**: Two identical `if` blocks setting `body["video"]`
- **Fix Options**:
  - Option A: Leave as-is (works correctly, endpoint is determined by mode at lines 144-148)
  - Option B: Use early return per endpoint to make the switch explicit (refactoring only)
- **Verdict**: This is NOT a bug — the endpoint correctly switches independently of body construction.

### Bug 3: `detectAudioFormat` length check in `groq/transcribe.go:218`
- **File**: `pkg/provider/groq/transcribe.go:218`
- **Issue**: `if len(data) < 12` is immediately followed by `if len(data) >= 3 && ...data[0]...data[2]...` — check should be `if len(data) < 3` to avoid panic on 3..11 byte inputs
- **Current Code**: `if len(data) < 12 { return "" }` then index-only-3 checks
- **Fix Required**: Change to `if len(data) < 3 { return "" }`

## Non-Issues (Slop-Remover false positives)

### `openai/embed.go:108-124`
- Code has bounds check (`if i < len(out.Embeddings)`) before assigning — is NOT a bug. Safe.

### `openai/speech.go`
- No actual slop identified by agents. Clean.

### `openai/transcribe.go`
- Agents suggested removing trivial comments — cosmetic only, no bugs.

### `cmd/ai-sdk/main.go`
- Clean — agents found no issues.

## Recommended Action

**Minimal fix**: Only Bug 3 (`groq/transcribe.go`) is a real bug that should be fixed.
Bug 1 is cosmetic/bool equivalent. Bug 2 is not a functional issue.

## Verification

After applying fixes, run:
- `gofmt -w ./...`
- `gopls check ./...`
- `go build ./...`
- `go test ./...`

## Conclusion

The slop-remover analysis revealed that the codebase is largely clean.
Only 1 minor bug was found (out-of-bounds risk in Groq audio format detection).
No AI slop was actually present in the modified files.
