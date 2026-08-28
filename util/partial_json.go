package util

import (
	"encoding/json"
	"slices"
	"strings"
)

// partialJSONTrimWindow bounds how far RepairPartialJSON backtracks from
// the end of the input looking for a point it can close into valid JSON.
// Dangling incomplete tokens (a partial key, a trailing comma) are short,
// so this stays a small, cheap search even on large accumulated buffers.
const partialJSONTrimWindow = 256

// RepairPartialJSON attempts to turn a truncated JSON document into a
// syntactically valid one, for progressively decoding an object as a
// provider streams its raw JSON text. It closes any string left open at
// the point of truncation and any open objects/arrays; if the tail still
// doesn't form valid JSON (for example a key with no value yet), it
// backtracks a bounded number of characters and retries. Returns "" if no
// valid JSON could be recovered from s.
func RepairPartialJSON(s string) string {
	minEnd := max(len(s)-partialJSONTrimWindow, 0)
	for end := len(s); end > minEnd; end-- {
		candidate := closeBrackets(s[:end])
		if json.Valid([]byte(candidate)) {
			return candidate
		}
	}
	return ""
}

func closeBrackets(s string) string {
	var stack []byte
	inString := false
	escaped := false

	for _, r := range s {
		if inString {
			switch {
			case escaped:
				escaped = false
			case r == '\\':
				escaped = true
			case r == '"':
				inString = false
			}
			continue
		}
		switch r {
		case '"':
			inString = true
		case '{', '[':
			stack = append(stack, byte(r))
		case '}':
			if len(stack) > 0 && stack[len(stack)-1] == '{' {
				stack = stack[:len(stack)-1]
			}
		case ']':
			if len(stack) > 0 && stack[len(stack)-1] == '[' {
				stack = stack[:len(stack)-1]
			}
		}
	}

	var b strings.Builder
	b.WriteString(s)
	if inString {
		b.WriteByte('"')
	}
	for _, c := range slices.Backward(stack) {
		if c == '{' {
			b.WriteByte('}')
		} else {
			b.WriteByte(']')
		}
	}
	return b.String()
}
