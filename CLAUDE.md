# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What clinote is

A single Go binary that opens a `.md` notebook in a browser-based UI. Each notebook is bound to one persistent shell session for the lifetime of the server process. Cells (fenced ` ```sh ` blocks) are executed; their outputs are spliced back into the same `.md` file as adjacent ` ```output ` blocks. The on-disk file stays plain CommonMark — readable, grep-able, GitHub-renderable.

The v1 spec ([clinote-spec.md](clinote-spec.md)) is authoritative. Anything not specified is out of scope, and the FUTURE.md items in §10 must not be implemented.

## Commands

```sh
go test ./...                 # all package tests
go test -race ./...           # race detector
go vet ./...                  # lint
go build ./cmd/clinote        # produce ./clinote
./clinote path/to/notebook.md # run

# Run a single test
go test ./internal/notebook/ -run TestRoundTrip
go test ./internal/runner/ -run TestInterruptTerminatesSleep -v
```

## Architecture (the load-bearing ideas)

These are the cross-cutting invariants that span multiple packages:

- **Round-trip byte-identity is the parser's hardest constraint.** `Serialize(Parse(src)) == src` byte-for-byte must hold for every accepted input. The implementation keeps `Source` immutable and applies mutations by rebuilding bytes via splice + re-parse — see [parse.go](internal/notebook/parse.go) and [rewrite.go](internal/notebook/rewrite.go). All 15 spec fixtures live in [testdata/](internal/notebook/testdata/). Unknown front-matter fields and unknown info-string attributes survive round-trip because they're inside the byte slice we don't touch.

- **Cell pairing is strictly positional, no IDs.** An `output` block belongs to the preceding `sh` block iff only whitespace separates them. Any intervening prose orphans the output. Whitespace-only gaps between blocks are NOT emitted as ProseBlocks — they're invisible to the block list but preserved in Source. This rule governs `SetOutput`'s replace-vs-insert decision and the server's view-layer pairing in [view.go](internal/server/view.go).

- **Persistent shell via pty + sentinels.** The runner spawns one `bash -i` / `zsh -i` under a pty and keeps it alive for the session's lifetime. Each `Run` writes `command + printf '\n<sentinel>:%d\n' "$?"` and reads pty until the sentinel arrives. State (cwd, env, functions) persists between cells because the shell does. Output buffer caps at 1 MiB; a separate 8 KB rolling tail guarantees sentinel detection even when the body is truncated. Only one `Run` at a time (mutex); `Interrupt()` sends SIGINT to the foreground pgrp via `TIOCGPGRP`. See [runner.go](internal/runner/runner.go).

- **No streaming.** Output is async with HTMX polling, not SSE/WebSockets. The spinner element polls `GET /cell/:idx` every 500ms; the endpoint returns spinner-or-output depending on run state. When done, the response omits `hx-trigger` and polling stops.

- **In-memory state is the source of truth during a session.** The notebook is parsed from disk at startup. After every mutation (SetOutput, SetProse), the notebook is re-parsed from the newly-spliced bytes — so `nb.Blocks` always reflects the current state. External edits to the file mid-session are overwritten on next save; documented in the README, not solved.

- **Output is ANSI-stripped on disk.** The `.md` file is plain text (the runner strips ANSI before returning). The server stores the post-strip bytes in a `liveANSI` map for one-shot live render before falling back to the on-disk version. (A future change could route raw bytes through to the renderer for true coloured live render.)

## Package layout

```
cmd/clinote/main.go         entrypoint: arg parsing, listener bind, signal handling
internal/notebook/          parser + rewriter + 15-fixture round-trip suite
internal/runner/            persistent pty shell + sentinel-based Run
internal/server/            Echo handlers + html/template + embedded assets
internal/render/            CSV/JSONL/text rendering for the browser, including hand-written SGR-to-HTML
```

Templates and static assets live under `internal/server/templates/` and `internal/server/static/` so `go:embed` can pick them up. There is no `web/` directory (the spec suggested one, but embed-friendly placement won out).

## Conventions

- SQLite driver (if ever needed): `modernc.org/sqlite`, not `mattn/go-sqlite3`. Pure Go, no CGO.
- `gofmt`, `go vet`, `staticcheck` clean.
- Errors returned, not panicked. Server logs to stderr.
- After a notebook mutation, indices in `nb.Blocks` may shift. For `SetOutput`, the cmd index stays put (output inserts go *after*); the server uses the cmd index for polling and that index remains valid.

## Implementation notes worth keeping in mind

- **The parser is a line-based fence scanner, not goldmark.** Goldmark stays in the dependency list for rendering prose blocks in the server (see `proseUnit` in [view.go](internal/server/view.go)), but the spec's structural scan is easier and more controllable with a small custom scanner.
- **Close needs to be aggressive.** The shell may not exit on `\nexit\n` in all configurations; the runner closes the pty first (shell sees EOF), then Kill, then Wait with a 2s ceiling to prevent test hangs.
- **JSONL sniffing requires `{...}` rows.** A column of bare numbers is valid JSON but not tabular; we sniff to "text" in that case so the JSONL renderer doesn't have to fall back.
