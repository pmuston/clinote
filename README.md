# clinote

A personal lab notebook for shell commands. One markdown file = one notebook. A persistent shell session is bound to the notebook for the lifetime of the server process — `cd`, env vars, and shell functions flow between cells. Commands run from fenced code cells; outputs are captured back into the same markdown file as adjacent fenced blocks.

The on-disk file stays plain CommonMark — readable, grep-able, GitHub-renderable. Parsing then re-serialising a notebook without edits produces byte-identical output.

## Install

```sh
go install github.com/pmuston/clinote/cmd/clinote@latest
```

## Usage

```sh
clinote path/to/notebook.md     # open a notebook
clinote                         # list .md files in cwd
clinote --no-browser notes.md   # don't auto-open the browser
```

The server binds to `127.0.0.1` on a free port and prints the URL to stdout. The browser opens automatically unless `BROWSER` is empty / `none` / `false` / `0`, or `--no-browser` is passed.

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

`title`, `created`, `shell` (`bash` or `zsh`) are recognised. Unknown fields are preserved on save.

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
- **jsonl** — sortable HTML table with the union of top-level keys as columns; nested values render as compact JSON strings.

Both table renderers cap displayed rows at 1000 with a "showing 1000 of N" notice. The full data stays in the `.md` file.

## Limitations

- Single user, single notebook per server process.
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
