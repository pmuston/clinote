# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What clinote is

A shell notebook: one `.md` file, cells are fenced ` ```sh ` blocks, and running a cell writes its result back into the same file. One interactive shell runs under a pty for the life of the server, so state carries between cells.

**This is v2, and it is a rewrite.** The format, parser, splice, run scheduling and browser UI belong to [notekit](https://github.com/pmuston/notekit); clinote is the shell executor plus a `main`. v1's engine — `internal/notebook`, `internal/runner`, `internal/render`, `internal/server` — was deleted in the v2 merge.

## Commands

```sh
make check         # build, vet, gofmt, test — what CI's build job runs
make conformance   # migrate the v1 corpus, judge it with notekit's notefmt
make linux         # run the checks in a Linux container
make help          # list targets

go test ./internal/migrate/ -run TestNoCellIsLost
go test ./cmd/clinote/ -run TestEndToEnd
```

`make linux` is not redundant with `make check`: the shell tests exercise every entry in `supportedShells` and **refuse to skip a shell they cannot find**. macOS has zsh and the Ubuntu CI runner does not, so a local pass can still fail on CI. Run it before pushing.

## Layout

```
cmd/clinote/main.go       flags, dispatch, kit bootstrap
cmd/clinote/shell.go      the pty executor — all pty knowledge lives here
cmd/clinote/migrate.go    the `migrate` subcommand and its reporting
internal/migrate/         v1 → notekit conversion, with v1's own fixtures as corpus
internal/version/         the single version constant; provenance derives from it
```

Everything else comes from `github.com/pmuston/notekit`: `doc` (format, parse, splice), `meta` (metadata grammar), `kind` (result rendering), `exec`/`run` (scheduling), `serve` (HTTP + HTMX), `notetool` (resolve, create, engine checks).

## The ideas that matter

- **Conformance is the contract with notekit, not shared code.** clinote may outgrow the kit's UI; it must never write a notebook the kit cannot read. CI's `conformance` job makes clinote write notebooks and has `notefmt` judge them. Never diverge on `doc` or `meta`.

- **A cell needs its own heading (level 2–6), and a section holds exactly one source fence.** A second fenced block in a section is silently prose — it never runs, and `notefmt` does not warn ([notekit#1](https://github.com/pmuston/notekit/issues/1)). This is the sharpest edge in the format and the reason migration counts cells.

- **Migration verifies itself by counting.** `internal/migrate.Convert` re-parses its own output and refuses to write when the cell count changed. Given the above, that count is the only thing between a heading mistake and a quietly gutted notebook. Do not weaken it.

- **Provenance is single-sourced.** `internal/version.Version` is the one constant; `Provenance()` derives the `tool=` string written into notebooks. They drifted once already (`0.1.7` in the constant while binaries stamped `clinote/2.0`), which is what the derivation prevents.

- **Results are volatile and whole.** A run replaces the entire result position. Success writes `output`, failure writes `error` with `status` — never `output` with a bad exit code.

- **stdout and stderr interleave.** v1 split them via a `2>` redirect and picked by exit code; the format has one result body, so the redirect is gone. Reinstating it means arguing the format should carry two streams.

## Conventions

- SQLite driver, if ever needed: `modernc.org/sqlite`, not `mattn/go-sqlite3`.
- `gofmt`, `go vet`, `staticcheck` clean.
- Errors returned, not panicked.
- `cmd/clinote/*.go` carries `//go:build unix` — the pty executor is Unix-only, with `unsupported.go` covering the rest.

## Deliberately absent

`dur=` is the only v1 feature still missing. It needs a reserved key in notekit's §6, which is a format change and a bigger question than a clinote one.

Everything else on that list came back **upstream**, in notekit's `serve` and `kind`, where sqlnote gets it too: `width: full`, `editable:`, `local-files:`, `requires:`, run-below, the format picker, and `tsv`. That is the pattern to follow — when a v1 feature is missing, first ask whether it is generic. Almost all of them were.

Two things worth knowing when working on this:

- **A tool must not hard-code what the kit knows.** `tsv` was documented in four clinote files while `cmd/clinote/shell.go` matched `case kind.CSV, kind.JSONL`, so the branch that builds a table payload was never reached. The executor now asks `kind.NewRegistry().LookupFormat`, and the UI builds its dropdown from `Registry.Formats()`. Neither should ever go back to a literal list.
- **Run all and Run below do not stop at the first failure.** The cells are submitted to a scheduler that serialises them, so the rest are queued by the time one fails. v1 halted the batch. Changing this means a batch concept in notekit's `run` and a change to what run-all already does for every tool — an open decision, not an oversight.

## Historical

[clinote-spec.md](clinote-spec.md) is the **v1** contract, kept for archaeology. It describes a format and an architecture this repository no longer implements — do not treat it as current.
