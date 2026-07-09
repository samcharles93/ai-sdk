package util

import (
	"regexp"

	"github.com/samcharles93/ai-sdk/chat"
)

var wordRE = regexp.MustCompile(`\w+|[^\s\w]`)

// CountTokens approximates token count. Uses a simple heuristic:
// number of word-like tokens + punctuation tokens. Approx 1 token ≈ 4 chars
// is a guideline but we return the token count directly.
func CountTokens(text string) int {
	if text == "" {
		return 0
	}
	return len(wordRE.FindAllString(text, -1))
}

// CountMessageTokens counts tokens in a single chat.Message by counting
// its textual representation (Parts/Text or Content).
func CountMessageTokens(msg chat.Message) int {
	return CountTokens(msg.Text())
}

// CountRequestTokens counts tokens across all messages in a chat.Request
// and includes a small overhead per message.
func CountRequestTokens(req chat.Request) int {
	total := 0
	for _, m := range req.Messages {
		total += CountMessageTokens(m)
		total += 3 // overhead per message (role, separators)
	}
	// include model name length as small overhead
	total += CountTokens(req.Model)
	if req.MaxTokens > 0 {
		total += 1
	}
	return total
}
