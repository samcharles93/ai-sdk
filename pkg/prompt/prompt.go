package prompt

import (
	"fmt"
	"regexp"
	"strings"
)

// PromptTemplate is a simple template string with {var} placeholders.
// Substitution is literal string replacement; no advanced templating is used.
type PromptTemplate string

// Render substitutes placeholders in the template using the provided map.
// Unknown placeholders are left unchanged.
func (t PromptTemplate) Render(vars map[string]string) string {
	s := string(t)
	if len(vars) == 0 {
		return s
	}

	// Find {var} occurrences and replace if present in vars.
	re := regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)
	return re.ReplaceAllStringFunc(s, func(m string) string {
		name := strings.Trim(m, "{}")
		if v, ok := vars[name]; ok {
			return v
		}
		return m
	})
}

// SystemPrompt represents a prompt that sets the system / model behaviour.
type SystemPrompt string

// NewSystemPrompt constructs a SystemPrompt from instructions. If constraints
// are provided they will be appended as a short 'Constraints:' section.
func NewSystemPrompt(instructions string, constraints ...string) SystemPrompt {
	if len(constraints) == 0 {
		return SystemPrompt(strings.TrimSpace(instructions))
	}
	// Join constraints into bullet list
	b := strings.Builder{}
	b.WriteString(strings.TrimSpace(instructions))
	b.WriteString("\n\nConstraints:\n")
	for _, c := range constraints {
		b.WriteString("- ")
		b.WriteString(strings.TrimSpace(c))
		b.WriteString("\n")
	}
	return SystemPrompt(strings.TrimSpace(b.String()))
}

// UserPrompt represents a prompt coming from the user.
type UserPrompt string

// NewUserPrompt constructs a UserPrompt. It trims surrounding space.
func NewUserPrompt(text string) UserPrompt {
	return UserPrompt(strings.TrimSpace(text))
}

// FormatMessages takes a slice of message-like maps and joins them into a
// single string representation. The function expects each message to be a
// map with keys: "role" (string) and either "content" (string) or
// "parts" ([]string). This function intentionally keeps types flexible so
// it can be used by callers that marshal messages differently.
//
// Example output:
// "system: You are a helpful assistant\nuser: Tell me a joke"
func FormatMessages(msgs []map[string]any) string {
	var out []string
	for _, m := range msgs {
		roleRaw := m["role"]
		role := fmt.Sprint(roleRaw)

		// Prefer content
		if c, ok := m["content"]; ok && c != nil {
			out = append(out, fmt.Sprintf("%s: %s", role, fmt.Sprint(c)))
			continue
		}

		// Fallback to parts (slice of strings)
		if p, ok := m["parts"]; ok && p != nil {
			switch v := p.(type) {
			case []string:
				out = append(out, fmt.Sprintf("%s: %s", role, strings.Join(v, "\n")))
			case []any:
				// convert any to string
				var parts []string
				for _, e := range v {
					parts = append(parts, fmt.Sprint(e))
				}
				out = append(out, fmt.Sprintf("%s: %s", role, strings.Join(parts, "\n")))
			default:
				out = append(out, fmt.Sprintf("%s: %v", role, v))
			}
			continue
		}

		// Last resort: join remaining keys into a representation.
		out = append(out, fmt.Sprintf("%s: %v", role, m))
	}
	return strings.Join(out, "\n")
}
