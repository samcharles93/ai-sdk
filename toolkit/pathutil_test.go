package toolkit

import (
	"path/filepath"
	"testing"
)

func TestCanonicalPath(t *testing.T) {
	// Every spelling the model may use for one file inside the work
	// directory must canonicalise to the same WorkDir-relative path, and
	// a path that escapes it must not masquerade as a relative one.
	dir := t.TempDir()
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "plain relative", path: "spec.txt", want: "spec.txt"},
		{name: "dot prefix", path: "./spec.txt", want: "spec.txt"},
		{name: "at prefix", path: "@spec.txt", want: "spec.txt"},
		{name: "nested relative", path: "./a//b", want: "a/b"},
		{name: "parent hop inside", path: "./a/../spec.txt", want: "spec.txt"},
		{name: "absolute inside", path: filepath.Join(dir, "spec.txt"), want: "spec.txt"},
		{name: "escape keeps absolute", path: "../spec.txt", want: filepath.Join(filepath.Dir(dir), "spec.txt")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanonicalPath(dir, tc.path); got != tc.want {
				t.Fatalf("CanonicalPath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}
