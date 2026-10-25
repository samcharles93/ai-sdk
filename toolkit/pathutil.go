package toolkit

import (
	"path/filepath"
	"strings"
)

const (
	// maxReadBytes is the maximum file size the read tool will load into memory.
	maxReadBytes = 5 * 1024 * 1024 // 5MB

	// maxWriteBytes is the maximum content size the write tool will accept.
	maxWriteBytes = 5 * 1024 * 1024 // 5MB
)

// resolvePath resolves a potentially relative path against the working directory.
// It also strips a leading @ (some LLMs include this).
func resolvePath(cwd, path string) string {
	path = strings.TrimPrefix(path, "@")
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(cwd, path))
}

// CanonicalPath returns the file a path argument refers to, expressed relative
// to cwd when it resolves inside it (absolute otherwise). Equivalent spellings
// — "spec.txt", "@spec.txt", "./spec.txt", an absolute path under cwd — all
// yield the same result, so a caller's path policy and the tool agree on which
// file is meant.
func CanonicalPath(cwd, path string) string {
	resolved := resolvePath(cwd, path)
	base, err := filepath.Abs(cwd)
	if err != nil {
		return resolved
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return resolved
	}
	rel, err := filepath.Rel(base, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return abs
	}
	return rel
}

// isConfined checks whether target is within (or equal to) the base directory.
// Returns false if target escapes via ../ or is an unrelated absolute path.
func isConfined(base, target string) bool {
	if base == "" {
		return true // no confinement if cwd is unset
	}
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return false
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false
	}

	rel, err := filepath.Rel(baseAbs, targetAbs)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..") && rel != ".."
}
