# clinote

A personal lab notebook for shell commands. One markdown file = one notebook. A persistent shell session is bound to the notebook for the lifetime of the server process — `cd`, env vars, and shell functions flow between cells. Commands run from fenced code cells; outputs are captured back into the same markdown file as adjacent fenced blocks.

The on-disk file stays plain CommonMark — readable, grep-able, GitHub-renderable. Parsing then re-serialising a notebook without edits produces byte-identical output.

> For a tour from install to power-user patterns, read the [user guide](docs/user-guide.md).

## Install

### Homebrew (macOS / Linux)

```sh
brew tap pmuston/tap
brew install pmuston/tap/clinote
```

Upgrade later with `brew upgrade pmuston/tap/clinote`.

### From source (Go)

```sh
go install github.com/pmuston/clinote/cmd/clinote@latest
```

Requires Go 1.25+. Use this if you don't have Homebrew or want to track `main`.

## Usage

```sh
clinote path/to/notebook.md         # open a notebook
clinote new path/to/notebook.md     # create a notebook (with starter content) and open it
clinote                             # list .md files in cwd
clinote --no-browser notes.md       # don't auto-open the browser
```

The server binds to `127.0.0.1` on a free port and prints the URL to stdout. The browser opens automatically unless `BROWSER` is empty / `none` / `false` / `0`, or `--no-browser` is passed.

`clinote new` refuses to overwrite an existing file. The title in the scaffolded front matter is derived from the filename (e.g. `disk-usage.md` → `Disk usage`).

## File format

A notebook is a UTF-8 markdown file with optional YAML front matter and any mix of prose, command cells, and output cells.

### Front matter

```yaml
---
title: Disk usage investigation
created: 2026-05-26T14:30:00Z
shell: bash
---
```

Recognised fields:

- `title` — string
- `created` — RFC 3339 timestamp
- `shell` — `bash` or `zsh` (default `bash`)
- `editable` — `true` unlocks in-browser editing of sh command bodies (default `false`)
- `width` — `full` to use the full window width for the notebook column (default is a narrow column suitable for prose reading)

Unknown fields are preserved on save.

### Command cells

Language tag `sh`. The body is sent verbatim to the persistent shell.

````markdown
```sh out=csv
psql -c "select * from users" --csv
```
````

`out=text|csv|jsonl` hints the renderer; if absent, the output type is sniffed.

### Output cells

Written by the tool, language tag `output`. Required attributes: `type`, `exit`, `ran`, `dur`. Optional: `truncated=true`.

````markdown
```output type=text exit=0 ran=2026-05-26T14:31:12Z dur=120ms
4.0K    /var/games
2.1G    /var/log
```
````

### Pairing

An output block is paired with the command above it iff only whitespace separates them. Any intervening prose orphans the output and marks the command as unrun. There are no IDs or cross-references — pairing is strictly positional.

## Rendering

- **text** — wrapped in `<pre>`; ANSI SGR escapes (16 colours, bold, underline) render as inline-styled spans on the first paint after a run. The on-disk file always contains ANSI-stripped text, so reloads show plain.
- **csv** — sortable HTML table; click a header to sort (numeric columns detected automatically).
- **tsv** — same as CSV, tab-separated.
- **jsonl** — sortable HTML table with the union of top-level keys as columns (alphabetical); nested values render as compact JSON strings.

All table renderers cap displayed rows at 1000 with a "showing 1000 of N" notice. The full data stays in the `.md` file.

### Triggering table rendering

Add `out=csv`, `out=tsv`, or `out=jsonl` to the command's info string:

````markdown
```sh out=csv
psql -c "select * from users" --csv
```

```sh out=tsv
awk 'BEGIN{OFS="\t"} {print $1, $3, $5}' data.txt
```

```sh out=jsonl
kubectl get pods -o json | jq -c '.items[]'
```
````

When you run the cell, the output block is written with the matching `type=` and renders as a sortable table. Without the hint, output renders as plain text (the default).

If you forget the hint and the output looks tabular, you can hand-edit `type=text` in the output block to `type=csv` / `type=tsv` / `type=jsonl` and reload.

## Working in the browser

- **Run** — each command cell has a Run button. Output is spliced into the `.md` file when it completes.
- **Edit prose** — hover over a prose paragraph and click _edit_ to swap to a textarea. Save persists immediately.
- **Edit sh cells** — only when the notebook has `editable: true` in its YAML front matter. Each cell gets an _edit_ button next to Run; click to swap to a textarea, type a new command, save. Without the flag, the edit button isn't shown and the endpoint returns 403 — the safe default for shared / demo notebooks.
- **Change output format** — only with `editable: true`. A dropdown next to the edit button lets you pick text / csv / tsv / jsonl after the fact. Selecting a value rewrites BOTH the command's `out=` attribute AND the existing output block's `type=`, so the on-disk file stays internally consistent and the next run will save with the new type automatically. Useful when you ran a command, saw the output was tabular, and want to reformat without re-running.
- **+ sh cell** / **+ prose** — buttons at the bottom of the notebook append a new block and open its editor immediately (empty textarea, focused). For sh cells this only works with `editable: true`; without the flag the new cell appears in view mode. Prose always opens in edit mode.
- **Delete (×)** — every block shows a delete button. Sh cells and orphan output blocks need `editable: true` to delete; prose can always be deleted. A confirmation dialog (`window.confirm`) appears before the deletion. Deleting an sh cell also removes its paired output block.
- **Interrupt** — visible top-right while a cell is running; sends SIGINT to the foreground process group.

## Output: stdout vs stderr

A command's stdout and stderr are captured separately. The saved output block contains:

- the command's **stdout** if it exited 0 (stderr noise like progress bars, warnings, or timing info is discarded);
- the command's **stderr** if it exited non-zero (the error message — what you almost always want to see when something failed);
- stdout as a fallback when exit≠0 but stderr is empty (e.g., `false`).

If you need *both* streams captured into the output, redirect explicitly: `cmd 2>&1`.

This is a deliberate departure from the v1 spec (which merged the streams). The `editable: true` format picker doesn't change which stream was saved — that's decided at run time by the exit code.

## Limitations

- Single user, single notebook per server process.
- `exit N` inside a cell will terminate the persistent shell. Use `return N` (inside a function) or `false` / `( ... ; exit N )` if you need a non-zero status without killing the session.
- The in-memory notebook is the source of truth during a session. External edits to the `.md` file while the server is running will be overwritten on next save.
- Interactive TUI commands (`vim`, `less`, `htop`) will hang the cell — use the **Interrupt** button to recover.
- ANSI colour is a live-render nicety; reloaded notebooks show plain text.
- Output is capped at 1 MiB per cell. Commands that produce more keep running; the excess is dropped and `truncated=true` is recorded.

## Building from source

```sh
go test ./...
go build ./cmd/clinote
./clinote path/to/notebook.md
```

Requires Go 1.25+. The dependencies are minimal: Echo (HTTP), goldmark (prose rendering), yaml.v3, creack/pty, x/sys.
