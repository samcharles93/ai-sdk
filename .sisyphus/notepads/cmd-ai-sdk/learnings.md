## Provider API Patterns

All chat providers follow the same Config pattern:
- `APIKey string` — required (except Ollama)
- `BaseURL string` — optional, has default
- `HTTPClient *http.Client` — optional

All constructors return `(*Provider, error)` except Ollama which returns `*Provider` only.

## ChatHandler

`handlers.NewChatHandler(reg, defaultProvider, defaultModel)` returns an `http.Handler`
that handles both POST (send) and GET (stream) on `/chat`.

## Registry

`reg.RegisterChat(name, provider)` takes a `chat.Provider` interface directly.
The `reg.Chat(name)` returns a `*chat.Client` wrapper with `.Provider()` to unwrap.

## Ollama is Special

- No API key required
- `New()` doesn't return error
- Defaults to `http://localhost:11434` if no base URL specified
