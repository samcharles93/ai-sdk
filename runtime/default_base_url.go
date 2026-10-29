package runtime

import (
	"github.com/samcharles93/ai-sdk/provider/anthropic"
	"github.com/samcharles93/ai-sdk/provider/cohere"
	"github.com/samcharles93/ai-sdk/provider/deepseek"
	"github.com/samcharles93/ai-sdk/provider/gemini"
	"github.com/samcharles93/ai-sdk/provider/groq"
	"github.com/samcharles93/ai-sdk/provider/minimax"
	"github.com/samcharles93/ai-sdk/provider/mistral"
	"github.com/samcharles93/ai-sdk/provider/ollama"
	"github.com/samcharles93/ai-sdk/provider/openai"
	"github.com/samcharles93/ai-sdk/provider/openaiobject"
	"github.com/samcharles93/ai-sdk/provider/perplexity"
	"github.com/samcharles93/ai-sdk/provider/togetherai"
	"github.com/samcharles93/ai-sdk/provider/xai"
)

// classDefaultBaseURL maps each built-in provider class to the default base
// URL of the provider package the class builds. Classes that require an
// explicit endpoint ("openai-compatible", "azure") have no default and are
// absent.
var classDefaultBaseURL = map[string]string{
	"anthropic":           anthropic.DefaultBaseURL,
	"cohere":              cohere.DefaultBaseURL,
	"deepseek":            deepseek.DefaultBaseURL,
	"gemini":              gemini.DefaultBaseURL,
	"gemini-interactions": gemini.DefaultBaseURL,
	"groq":                groq.DefaultBaseURL,
	"minimax":             minimax.DefaultBaseURL,
	"mistral":             mistral.DefaultBaseURL,
	"ollama":              ollama.DefaultBaseURL,
	"openai":              openai.DefaultBaseURL,
	"openaiobject":        openaiobject.DefaultBaseURL,
	"perplexity":          perplexity.DefaultBaseURL,
	"togetherai":          togetherai.DefaultBaseURL,
	"xai":                 xai.DefaultBaseURL,
}

// DefaultBaseURL returns the base URL a built-in provider class uses when its
// ProviderConfig sets no BaseURL. The second return value is false for
// classes without a default and for unregistered class names.
func DefaultBaseURL(class string) (string, bool) {
	baseURL, ok := classDefaultBaseURL[class]
	return baseURL, ok
}
