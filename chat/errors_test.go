package chat

import (
	"strconv"
	"strings"
	"testing"
)

func TestSanitizeErrorBody(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"empty", "", ""},
		{"short json passes through", `{"error":"bad request"}`, `{"error":"bad request"}`},
		{
			"long body truncated",
			strings.Repeat("a", maxErrorBodySnippet+50),
			strings.Repeat("a", maxErrorBodySnippet) + "… (truncated)",
		},
		{
			"html doctype collapsed",
			"<!DOCTYPE html><html><body>" + strings.Repeat("gateway timeout ", 200) + "</body></html>",
			"(html error page, " + strconv.Itoa(len("<!DOCTYPE html><html><body>"+strings.Repeat("gateway timeout ", 200)+"</body></html>")) + " bytes)",
		},
		{
			"bare html tag collapsed",
			"<html><head><title>502 Bad Gateway</title></head></html>",
			"(html error page, 56 bytes)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeErrorBody([]byte(tc.body))
			if got != tc.want {
				t.Fatalf("SanitizeErrorBody(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}
