# clinote

A personal lab notebook for shell commands. One markdown file is one notebook;
cells are fenced `sh` blocks, and running one writes its result back into the same
file. A single shell is bound to the notebook for the life of the server, so `cd`,
environment variables and shell functions carry from one cell to the next.

The file stays plain CommonMark — readable, grep-able, and correct on GitHub.

> **v2 is a rewrite on [notekit](https://github.com/pmuston/notekit).** The format,
> parser, run scheduling and browser UI now belong to that library; clinote is the
> shell executor. **v2 cannot open a v1 notebook** — see
> [Migrating from v1](#migrating-from-v1).

For a tour, read the [user guide](docs/user-guide.md).

## Install

```sh
brew tap pmuston/tap
brew trust pmuston/tap      # required for third-party taps
brew install pmuston/tap/clinote
```

Or from source, requiring Go 1.25+:

```sh
go install github.com/pmuston/clinote/cmd/clinote@latest
```

## Usage

```sh
clinote notebook.md                  # serve a notebook
clinote                              # use the notebook in the current directory
clinote new notebook.md              # create one, then serve it
clinote migrate old.md               # convert a v1 notebook
clinote version
```

| Flag | Meaning |
|---|---|
| `-addr` | Address to listen on. Default `127.0.0.1:8080`. |
| `-shell` | `bash` or `zsh`. Defaults to your `$SHELL` when it is one of those. |
| `-term` | `TERM` for the shell. Default `dumb`; a real value lets tools auto-detect colour. |
| `-poll` | How often the browser polls a running cell. Default `500ms`. |
| `-list` | List candidate notebooks and exit. |
| `-allow-local-files` | Serve files from the notebook's directory so its image links resolve. Off by default. |

clinote prints its URL and waits; **it does not open a browser** (v1 did). Ctrl-C
stops it.

In the browser each cell offers **Run**, **Run below** (this cell and everything
after it — for a pipeline whose first stage is expensive), a **format dropdown**,
and move/delete. **Interrupt** sends SIGINT to a running command, which is how you
recover a hung cell without stopping the server.

## The notebook format

The format is notekit's, and its
[specification](https://github.com/pmuston/notekit/blob/main/notekit-format-spec.md)
is the authority. In short:

````markdown
---
notekit: 1
title: Disk usage
notekit-tool: clinote
---

## Largest directories

```sh {format=csv}
du -d1 -h | sort -hr | head -5
```

```output {format=csv, run="2026-07-16T09:41:07Z", tool="clinote/2.2"}
size,path
1.2G,./data
```
````

Two rules catch people out:

- **A cell needs its own heading**, level 2–6. A section holds exactly one source
  fence; a second fence in the same section is prose, silently
  ([notekit#1](https://github.com/pmuston/notekit/issues/1)).
- **Failures are `error` blocks**, not output with a bad exit code:
  ` ```error {status=127, …} `.

Declare a result kind with `{format=csv}`, `{format=tsv}` or `{format=jsonl}` on
the cell, or pick one from the dropdown in the browser; tables render sortable and
stay plain text on disk.

Four front-matter keys affect the reader:

| Key | Effect |
|---|---|
| `width: full` | Use the whole window instead of a reading column. |
| `editable: false` | Withhold editing. **Never** gates running — a notebook handed out to be worked through still runs. A guard rail, not a control: anyone can edit the file in their editor. |
| `local-files: true` | Declare that the notebook shows files from its directory. Requests only — `-allow-local-files` is what grants it. |
| `requires: [NAME, …]` | Name the environment variables the notebook needs. Reports which are unset; never blocks. Write it **inline** — a YAML block list is invisible to the reader. |

## Migrating from v1

v1 notebooks have no `notekit: 1` marker and no per-cell headings, so v2 refuses
them and points here:

```sh
clinote migrate notebooks/s88.md      # writes notebooks/s88.v2.md
clinote migrate -dry-run notebooks/*.md
clinote migrate -in-place notes.md    # keeps notes.md.v1.bak
```

Migration reports what it did and what could not come across, and **refuses to
write if the cell count changed** — a stranded fence becomes prose silently, so
counting is the only guard against a quietly gutted notebook.

What changes: every cell gains a heading (invented from the command when there is
none — rename freely, it costs nothing); failed results become `error` blocks;
`dur=` is dropped, having no home in the format. Results keep their original
timestamp and are marked `tool="clinote/1"`, because that is what produced them.

## Output: stdout and stderr

Both streams interleave as produced, exactly as in a terminal. A cell that exits
zero writes an `output` block; a non-zero exit writes an `error` block carrying
the status.

v1 captured the two separately and picked one by exit code, suppressing stderr on
success. The notekit format has one result body, so that distinction is gone —
`cmd 2>/dev/null` if you want a noisy command quietened.

## Not in v2 yet

Present in v1, still absent. As of 2.2.0 this list is down to one entry, the rest
having gone upstream into notekit's `serve` where sqlnote gets them too:

- **`dur=`** — the format records `run` and `tool`, not elapsed time. Adding it
  means a new reserved key in notekit's §6, which is a format change rather than a
  clinote one.

One behaviour differs from v1 rather than being missing: **Run all and Run below do
not stop at the first failure.** notekit submits the cells to a scheduler that
serialises them, so by the time one fails the rest are already queued. v1 halted the
batch on a non-zero exit.

## Building

```sh
make check         # build, vet, gofmt, test
make conformance   # migrate the v1 corpus, judge it with notekit's notefmt
make linux         # run the checks in a container
```

`make linux` is worth running before pushing: the shell tests exercise every
supported shell and refuse to skip a missing one, so a macOS pass can still fail
on CI where zsh is absent.

## Limitations

- Single user, one notebook per server process.
- The in-memory notebook is authoritative; external edits are overwritten on save.
- Interactive TUI commands (`vim`, `less`, `htop`) hang the cell — use Interrupt.
- Cells run under a pty, so tools see a terminal and draw progress bars. Single-line
  redraws are replayed, so the file keeps the final line; multi-line ones are not.
  `export CI=1` in a setup cell, a tool's own `--progress=plain`, or `| cat` stops
  them being drawn at all.
- Output is capped per cell; the excess is dropped and marked `truncated`.
- `exit N` in a cell terminates the persistent shell; use `false` or a subshell.
- ANSI colour renders live only; the file keeps plain text.
