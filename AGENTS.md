# AGENTS.md

Rules for any agent working in this repository. Short on purpose: every rule
here changes a decision. If a rule stops changing decisions, delete it.

## What this is

**ai-sdk** is a provider-agnostic Go SDK. Each AI domain (chat, embed, image,
speech, transcribe, object, video, rerank, music, decide) is a typed `Provider`
interface, concrete backends live in `provider/*`, and `runtime` resolves a
`provider/model` reference into a working provider. Its main consumer is
archie-core; design APIs for how archie-core calls them.

## Authority

When sources disagree, the higher one wins:

1. The maintainer's instruction in this session.
2. This file.
3. The code as it is in the tree.
4. Beads issues (`bd show <id>`): the plan for a piece of work.
5. Everything else (memories, comments, old docs) is a hint. Verify it against
   the code before acting on it.

## Working with the maintainer

- **A clear direction is the go-ahead.** Do it in the same turn. Do not restate
  the plan, offer options, or ask "shall I proceed?". Stop only for destructive
  or outward-facing actions (deleting data, pushing, tagging a release).
- **Post outside the repo only when told.**
- **Report, don't narrate.** Final replies say what changed, what was verified,
  and anything blocked, in a few lines. Leave out what the maintainer can see
  or infer: a clean tree, a passing gate (commits imply it), steps the task
  never asked for.

## How to decide

- **Broken beats new.** A bug that blocks work gets fixed now, with the smallest
  change that unblocks it.
- **Cheapest option first.** Before building, name the smallest change that
  solves the problem. If your plan adds a new abstraction, package or option,
  or grows past about 100 lines, stop and check you're taking the path of least
  resistance.
- **Delete before adding.** Unused code, dead paths and fallbacks nobody takes
  get removed. Solo project: breaking changes need no compatibility shims; mark
  them `feat!`/`fix!` so archie-core's bump is deliberate.
- **Finish before starting.** Complete a half-built domain or provider end to
  end before starting another.
- **Use the provider's real API.** Implement the wire format the provider
  documents. Read the provider's current docs or official SDK types before
  writing request or response structs; never guess field names.
- **Ask only for real decisions.** API shape trade-offs the maintainer owns
  are questions. Everything with a conventional answer is not: decide, do it,
  say what you chose.

## Architecture

Onion model; dependencies point inward only.

| Layer | Packages | May import |
| --- | --- | --- |
| Domain | `chat`, `embed`, `image`, `speech`, `transcribe`, `object`, `video`, `rerank`, `music`, `decide` | stdlib only |
| Infrastructure | `error`, `util`, `schema`, `logger`, `telemetry`, `prompt`, `catalog`, `upload`, `registry` | stdlib, plus domain types for `upload` and `registry` |
| Providers | `provider/*` | domain packages, infrastructure |
| Middleware | `middleware` | domain, `telemetry`, `logger`, `error` |
| Services | `core` | domain |
| Agent | `agent` | `core`, domain |
| Runtime | `runtime` | domain, providers, `core`, `catalog` |
| UI | `ui`, `uimessage` | `core`, domain, `registry`, `runtime` |

- A provider package never imports another provider package, `core`,
  `middleware`, `registry` or `ui`. Shared wire helpers go in `internal/`.
- Nothing imports `runtime` except UI and applications.
- A domain package has `doc.go`, `types.go`, `provider.go`, `client.go` (a thin
  nil-guarded facade over its own `Provider`), `errors.go`, and
  `provider_options.go` when it has options. Copy `rerank/` for a new one.
- An optional capability is its own interface (`image.Editor`,
  `speech.Streamer`), never a method added to `Provider`. Callers type-assert;
  the client facade returns the domain's not-supported error.
- Every struct has a `New` constructor taking its dependencies as interfaces.
  No package-level mutable state, no `init()` wiring.

## Runtime

- A `ProviderClass` turns a `ProviderConfig` into a `ProviderSet` with one
  optional field per domain. A class must advertise a capability in `caps` and
  return a provider that implements it; otherwise the field stays nil.
- Each domain has `rt.<Domain>(ctx, ref, req)` and `rt.<Domain>Provider(ctx,
  ref)`. A new domain adds both, its `Capability` constant and its
  `ProviderSet` field.
- Resolution is metadata-gated where the catalog or server publishes
  modalities: a model whose metadata excludes the capability is refused with
  `ErrCapabilityNotSupported` before a request is sent.
- Self-hosted OpenAI-compatible servers (llama.cpp, llama-swap, vLLM, Kokoro)
  resolve through the `openai-compatible` class with a `BaseURL` and
  `auth.type: none`.
- MiniMax is the worked example of a non-catalog provider wired as a custom
  class with per-model config.

## Code

- Idiomatic Go. Small functions, explicit errors, wrap with `%w`, return the
  domain's sentinel errors so callers use `errors.Is`.
- Fix invalid state where it is produced, not with checks where it is consumed.
- If a fix fails twice, throw the block away and rewrite it simply.
- Comments say why, briefly. No history or issue narration in code.
- Generated files (`*_templ.go`, `go generate` output) are regenerated, never
  hand-edited.

## Tests

The default for a change is no new test. Write one only when:

- something actually broke: write the test that reproduces it, see it fail on
  the assertion, then fix; or
- the change alters a wire contract: a provider's request encoding or response
  decoding, error mapping, or runtime capability resolution and gating.

Provider tests use `httptest` fakes with the provider's documented payloads.
No test calls a live API or needs a key; prove live behaviour with a throwaway
program in `/tmp` and report its output. A test must catch a real regression,
survive refactoring, run fast and be cheap to keep. Never test wiring,
constants, defaults, struct shapes or fakes. One table-driven test per
behaviour; extend the table instead of adding a second test.

## Build and gate

```bash
task check   # gofumpt, go vet, golangci-lint (includes staticcheck), deadcode, go test ./...
task fmt     # gofumpt + goimports
```

Go version is in `go.mod`. Adopt `task fmt` output as-is.

- `task check` gates the commit: `task check && git commit ...`. Never claim a
  gate result you did not just produce on that exact tree.
- Commit finished work without asking. Stage only your own paths and commit in
  the same command. Conventional Commits scoped by package (`feat(decide)`,
  `fix(openai)`). Push only when told.
- Never commit binaries, `coverage.out`, scratch files or `.beads/`. Scratch
  goes in `/tmp`.
- `ai-sdk-examples/` and `ai-sdk-local/` are separate modules; `task tidy`
  covers both.

## Credentials

Never inspect an API key: not its value, not a presence check. Run the command
and deal with credentials only if it fails for want of one.

## Issues (beads)

Work is tracked in `bd`, never in Markdown TODO files.

```bash
bd ready | bd show <id> | bd update <id> --claim | bd close <id>
```

- An issue is the plan: what is wrong or wanted, and how you know it is done.
- Bead state syncs through Dolt (`bd dolt push/pull`), only when told.
- File follow-up work you find as a bead. Do not fix it in passing.

## Changing this file

A rule is added only after a real failure it would have prevented, and it must
say which decision it changes. Remove rules that no longer change decisions.
Keep it short enough to read in full every session.
