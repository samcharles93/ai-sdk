//go:build !windows

package toolkit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// alive reports whether pid still exists. Signal 0 performs the permission
// and existence checks without delivering anything (see kill(2)).
func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// TestShellTimeoutKillsGrandchildren pins the process-group contract: when a
// shell command times out, everything it spawned dies with it.
//
// exec.CommandContext on its own signals only the shell, so a backgrounded
// grandchild survives the timeout, keeps running, and holds the output pipe
// open. The shell tool therefore starts the command as a process-group leader
// and signals the whole group on cancellation.
func TestShellTimeoutKillsGrandchildren(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")

	// Background a long sleep, record its pid, then block. The tool's
	// timeout fires while the shell is parked in wait.
	script := "sleep 60 & echo $! > " + pidFile + "; wait"

	exec := makeShellExecutor(dir, nil)
	params, err := json.Marshal(ShellParams{Command: script, Timeout: 1})
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	res, err := exec(context.Background(), params, NonInteractiveBridge{})
	if err != nil {
		t.Fatalf("executor returned a Go error: %v", err)
	}
	elapsed := time.Since(start)

	// The call must not hang waiting on the orphan's inherited pipe.
	if elapsed > 30*time.Second {
		t.Fatalf("shell blocked for %v after a 1s timeout — the grandchild kept the output pipe open", elapsed)
	}
	if !res.IsError {
		t.Errorf("expected a timeout to report IsError, got: %q", res.Content)
	}

	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("grandchild never recorded its pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("unreadable pid %q: %v", raw, err)
	}

	// Reaping is asynchronous; give the signal a moment to land.
	deadline := time.Now().Add(5 * time.Second)
	for alive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	if alive(pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL) // don't leak the orphan out of the test
		t.Fatalf("grandchild pid %d survived the shell timeout — the process group was not signalled", pid)
	}
}
