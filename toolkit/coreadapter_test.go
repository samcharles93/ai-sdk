package toolkit_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samcharles93/ai-sdk/toolkit"
)

func TestCoreToolSetExecutesRegisteredTool(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello toolkit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := toolkit.NewRegistry()
	if err := toolkit.RegisterBuiltins(reg, dir); err != nil {
		t.Fatal(err)
	}

	set := reg.CoreToolSet(toolkit.HeadlessBridge{})
	read, ok := set["read"]
	if !ok {
		t.Fatalf("adapter did not expose the read tool; got %d tools", len(set))
	}

	out, err := read.Execute(context.Background(), `{"path":"hello.txt"}`)
	if err != nil {
		t.Fatalf("read execute: %v", err)
	}
	if !strings.Contains(out, "hello toolkit") {
		t.Fatalf("read output does not contain file content: %q", out)
	}
}

func TestCoreToolSetKeepsFailuresInBand(t *testing.T) {
	reg := toolkit.NewRegistry()
	if err := reg.Register(toolkit.Tool{
		Schema: toolkit.Schema{Name: "boom", Description: "always fails"},
		Execute: func(ctx context.Context, params json.RawMessage, ui toolkit.UIBridge) (toolkit.Result, error) {
			return toolkit.Result{}, errors.New("kaboom")
		},
	}); err != nil {
		t.Fatal(err)
	}

	set := reg.CoreToolSet(toolkit.HeadlessBridge{})
	out, err := set["boom"].Execute(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("executor error must stay in-band, got Go error: %v", err)
	}
	if !strings.Contains(out, "kaboom") {
		t.Fatalf("failure text not fed back to the model: %q", out)
	}
}

func TestCoreToolSetPropagatesCancellation(t *testing.T) {
	reg := toolkit.NewRegistry()
	if err := reg.Register(toolkit.Tool{
		Schema: toolkit.Schema{Name: "slow", Description: "waits for ctx"},
		Execute: func(ctx context.Context, params json.RawMessage, ui toolkit.UIBridge) (toolkit.Result, error) {
			<-ctx.Done()
			return toolkit.Result{}, ctx.Err()
		},
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := reg.CoreToolSet(toolkit.HeadlessBridge{})["slow"].Execute(ctx, `{}`)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation must propagate as a Go error, got: %v", err)
	}
}

func TestHeadlessBridgeAutoApproves(t *testing.T) {
	b := toolkit.HeadlessBridge{Session: "s1"}

	ok, err := b.Confirm(context.Background(), "t", "d")
	if err != nil || !ok {
		t.Fatalf("Confirm = (%v, %v), want (true, nil)", ok, err)
	}
	sel, err := b.Select(context.Background(), "t", []string{"first", "second"})
	if err != nil || sel != "first" {
		t.Fatalf("Select = (%q, %v), want (\"first\", nil)", sel, err)
	}
	if got := b.SessionID(); got != "s1" {
		t.Fatalf("SessionID = %q, want %q", got, "s1")
	}
}
