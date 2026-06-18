# clinote

A personal lab notebook for shell commands. One markdown file is one notebook; commands and their outputs live together as plain CommonMark — readable, grep-able, and renders correctly on GitHub.

## Purpose

Capture an investigation, runbook, or quick experiment as a sequence of runnable shell cells with their outputs baked in. The browser is just a convenient runner — the `.md` file is the artifact. Send it to a colleague, commit it to git, open it next month: it's all still there.

## Features

- **Persistent shell session per notebook** — one `bash -i` or `zsh -i` lives for the lifetime of the server; `cd`, env vars, and shell functions flow between cells.
- **Output spliced back into the file** — runs the cell, captures the output, writes it as an adjacent ` ```output ``` ` block. The on-disk file always reflects the current state.
- **Round-trip safe parser** — `Serialize(Parse(src)) == src` byte-for-byte for every accepted input; your file isn't reformatted behind your back.
- **Typed output rendering** — `out=csv` / `out=tsv` / `out=jsonl` on a cell turns the output into a sortable HTML table; default `text` gets ANSI-colour rendering on the first paint.
- **stdout vs stderr by exit code** — `exit=0` saves stdout; `exit≠0` saves stderr (the error message). `2>&1` escape hatch if you want both.
- **In-browser authoring** (opt-in via `editable: true` in front matter):
  - Add `+ sh cell` / `+ prose` — new blocks open straight into an editor.
  - Edit command bodies inline.
  - Delete any block (`×`); deleting a cell also removes its paired output.
  - Format picker — change a cell's `out=` and the saved output's `type=` together, after the fact.
- **Always-on prose editing** — hover, click _edit_, change the markdown, save.
- **Interrupt button** — `SIGINT` to the foreground process group when a cell hangs.
- **Wide layout option** — `width: full` in front matter for log-heavy or table-heavy notebooks.
- **`clinote new <path>`** — scaffolds a starter notebook with sensible defaults.
- **Single binary, no JS framework, no build step** — Go binary, embedded HTMX, hand-written sortable table and ANSI-to-HTML.

## Non-goals (intentionally not built)

Multi-user / collaboration, CI / headless execution, streaming output, file watcher for external edits, interactive TUI apps, multiple language kernels.

## At a glance

```sh
go install github.com/pmuston/clinote/cmd/clinote@latest
clinote new notebook.md
# → opens the browser; click Run on the example cell; output appears in the file
```

For the full tour, see [user-guide.md](user-guide.md).
