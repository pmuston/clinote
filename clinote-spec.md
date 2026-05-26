# clinote — v1 Specification

A personal lab notebook for shell commands. One markdown file = one notebook. A persistent shell session is bound to the notebook for the lifetime of the server process. Commands are run from fenced code cells; outputs are captured back into the same markdown file as adjacent fenced blocks.

This document is the v1 contract. Anything not specified here is out of scope. A `FUTURE.md` section at the end lists deferred features — do not implement them.

---

## 1. Goals and non-goals

### Goals

- A single Go binary that opens a `.md` notebook in a browser-based UI.
- A persistent shell session per notebook; state (cwd, env vars, shell functions) flows between cells.
- Run a cell, capture its output, splice the output back into the markdown file on disk.
- The on-disk file is plain CommonMark Markdown — readable, grep-able, renders correctly on GitHub.
- Round-trip safety: parsing then re-serialising a notebook without edits produces byte-identical output.
- Typed outputs: `text` (default), `csv`, `jsonl`, with sortable-table rendering in the browser.

### Non-goals (v1)

- Multi-user, auth, collaboration.
- CI/headless execution of notebooks.
- Cloud-provider rendering panels.
- Interactive TUI applications (`vim`, `less`, `htop`) — out of scope, may hang.
- Multiple language kernels — shell only.
- `$LAST_OUTPUT` or any cell-to-cell variable passing.
- Sidecar files for large output.
- Cell IDs, cell tags (`secret`, `skip`, `timeout`), per-cell environment overrides.
- Separating stdout and stderr — they are merged.
- Binary output types (images, etc.).
- WebSocket / SSE streaming. (See §6 — output is async with a spinner, populated when the command completes.)

---

## 2. File format

### 2.1 Overall structure

A notebook is a UTF-8 Markdown file with optional YAML front matter:

````markdown
---
title: Disk usage investigation
created: 2026-05-26T14:30:00Z
shell: bash
---

# Heading

Prose paragraphs and any other markdown.

```sh out=text
du -sh /var/* | sort -h | tail
```

```output type=text exit=0 ran=2026-05-26T14:31:12Z dur=1.2s
4.0K    /var/games
2.1G    /var/log
```

More prose.
````

### 2.2 Front matter

- Optional. If present, the file must start with `---\n`, followed by YAML, followed by `---\n`.
- Recognised fields:
  - `title` (string)
  - `created` (RFC 3339 timestamp)
  - `shell` (string; `bash` or `zsh`; defaults to `bash` if absent)
- Unknown fields must be preserved on round-trip.
- If the file does not start with `---\n`, there is no front matter; the parser must not consume any leading content.

### 2.3 Cells

Two kinds of fenced code blocks have semantic meaning:

**Command cells** — language tag `sh`:

````markdown
```sh out=csv
psql -c "select * from users" --csv
```
````

- Body is one or more lines of shell input, sent verbatim to the persistent shell.
- Info string attributes (after `sh`):
  - `out=text|csv|jsonl` — declared output type hint. Optional. Defaults to `text`.
  - Unknown attributes are preserved on round-trip but ignored by the runner.

**Output cells** — language tag `output`:

````markdown
```output type=csv exit=0 ran=2026-05-26T14:31:12Z dur=0.3s
user_id,email
1042,alice@example.com
```
````

- Info string attributes:
  - `type=text|csv|jsonl` — required; written by the tool.
  - `exit=<int>` — exit code; required; written by the tool.
  - `ran=<RFC 3339>` — start time; required; written by the tool.
  - `dur=<Go duration>` — wall-clock duration (e.g. `1.2s`, `300ms`); required; written by the tool.
  - `truncated=true` — present iff output exceeded the cap (see §5.3).
  - Unknown attributes preserved on round-trip.

All other fenced code blocks (language tags other than `sh` or `output`, or no language tag) are treated as prose — rendered but not executed.

### 2.4 Pairing rules

- An `output` block is owned by the most recent preceding `sh` block **if and only if** only whitespace (blank lines) separates them.
- If any non-whitespace content (prose, another fenced block) intervenes, the `output` block is orphaned and the `sh` block is treated as unrun.
- Pairing is strictly positional. No IDs, no cross-references.

### 2.5 Fence length

Output bodies may contain literal triple-backtick sequences. Output blocks must use a fence of length `(longest run of backticks in the body) + 1`, minimum 3.

### 2.6 Info string parsing

- After the language tag, attributes are whitespace-separated `key=value` pairs.
- Bare tokens (no `=`) are stored with value `""`.
- Values may not contain whitespace in v1 (no quoting support).

---

## 3. Project layout and surface

### 3.1 Module name and binary

- Module: `github.com/<user>/clinote` (use a placeholder if the user hasn't set this).
- Binary: `clinote`.

### 3.2 CLI

```
clinote [path]
```

- With a path to a `.md` file: open that notebook.
- With no path: show a picker UI listing `.md` files in the current working directory (non-recursive).
- Server binds to `127.0.0.1` on a free port; print the URL to stdout and open the browser if `BROWSER` env var is unset to disallow.

### 3.3 Suggested package layout

```
cmd/clinote/main.go         # entrypoint, flag parsing, server bootstrap
internal/notebook/          # parser, rewriter, types (see §4)
internal/runner/            # persistent shell + sentinel-based command execution (see §5)
internal/server/            # Echo handlers, HTMX endpoints (see §6)
internal/render/            # CSV/JSONL/text rendering for the browser
web/                        # static assets (HTMX, CSS, minimal JS for ANSI rendering)
web/templates/              # Go html/template files
```

### 3.4 Dependencies

- `github.com/labstack/echo/v4` — HTTP server.
- `github.com/yuin/goldmark` — Markdown parser (structural scan only; not for HTML rendering).
- `gopkg.in/yaml.v3` — YAML front matter.
- `github.com/creack/pty` — pty for the persistent shell.
- Minimal client-side: HTMX, a small ANSI-to-HTML converter (e.g. `anser` or a tiny hand-written one — see §7.3). No frontend build step.

---

## 4. The `notebook` package

### 4.1 Types

```go
package notebook

import "time"

type Notebook struct {
    Source      []byte       // original file bytes; never mutated after Parse
    FrontMatter FrontMatter
    Blocks      []Block      // in document order
}

type FrontMatter struct {
    Title   string
    Created time.Time
    Shell   string  // "bash" if empty in source

    Raw []byte     // original YAML for round-trip of unknown fields
    Present bool   // false if file had no `---` header
}

type Block interface {
    Span() (start, end int) // byte range in Notebook.Source
    isBlock()
}

type ProseBlock struct {
    Start, End int
}

type CommandBlock struct {
    Start, End int          // full span including fences and trailing newline
    BodyStart  int          // first byte after opening fence line
    BodyEnd    int          // byte before closing fence line
    InfoString string       // raw info text after "sh"
    Attrs      map[string]string
}

func (c CommandBlock) Body(src []byte) string {
    return string(src[c.BodyStart:c.BodyEnd])
}

type OutputBlock struct {
    Start, End int
    BodyStart  int
    BodyEnd    int
    InfoString string
    Attrs      map[string]string
}

func (o OutputBlock) Body(src []byte) string {
    return string(src[o.BodyStart:o.BodyEnd])
}

func (o OutputBlock) Type() string             { /* "text" if absent */ }
func (o OutputBlock) ExitCode() (int, bool)    { /* parse, ok=false if absent/bad */ }
func (o OutputBlock) Ran() (time.Time, bool)
func (o OutputBlock) Duration() (time.Duration, bool)
func (o OutputBlock) Truncated() bool
```

All three concrete block types implement `Block` with a `Span()` method and an unexported `isBlock()` method.

### 4.2 Parser

```go
func Parse(r io.Reader) (*Notebook, error)
```

Algorithm:

1. Read all bytes into memory. Notebooks are small; do not stream.
2. If the source begins with `---\n`, find the next line that is exactly `---\n`. The bytes between are YAML; parse with `gopkg.in/yaml.v3` into `FrontMatter`. Store the raw YAML bytes in `FrontMatter.Raw`. Set `FrontMatter.Present = true`. Record the body offset.
3. Run goldmark's parser on the body. Walk the AST and collect every `*ast.FencedCodeBlock` with its byte ranges (use the segment information goldmark provides; translate to absolute file offsets by adding the body offset).
4. For each fenced block, parse its info string with `ParseInfoString`. Classify:
   - Language `sh` → `CommandBlock`.
   - Language `output` → `OutputBlock`.
   - Anything else → ignore (it'll fall into a `ProseBlock`).
5. Build `Blocks` by walking the file linearly. For every gap between consecutive recognised blocks (or between body start / end and the first / last recognised block), emit a `ProseBlock` if the gap is non-empty.
6. Return the `Notebook`.

```go
func ParseInfoString(info string) (lang string, attrs map[string]string)
```

- Trim whitespace.
- Split on whitespace. First token is `lang`. Each remaining token is split on `=`: `k=v` → `attrs[k] = v`; bare `k` → `attrs[k] = ""`.

### 4.3 Rewriter

```go
// Serialize returns the current on-disk representation of the notebook.
// If no edits have been applied, the result is byte-identical to Source.
func (nb *Notebook) Serialize() []byte
```

Internally, the notebook may carry a pending list of edits (byte-range splices) that are applied on `Serialize()`. The simplest implementation: keep `Source` as-is and rebuild on each edit, since notebooks are small.

```go
// SetOutput replaces (or inserts) the output block following the CommandBlock
// at blockIdx. Returns an error if blockIdx is not a CommandBlock.
//
// If an OutputBlock immediately follows (whitespace-only between), it is
// replaced. Otherwise a new OutputBlock is inserted directly after the
// CommandBlock with a single blank line of separation.
func (nb *Notebook) SetOutput(blockIdx int, body string, attrs map[string]string) error
```

```go
// SetProse replaces the bytes of the ProseBlock at blockIdx with newText.
// newText is taken verbatim — the caller is responsible for trailing newlines.
func (nb *Notebook) SetProse(blockIdx int, newText string) error
```

```go
// WriteFile writes Serialize() to the path atomically (write to temp + rename).
func (nb *Notebook) WriteFile(path string) error
```

### 4.4 Round-trip invariant

For every input the parser accepts:

```
src := <input bytes>
nb, err := Parse(bytes.NewReader(src))
require.NoError(t, err)
require.Equal(t, src, nb.Serialize())
```

This must hold for all v1 fixture files. The simplest way to guarantee this is for `Serialize()` to return `Source` unchanged when no edits have been recorded, and for splice operations to preserve everything outside the spliced range exactly.

### 4.5 Output block rendering

```go
// renderOutputBlock formats an output block as bytes, ready to splice.
// It picks a fence length one greater than the longest backtick run in body.
// Attributes are written in a stable order: type, exit, ran, dur, truncated,
// then any remaining keys sorted alphabetically.
func renderOutputBlock(body string, attrs map[string]string) []byte
```

Output format:

```
<fence> output <attrs>
<body>
<fence>
```

Where `<fence>` is `n` backticks (n ≥ 3, n = max-backtick-run-in-body + 1). Body is appended verbatim; if it doesn't end with a newline, one is added before the closing fence.

---

## 5. The `runner` package

### 5.1 Types

```go
package runner

import (
    "context"
    "time"
)

type Runner struct {
    // unexported: pty, shell pid, sentinel state, mutex
}

func New(shell string) (*Runner, error)  // "bash" or "zsh"

func (r *Runner) Run(ctx context.Context, command string) (Result, error)

func (r *Runner) Interrupt() error  // SIGINT to foreground process group

func (r *Runner) Close() error

type Result struct {
    Output    []byte    // combined stdout+stderr, ANSI-stripped (see §5.4)
    ExitCode  int
    Started   time.Time
    Duration  time.Duration
    Truncated bool
}
```

### 5.2 Shell session

- On `New`, spawn the chosen shell with `-i` under a pty: `bash -i` or `zsh -i`.
- Set a stable, simple `PS1` (e.g. `PS1='\u0001\u0002'` or empty) to minimise prompt noise in output. Disable history expansion side effects if necessary.
- The shell lives for the lifetime of the `Runner`. Only one `Run` call may be in flight at a time (guard with a mutex).

### 5.3 Sentinel-based command execution

To capture exit code and detect command completion:

1. Generate a unique sentinel string per `Runner` instance, e.g. `__CLINOTE_END_<random hex>__`.
2. For each `Run(command)`:
   1. Record `Started = time.Now()`.
   2. Drain any pending pty output (it shouldn't exist, but be safe).
   3. Write to the pty: `command + "\nprintf '\\n" + sentinel + ":%d\\n' \"$?\"\n"`.
   4. Read pty output into a buffer until a line matching `<sentinel>:<digits>` appears.
   5. Extract the exit code from that line; remove the sentinel line from the buffer.
   6. Record `Duration = time.Since(Started)`.
3. Output is capped at **1 MiB** of bytes. If the cap is reached, stop appending, set `Truncated = true`, but continue reading until the sentinel arrives (so the shell stays in sync).
4. Strip ANSI escape sequences from `Output` before returning (see §5.4).

### 5.4 ANSI handling

- Output written to the `.md` file is plain text with ANSI escape sequences stripped.
- Use a simple regex strip: remove all `\x1b\[[0-9;]*[a-zA-Z]` sequences and the bare `\x1b` and friends. (The browser renders colour live; see §7.3.)

### 5.5 Interrupt

- `Interrupt()` sends `SIGINT` to the foreground process group of the pty (use `unix.IoctlGetInt(fd, unix.TIOCGPGRP)` or send to the shell's pid and rely on job control).
- The currently-executing `Run` call will then see the sentinel arrive shortly with a non-zero exit code.

### 5.6 TUI applications

- Commands that try to take over the terminal (`vim`, `less`, `htop`) are out of scope. They may hang. Users use the "interrupt" button to recover.
- Do **not** attempt to detect and refuse them in v1.

---

## 6. The `server` package

### 6.1 Bootstrap

- `main` opens the notebook file, parses it, creates a `Runner`, starts an Echo server on `127.0.0.1:<free port>`, prints the URL, and (if `BROWSER` env var is unset to disallow) opens the URL in the default browser.
- Server holds:
  - The path to the `.md` file.
  - The current parsed `*notebook.Notebook`.
  - The `*runner.Runner`.
  - A mutex guarding all of the above.
  - A map of `cellIdx → runState` (idle / running / done / failed) for the spinner UI.

### 6.2 Endpoints

- `GET /` — render the notebook as HTML. Each command cell is rendered with a "Run" button; output cells render according to type (§7). Each prose block has a "Edit" affordance (click to swap to a `<textarea>`).
- `POST /run/:idx` — run the command at block index `idx`.
  - Returns an HTMX fragment immediately: the command cell with a spinner shown beneath it. The actual run happens in a goroutine.
  - When the run completes, the server holds the new output ready for the next poll/fetch.
  - **Strategy: HTMX polling.** The spinner element has `hx-get="/cell/:idx" hx-trigger="every 500ms"`. When the run is done, `/cell/:idx` returns the command + rendered output (no `hx-trigger`); when still running, it returns the spinner fragment again. This avoids any need for WebSockets/SSE in v1.
- `GET /cell/:idx` — return the HTMX fragment for cell `idx` reflecting its current state.
- `POST /interrupt` — send SIGINT to the currently-running command. Returns 204.
- `POST /prose/:idx` — accept form-encoded `text`; replace the prose block; save; return the rendered prose fragment.
- `GET /picker` — only used if `clinote` was invoked without a path; lists `*.md` in the cwd.

### 6.3 Save behaviour

- After every successful run (regardless of exit code), the notebook is auto-saved to disk via `Notebook.WriteFile`.
- After every prose edit, the notebook is auto-saved.
- A visible header indicator shows the file path and a "saved" / "saving…" status. Unsaved-changes indicator is only relevant for in-flight prose edits (between user clicking out of a textarea and save completing) — for v1 this is brief enough that a simple "saving…" flash is sufficient.

### 6.4 Concurrency

- Only one `Run` may be in flight at a time. Other Run requests return 409 Conflict.
- All notebook mutations happen under the server's mutex.
- The notebook is re-parsed from disk at startup only. In-memory state is the source of truth during a session; if the user edits the file externally while the server is running, those edits will be overwritten on next save. Document this in the README; do not solve it in v1.

---

## 7. Rendering

### 7.1 Page structure

- Header: notebook title, file path, save status, "interrupt" button (visible when a command is running).
- Body: blocks rendered in order.
  - Prose blocks: rendered with goldmark to HTML, wrapped in a `<div class="prose">` with an "edit" button. Clicking swaps to a `<textarea>` and a "save" button.
  - Command blocks: a `<pre><code>` with the command, a "Run" button, and a slot for the output rendering below.
  - Output blocks: rendered by type (§7.2–§7.4).

### 7.2 Text output

- Wrapped in `<pre class="output output-text">`.
- ANSI escapes converted to HTML spans with inline styles (see §7.3).
- Footer shows `exit=N · dur=...` muted; non-zero exits get a red accent.

### 7.3 ANSI rendering in the browser

- The on-disk `.md` file contains ANSI-stripped text (§5.4). But for the **live render after a run completes**, the server has the original (with-ANSI) bytes briefly in memory; that version is passed to the renderer for the initial display.
- Convert ANSI escape sequences (SGR only — colours, bold, underline) to `<span style="...">…</span>`. A minimal hand-written converter is sufficient — support 16 standard colours, bold, underline. Ignore everything else (cursor movement, clear screen).
- On page reload, the .md file is the source of truth and contains no ANSI — output renders as plain text. This is acceptable for v1; colour is a live-run nicety.

### 7.4 CSV output

- Parse with `encoding/csv`. First row is the header.
- Render as an HTML `<table>` with sortable column headers (click to sort ascending/descending; numeric columns sort numerically, others lexically — detect by attempting `strconv.ParseFloat` on all values in the column).
- Cap rendered rows at 1000 in the browser; if more, show a "showing 1000 of N" notice. The full data remains in the .md file.

### 7.5 JSONL output

- Parse each line as JSON. Flatten to columns: union of top-level keys across all rows. Missing values render as empty cells.
- Nested objects/arrays render as their compact JSON string in the cell.
- Sortable the same way as CSV.
- Same 1000-row render cap.

### 7.6 Type sniffing

- The output block's `type=` attribute is authoritative.
- If absent (e.g. the user hand-wrote the block), sniff: valid JSON on every non-empty line and at least one line → `jsonl`; comma-separated with consistent column count across lines → `csv`; otherwise → `text`. Sniffing affects rendering only; it does not rewrite the `.md` file.

---

## 8. Test plan

### 8.1 Parser/rewriter tests (table-driven, in `internal/notebook`)

Required fixtures:

1. Empty file.
2. Front matter only, no body.
3. Body only (no front matter).
4. Prose only, no cells.
5. Single command, no output.
6. Single command, immediately-following output (no blank line between fences).
7. Single command, blank-line-separated output.
8. Command, prose paragraph, output — output is **orphaned**, not paired.
9. Two commands in a row, output between — first owns it, second has none.
10. Code block with language `python` between two commands — treated as prose.
11. Output body containing literal triple backticks.
12. Output body containing a sequence of 5 backticks — fence must be 6 backticks.
13. Front matter with unknown field — preserved on round-trip.
14. Info string with bare token (`out`, no `=value`) — attribute stored with `""` value.
15. Multi-line command body (heredocs, multi-line pipelines).

For each fixture: assert that `Serialize(Parse(src)) == src` byte-for-byte.

Additional rewriter tests:

- `SetOutput` on a command that already has output: output is replaced, surrounding bytes unchanged.
- `SetOutput` on a command that has no output: output is inserted with a blank line before it.
- `SetOutput` on a command followed by prose (orphan case): output is inserted between command and prose.
- `SetProse` replaces only the prose block's bytes.

### 8.2 Runner tests

- Echo a string, assert output and exit code 0.
- `false`, assert exit code 1.
- `cd /tmp && pwd` then `pwd` in a second `Run` — state persists.
- Long output (≥1 MiB + 1) — assert `Truncated == true` and output capped.
- `Interrupt` during `sleep 30` — assert command terminates within ~1s.
- Output containing the sentinel-like string but not the actual sentinel — must not confuse the parser (the random suffix prevents this in practice; test with a fixed sentinel for determinism).

### 8.3 Server smoke tests

- Start the server with a fixture notebook, GET `/`, assert HTML contains expected cell structure.
- POST `/run/:idx`, poll `/cell/:idx`, assert spinner → output transition.
- Assert `.md` file on disk is updated after a run.
- Assert second concurrent POST `/run/:idx` returns 409.

---

## 9. README contents

The README must include:

1. What it is, one paragraph.
2. The format spec (§2), condensed.
3. Install: `go install ./cmd/clinote`.
4. Usage: `clinote [path]`.
5. Limitations:
   - Single user, single notebook per server process.
   - External edits to the .md file during a session will be overwritten.
   - Interactive TUI commands (`vim`, `less`, etc.) will hang; use the interrupt button.
   - ANSI colour is a live-render nicety; reloaded notebooks show plain text.
   - Output capped at 1 MiB per cell.

---

## 10. FUTURE.md — explicitly deferred

These are intentionally not in v1. Do not implement.

- `$LAST_OUTPUT` env var / variable passing between cells.
- Sidecar files for large output (`.notebook-name/` directory).
- Cell IDs and stable cross-references.
- Cell tags: `secret`, `skip`, `timeout`, per-cell env overrides.
- Streaming output via SSE/WebSockets.
- Separating stdout and stderr.
- Binary output types (images, etc.).
- File watcher for external .md edits.
- Multi-language kernels.
- Headless / CI mode.
- Authentication, multi-user, collaboration.

---

## 11. Style and quality

- Standard Go formatting (`gofmt`, `go vet`, `staticcheck` clean).
- No external state — all state lives in memory or in the .md file.
- Minimal dependencies (see §3.4).
- No JS frameworks. HTMX + a small hand-written ANSI-to-HTML function + a small hand-written sortable-table function. No build step.
- Errors returned, not panicked. The server logs to stderr.
