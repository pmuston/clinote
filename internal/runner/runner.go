// Package runner spawns a persistent interactive shell under a pty and runs
// commands inside it. Each Run returns the command's combined stdout+stderr
// (ANSI-stripped) along with the exit code, tracked via a per-Runner sentinel
// string that the shell prints after the command's own output.
package runner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// MaxOutputBytes is the per-command output cap (§5.3).
const MaxOutputBytes = 1 << 20 // 1 MiB

type Runner struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	pty      *os.File
	sentinel string
	closed   bool
}

type Result struct {
	Output    []byte
	ExitCode  int
	Started   time.Time
	Duration  time.Duration
	Truncated bool
}

// New spawns an interactive shell ("bash" or "zsh") under a pty, quiets its
// prompt and echo, and returns a Runner ready to accept Run calls.
func New(shell string) (*Runner, error) {
	if shell != "bash" && shell != "zsh" {
		return nil, fmt.Errorf("unsupported shell %q", shell)
	}
	path, err := exec.LookPath(shell)
	if err != nil {
		return nil, fmt.Errorf("locate %s: %w", shell, err)
	}
	cmd := exec.Command(path, "-i")
	cmd.Env = append(os.Environ(),
		"PS1=", "PS2=", "PS3=", "PS4=",
		"PROMPT=", "RPROMPT=", "PROMPT_COMMAND=",
		"HISTFILE=/dev/null",
		"TERM=dumb",
	)

	p, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("pty start: %w", err)
	}

	r := &Runner{
		cmd:      cmd,
		pty:      p,
		sentinel: makeSentinel(),
	}
	if err := r.initShell(); err != nil {
		p.Close()
		_ = cmd.Process.Kill()
		return nil, err
	}
	return r, nil
}

// initShell sends the setup line to quiet the shell, then runs a no-op via
// the sentinel protocol to consume any banner / setup echoes.
func (r *Runner) initShell() error {
	setup := "stty -echo -onlcr; PS1=; PS2=; PROMPT=; RPROMPT=; PROMPT_COMMAND=; unset PROMPT_COMMAND HISTFILE\n"
	if _, err := r.pty.Write([]byte(setup)); err != nil {
		return err
	}
	if _, err := r.Run(context.Background(), "true"); err != nil {
		return fmt.Errorf("shell init: %w", err)
	}
	return nil
}

func makeSentinel() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand should not fail
	}
	return "__CLINOTE_END_" + hex.EncodeToString(b[:]) + "__"
}

// Run executes command in the persistent shell and returns its output and exit
// code. Only one Run may be in flight at a time. The context is currently
// advisory — to cancel a running command, use Interrupt.
func (r *Runner) Run(ctx context.Context, command string) (Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return Result{}, errors.New("runner closed")
	}

	started := time.Now()
	// Drain any pending pty bytes that may have accumulated between commands
	// (e.g. shell-side prompt updates that aren't fully silenced).
	r.drain()

	// Send: <command>\nprintf '\n<sentinel>:%d\n' "$?"\n
	full := command + "\nprintf '\\n" + r.sentinel + ":%d\\n' \"$?\"\n"
	if _, err := r.pty.Write([]byte(full)); err != nil {
		return Result{}, fmt.Errorf("pty write: %w", err)
	}

	body, exit, truncated, err := r.readUntilSentinel()
	if err != nil {
		return Result{}, err
	}
	return Result{
		Output:    stripANSI(body),
		ExitCode:  exit,
		Started:   started,
		Duration:  time.Since(started),
		Truncated: truncated,
	}, nil
}

// drain reads any pty bytes available without blocking. Used between commands
// to clear stray output.
func (r *Runner) drain() {
	if err := r.pty.SetReadDeadline(time.Now().Add(20 * time.Millisecond)); err != nil {
		return
	}
	defer r.pty.SetReadDeadline(time.Time{})
	buf := make([]byte, 4096)
	for {
		n, err := r.pty.Read(buf)
		if n == 0 || err != nil {
			return
		}
	}
}

// readUntilSentinel reads pty output until the sentinel-tagged exit-code line
// appears. The body returned excludes the sentinel line and the leading newline
// the printf injects before it. Output bytes beyond MaxOutputBytes are dropped,
// but reading continues so the shell stays in sync — Truncated indicates this.
func (r *Runner) readUntilSentinel() ([]byte, int, bool, error) {
	sentinelLine := []byte("\n" + r.sentinel + ":")

	// We keep body (capped at MaxOutputBytes) and tail (rolling window large
	// enough to always contain the sentinel line when it arrives). Sentinel
	// detection scans the tail; truncation never hides the sentinel.
	const tailCap = 8192
	var body bytes.Buffer
	body.Grow(64 * 1024)
	tail := make([]byte, 0, tailCap)

	truncated := false
	chunk := make([]byte, 4096)

	for {
		n, err := r.pty.Read(chunk)
		if n > 0 {
			data := chunk[:n]

			// Append to body up to the cap.
			room := MaxOutputBytes - body.Len()
			if room > 0 {
				if len(data) <= room {
					body.Write(data)
				} else {
					body.Write(data[:room])
					truncated = true
				}
			} else {
				truncated = true
			}

			// Maintain trailing window for sentinel detection.
			if len(tail)+len(data) <= tailCap {
				tail = append(tail, data...)
			} else {
				combined := append(tail, data...)
				tail = combined[len(combined)-tailCap:]
			}

			if idx := bytes.Index(tail, sentinelLine); idx >= 0 {
				after := idx + 1 // skip the leading \n
				nlIdx := bytes.IndexByte(tail[after:], '\n')
				if nlIdx < 0 {
					// Sentinel line not yet complete; keep reading.
					continue
				}
				line := tail[after : after+nlIdx]
				colonIdx := bytes.IndexByte(line, ':')
				if colonIdx < 0 {
					return nil, 0, truncated, fmt.Errorf("malformed sentinel line: %q", line)
				}
				exit, perr := strconv.Atoi(string(line[colonIdx+1:]))
				if perr != nil {
					return nil, 0, truncated, fmt.Errorf("parse exit code from %q: %w", line, perr)
				}

				bodyBytes := body.Bytes()
				if oidx := bytes.Index(bodyBytes, sentinelLine); oidx >= 0 {
					bodyBytes = bodyBytes[:oidx]
				}
				return bodyBytes, exit, truncated, nil
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, 0, truncated, io.ErrUnexpectedEOF
			}
			return nil, 0, truncated, fmt.Errorf("pty read: %w", err)
		}
	}
}

// Interrupt sends SIGINT to the foreground process group of the pty. The
// currently-executing Run will see the sentinel arrive shortly with a non-zero
// exit code (usually 130).
func (r *Runner) Interrupt() error {
	if r.pty == nil {
		return errors.New("runner not started")
	}
	pgrp, err := unix.IoctlGetInt(int(r.pty.Fd()), unix.TIOCGPGRP)
	if err != nil {
		return fmt.Errorf("TIOCGPGRP: %w", err)
	}
	return syscall.Kill(-pgrp, syscall.SIGINT)
}

// Close terminates the shell and releases the pty. The pty is closed first so
// the shell sees EOF on stdin and exits on its own; if it lingers, it gets
// killed. Wait is bounded to avoid hanging on weird process states.
func (r *Runner) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	_ = r.pty.Close()
	if r.cmd.Process != nil {
		_ = r.cmd.Process.Kill()
	}
	done := make(chan struct{})
	go func() {
		_ = r.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		// Give up cleanly; the process is gone or detached.
	}
	return nil
}

// ANSI escape stripper. CSI sequences cover the vast majority of terminal
// formatting; OSC (title-set) sequences are also stripped; bare ESC bytes are
// removed afterwards.
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07]*\x07`)

func stripANSI(b []byte) []byte {
	b = ansiPattern.ReplaceAll(b, nil)
	return bytes.ReplaceAll(b, []byte{0x1b}, nil)
}
