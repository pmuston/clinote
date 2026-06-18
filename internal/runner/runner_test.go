package runner

import (
	"context"
	"strings"
	"testing"
	"time"
)

func newRunner(t *testing.T) *Runner {
	t.Helper()
	r, err := New("bash")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestEchoAndExitZero(t *testing.T) {
	r := newRunner(t)
	res, err := r.Run(context.Background(), "echo hello-world")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit = %d, want 0", res.ExitCode)
	}
	if !strings.Contains(string(res.Output), "hello-world") {
		t.Errorf("output missing payload: %q", res.Output)
	}
	if res.Truncated {
		t.Error("unexpected Truncated=true")
	}
	if res.Duration <= 0 {
		t.Errorf("expected positive duration, got %v", res.Duration)
	}
}

func TestExitCodeNonZero(t *testing.T) {
	r := newRunner(t)
	res, err := r.Run(context.Background(), "false")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 1 {
		t.Errorf("exit = %d, want 1", res.ExitCode)
	}
}

func TestStatePersistsAcrossRuns(t *testing.T) {
	r := newRunner(t)
	if _, err := r.Run(context.Background(), "cd /tmp"); err != nil {
		t.Fatalf("cd: %v", err)
	}
	res, err := r.Run(context.Background(), "pwd")
	if err != nil {
		t.Fatalf("pwd: %v", err)
	}
	if !strings.Contains(string(res.Output), "/tmp") {
		t.Errorf("expected /tmp in pwd output, got %q", res.Output)
	}

	// Env vars also persist.
	if _, err := r.Run(context.Background(), "export FOO=bar"); err != nil {
		t.Fatalf("export: %v", err)
	}
	res, err = r.Run(context.Background(), "echo $FOO")
	if err != nil {
		t.Fatalf("echo: %v", err)
	}
	if !strings.Contains(string(res.Output), "bar") {
		t.Errorf("expected bar from env var, got %q", res.Output)
	}
}

func TestTruncation(t *testing.T) {
	r := newRunner(t)
	// Generate > 1 MiB of output. yes | head -c is the fastest.
	res, err := r.Run(context.Background(), "yes a | head -c 1200000")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Truncated {
		t.Errorf("expected Truncated=true; got Output len=%d", len(res.Output))
	}
	if len(res.Output) > MaxOutputBytes {
		t.Errorf("output exceeded cap: %d > %d", len(res.Output), MaxOutputBytes)
	}
	if res.ExitCode != 0 {
		t.Errorf("expected exit 0 even when truncated, got %d", res.ExitCode)
	}
}

func TestInterruptTerminatesSleep(t *testing.T) {
	r := newRunner(t)
	done := make(chan struct{})

	var res Result
	var runErr error
	go func() {
		res, runErr = r.Run(context.Background(), "sleep 30")
		close(done)
	}()

	// Give the sleep a moment to start under its own pgrp.
	time.Sleep(200 * time.Millisecond)
	if err := r.Interrupt(); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return within 3s of Interrupt")
	}
	if runErr != nil {
		t.Fatalf("Run error: %v", runErr)
	}
	// sleep killed by SIGINT typically exits with 130.
	if res.ExitCode == 0 {
		t.Errorf("expected non-zero exit after SIGINT, got %d", res.ExitCode)
	}
}

// The runner must not confuse output that LOOKS like a sentinel-prefixed line
// but isn't the actual sentinel.
func TestNearSentinelInOutputNotConfused(t *testing.T) {
	r := newRunner(t)
	// Print a string that resembles a sentinel pattern (the same prefix, but a
	// different unique-ish suffix), then the real command completes normally.
	res, err := r.Run(context.Background(), `printf '__CLINOTE_END_deadbeef__:0\n'; echo done`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit = %d, want 0", res.ExitCode)
	}
	if !strings.Contains(string(res.Output), "done") {
		t.Errorf("output should contain 'done': %q", res.Output)
	}
	if !strings.Contains(string(res.Output), "deadbeef") {
		t.Errorf("decoy sentinel-like content should appear in body verbatim: %q", res.Output)
	}
}

func TestStdoutStderrCapturedSeparately(t *testing.T) {
	r := newRunner(t)
	res, err := r.Run(context.Background(), `printf 'out\n'; printf 'err\n' 1>&2`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit = %d", res.ExitCode)
	}
	if string(res.Stdout) != "out\n" {
		t.Errorf("Stdout = %q, want %q", res.Stdout, "out\n")
	}
	if string(res.Stderr) != "err\n" {
		t.Errorf("Stderr = %q, want %q", res.Stderr, "err\n")
	}
	// Back-compat alias.
	if string(res.Output) != string(res.Stdout) {
		t.Errorf("Output should alias Stdout")
	}
}

func TestStderrOnFailureCaptured(t *testing.T) {
	r := newRunner(t)
	res, err := r.Run(context.Background(), `ls /this-path-does-not-exist-clinote-test 2>&1 1>/dev/null; false`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode == 0 {
		t.Errorf("expected non-zero exit, got %d", res.ExitCode)
	}
}

func TestANSIStripped(t *testing.T) {
	r := newRunner(t)
	// Emit explicit CSI red+reset around text.
	res, err := r.Run(context.Background(), `printf '\033[31mred\033[0m\n'`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	out := string(res.Output)
	if strings.ContainsRune(out, 0x1b) {
		t.Errorf("ANSI escape leaked into stripped output: %q", out)
	}
	if !strings.Contains(out, "red") {
		t.Errorf("payload missing: %q", out)
	}
}

func TestUnsupportedShell(t *testing.T) {
	if _, err := New("fish"); err == nil {
		t.Error("expected error for unsupported shell")
	}
}
