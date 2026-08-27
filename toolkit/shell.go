package toolkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const (
	// shellCancelGrace is how long a cancelled command is given to finish
	// on its own before the tool starts signalling it.
	shellCancelGrace = 1 * time.Second

	// shellKillGrace is how long the tool waits after SIGTERM before
	// escalating to the SIGKILL backstop.
	shellKillGrace = 5 * time.Second
)

// ShellParams are the parameters for the shell tool.
type ShellParams struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"` // optional seconds; zero uses the caller context
}

var shellSchema = Schema{
	Name:        "shell",
	Description: fmt.Sprintf("Execute a shell command. Uses PowerShell on Windows, bash on Linux/macOS. Returns stdout and stderr, truncated to the last %d lines or %s (whichever is hit first); when truncated, the full output is saved to a temp file whose path is included in the notice. Use for builds, tests, git, and other commands - prefer the dedicated grep, find, and read tools for searching and reading files.", DefaultMaxLines, FormatSize(DefaultMaxBytes)),
	Parameters: json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {
				"type": "string",
				"description": "The shell command to execute"
			},
			"timeout": {
				"type": "integer",
				"description": "Optional timeout in seconds. Omit to use the caller's context budget."
			}
		},
		"required": ["command"]
	}`),
}

// NewShellTool creates the built-in shell execution tool.
func NewShellTool(cwd string, mq *MutationQueue) Tool {
	return Tool{
		Schema:  shellSchema,
		Source:  "builtin",
		Execute: makeShellExecutor(cwd, mq),
	}
}

func shellTimeout(seconds int) (time.Duration, bool) {
	if seconds <= 0 {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}

type bridgeWriter struct {
	bridge UIBridge
}

func (w *bridgeWriter) Write(p []byte) (n int, err error) {
	w.bridge.Log(string(p))
	return len(p), nil
}

func makeShellExecutor(cwd string, mq *MutationQueue) Executor {
	return func(ctx context.Context, params json.RawMessage, bridge UIBridge) (Result, error) {
		var p ShellParams
		if err := json.Unmarshal(params, &p); err != nil {
			return Result{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
		}

		p.Command = strings.TrimSpace(p.Command)
		if p.Command == "" {
			return Result{Content: "command is required", IsError: true}, nil
		}

		timeout, hasTimeout := shellTimeout(p.Timeout)

		if cwd != "" {
			info, err := os.Stat(cwd)
			if err != nil || !info.IsDir() {
				return Result{Content: fmt.Sprintf("invalid cwd %q", cwd), IsError: true}, nil
			}
		}

		// Serialise with file-mutation tools: shell commands may modify
		// files that write/edit are working on.
		if mq != nil {
			mq.GlobalLock()
			defer mq.GlobalUnlock()
		}

		runCtx := ctx
		cancel := func() {}
		if hasTimeout {
			runCtx, cancel = context.WithTimeout(ctx, timeout)
		}
		defer cancel()

		shell, args := shellCommand(p.Command)
		cmd := exec.Command(shell, args...)
		if cwd != "" {
			cmd.Dir = cwd
		}

		// Run the shell as its own process-group leader so cancellation
		// escalation can signal the whole tree — build steps, servers,
		// grandchild agents — rather than just the shell itself.
		setProcessGroup(cmd)

		var stdout, stderr bytes.Buffer
		bw := &bridgeWriter{bridge: bridge}
		cmd.Stdout = io.MultiWriter(&stdout, bw)
		cmd.Stderr = io.MultiWriter(&stderr, bw)

		if err := cmd.Start(); err != nil {
			return Result{Content: fmt.Sprintf("error starting command: %v", err), IsError: true}, nil
		}

		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()

		var err error
		interrupted := false
		select {
		case err = <-done:
			// Completed within budget.
		case <-runCtx.Done():
			interrupted = true
			err = signalGracefulShutdown(cmd, done)
		}

		var b strings.Builder
		if stdout.Len() > 0 {
			b.WriteString(stdout.String())
		}
		if stderr.Len() > 0 {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString("[stderr]\n")
			b.WriteString(stderr.String())
		}

		output := b.String()
		if output == "" {
			output = "(no output)"
		}

		tr := TruncateTail(output, DefaultMaxLines, DefaultMaxBytes)
		content := tr.Content
		if tr.Truncated {
			// Save the full output so the model can grep/tail it instead of
			// re-running an expensive command.
			if path, saveErr := saveFullOutput(output); saveErr == nil {
				content += "\n[full output saved to: " + path + "]"
			}
		}

		timedOut := interrupted && hasTimeout && ctx.Err() == nil && errors.Is(runCtx.Err(), context.DeadlineExceeded)
		if timedOut {
			// A timeout is reported as such even when the command exited
			// cleanly during the SIGTERM grace period: it still overran its
			// budget, and exit 0 is not a success it earned.
			return Result{
				Content: fmt.Sprintf("[timeout after: %s]\n%s", timeout, content), IsError: true,
				ErrorKind: "timeout", Truncated: tr.Truncated, ResultBytes: len(output),
			}, nil
		}
		if interrupted {
			return Result{
				Content: "[cancelled]\n" + content, IsError: true,
				ErrorKind: "cancelled", Truncated: tr.Truncated, ResultBytes: len(output),
			}, nil
		}

		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				return Result{
					Content: fmt.Sprintf("[exit code: %d]\n%s", exitErr.ExitCode(), content), IsError: true,
					ErrorKind: "command_exit", Truncated: tr.Truncated, ResultBytes: len(output),
				}, nil
			}

			return Result{Content: fmt.Sprintf("error executing command: %v", err), IsError: true}, nil
		}

		return Result{Content: content, Truncated: tr.Truncated, ResultBytes: len(output)}, nil
	}
}

// signalGracefulShutdown walks a timed-out shell command through the
// escalation the OS needs before resorting to SIGKILL, mirroring the
// child-agent supervisor's three phases:
//
//  1. give the command a grace period to finish on its own,
//  2. SIGTERM the process group so a well-behaved command can flush output
//     and clean up temp files before it dies,
//  3. SIGKILL the group — the backstop only the OS guarantees.
//
// It blocks until the command has exited. done delivers the result of
// cmd.Wait; the channel is buffered so the waiter goroutine never leaks.
func signalGracefulShutdown(cmd *exec.Cmd, done <-chan error) error {
	// Phase 1: the command may be a moment from finishing on its own.
	select {
	case err := <-done:
		return err
	case <-time.After(shellCancelGrace):
	}

	// Phase 2: SIGTERM the group so a command can trap it and clean up.
	// Signal errors are best-effort here — the SIGKILL backstop covers
	// them — so they are not surfaced.
	_ = signalProcessGroup(cmd, syscall.SIGTERM)
	select {
	case err := <-done:
		return err
	case <-time.After(shellKillGrace):
	}

	// Phase 3: SIGKILL cannot be trapped; the OS guarantees it terminates.
	_ = signalProcessGroup(cmd, syscall.SIGKILL)
	return <-done
}

// saveFullOutput writes the complete, untruncated command output to a temp
// file and returns its path.
func saveFullOutput(output string) (string, error) {
	f, err := os.CreateTemp("", "tau-shell-*.log")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(output); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// shellCommand returns the shell binary and arguments for the current platform.
func shellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		// Prefer PowerShell 7 (pwsh), fall back to Windows PowerShell.
		shell := "powershell.exe"
		if _, err := exec.LookPath("pwsh"); err == nil {
			shell = "pwsh"
		}
		return shell, []string{
			"-NoProfile",
			"-NonInteractive",
			"-ExecutionPolicy", "Bypass",
			"-Command", command,
		}
	}
	return "bash", []string{"-c", command}
}
