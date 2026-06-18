# clinote user guide

A walk-through of clinote from first install to power-user patterns. For a one-paragraph overview see the project [README](../README.md); for the v1 contract see [clinote-spec.md](../clinote-spec.md).

## Contents

- [What clinote is for](#what-clinote-is-for)
- [Install](#install)
- [Your first notebook](#your-first-notebook)
- [Anatomy of a notebook file](#anatomy-of-a-notebook-file)
- [Working in the browser](#working-in-the-browser)
  - [Running cells](#running-cells)
  - [The persistent shell](#the-persistent-shell)
  - [Editing prose](#editing-prose)
  - [Editing sh cells (`editable: true`)](#editing-sh-cells-editable-true)
  - [Adding cells](#adding-cells)
  - [Deleting blocks](#deleting-blocks)
  - [Output format picker](#output-format-picker)
  - [Interrupting a run](#interrupting-a-run)
- [Output types](#output-types)
  - [text and ANSI](#text-and-ansi)
  - [csv](#csv)
  - [tsv](#tsv)
  - [jsonl](#jsonl)
- [stdout vs stderr](#stdout-vs-stderr)
- [Front-matter reference](#front-matter-reference)
- [CLI reference](#cli-reference)
- [Limitations and gotchas](#limitations-and-gotchas)

## What clinote is for

clinote is a personal lab notebook for shell commands. One `.md` file = one notebook. Open it in your browser, run cells, and the output gets spliced back into the same `.md` file as adjacent fenced blocks.

The file stays plain CommonMark — readable, grep-able, and renders correctly on GitHub. The browser is just a convenient way to interact with it. There's no database, no proprietary format, no lock-in.

It's good for:

- Reproducing a one-off investigation later (the commands and their outputs live together).
- Iterating on a shell pipeline while keeping previous attempts visible.
- Lightweight runbooks where each step is a runnable cell.
- Sharing a "here's how I did it" with a colleague (just send the `.md`).

It's not for:

- CI / headless execution. There is no batch mode.
- Multi-user collaboration. Single user, single notebook per server process.
- Interactive TUI applications (`vim`, `htop`, `less`). They will hang the cell — use the **Interrupt** button to recover.

## Install

```sh
go install github.com/pmuston/clinote/cmd/clinote@latest
```

Requires Go 1.25+. The binary lands in `$GOBIN` (or `$GOPATH/bin`).

If the repo is private, set:

```sh
go env -w GOPRIVATE=github.com/pmuston/*
git config --global url."git@github.com:".insteadOf "https://github.com/"
go install github.com/pmuston/clinote/cmd/clinote@latest
```

To install from a local clone (typical during development):

```sh
git clone <repo>
cd clinote
go install ./cmd/clinote
```

## Your first notebook

The fastest way to start:

```sh
clinote new my-notebook.md
```

This scaffolds a starter notebook with sensible defaults and opens it in your browser:

```yaml
---
title: My notebook
created: 2026-05-26T14:15:06Z
shell: bash
editable: true
width: full
---

# My notebook

```sh
echo "hello from clinote"
```
```

Click **Run** on the example cell. The output gets spliced into the file as a paired `output` block. Look at the file on disk — it now contains:

````markdown
```sh
echo "hello from clinote"
```

```output type=text exit=0 ran=... dur=1ms
hello from clinote
```
````

That's the whole loop: command goes in, output comes out, both stay in the file.

## Anatomy of a notebook file

A notebook is plain markdown with optional YAML front matter. Three kinds of things have semantic meaning to clinote:

**Prose** — any markdown that isn't a code block. Headings, paragraphs, lists, links. Rendered with goldmark when displayed.

**Command cells** — fenced code blocks tagged `sh`:

````markdown
```sh
du -sh /var/* | sort -h | tail
```
````

The body is sent verbatim to the persistent shell. Multi-line bodies, heredocs, and pipelines all work.

**Output cells** — fenced code blocks tagged `output`, written by clinote when a command completes:

````markdown
```output type=text exit=0 ran=2026-05-26T14:31:12Z dur=1.2s
4.0K    /var/games
2.1G    /var/log
```
````

Attributes:
- `type` — `text`, `csv`, `tsv`, or `jsonl` (drives the renderer).
- `exit` — exit code.
- `ran` — start timestamp (RFC 3339, UTC).
- `dur` — wall-clock duration.
- `truncated=true` — present only if the output hit the 1 MiB cap.

**Pairing rule.** An output block belongs to the command block above it iff only whitespace separates them. Any intervening prose breaks the pairing — clinote marks the command as unrun and shows the output as orphaned. There are no IDs or cross-references; pairing is strictly positional.

Anything else in the file (headings, links, lists, code blocks in other languages like `python` or `json`) is just prose.

## Working in the browser

### Running cells

Each command cell has a **Run** button. Click it, see the spinner, get the output. Behind the scenes clinote POSTs `/run/:idx`, starts the command in a goroutine, and the browser polls `/cell/:idx` every 500ms via HTMX until the output is ready.

While a run is in flight, an **Interrupt** button appears at the top right. The save-status indicator changes from "saved" to "running…". Only one Run is in flight at a time — kicking off a second Run while one is running returns 409.

### The persistent shell

clinote runs one `bash -i` (or `zsh -i`) under a pty for the entire lifetime of the server process. **State flows between cells.** Anything you change in one cell is still set in the next:

````markdown
```sh
cd /var/log
```

```sh
pwd
```
````

The second cell prints `/var/log` because the `cd` happened in the same shell session.

This works for:
- Working directory (`cd`).
- Environment variables (`export FOO=bar`).
- Shell functions (`f() { echo $1; }`).
- Aliases.
- `set` options.

The session ends when you close clinote.

### Editing prose

Hover over any prose paragraph and an **edit** button appears in the top right. Click it; the paragraph swaps to a textarea with the raw markdown. Type, click **save**, and the file is updated immediately.

This works without any front-matter flag. Prose editing is always available.

### Editing sh cells (`editable: true`)

By default, command cells are read-only in the UI (the spec's safe default for shared / demo notebooks). To unlock editing, add this to your front matter:

```yaml
---
editable: true
---
```

Each command cell now gets an **edit** button next to Run. Click → the cell swaps to a textarea with the command body. Type a new command, save. The file is rewritten; the next Run uses the new command.

`clinote new` adds this flag automatically because notebooks you're authoring are almost always ones you want to edit freely. Delete the line later if you want to lock the notebook down.

### Adding cells

At the bottom of the notebook there are two buttons:

- **+ sh cell** — appends a new `sh` cell. If `editable: true`, the new cell opens directly into the editor with an empty textarea — start typing immediately. Without the flag, the new cell appears in view mode (an empty `\`\`\`sh\n\`\`\``) and you'd need to edit the file externally to fill it in.
- **+ prose** — appends a new prose paragraph and opens its editor. Type your prose, save.

If you cancel the editor without saving:
- An empty sh cell remains (use **×** to remove it).
- A prose block containing `<!-- new -->` remains — invisible when rendered (it's an HTML comment), but you can also delete it with **×**.

### Deleting blocks

Every block — prose, command, orphan output — has an **×** button:

- **Prose** can always be deleted. A `window.confirm` dialog appears first.
- **Command cells** require `editable: true`. Deleting a command also removes its paired output block, so you don't end up with an orphan.
- **Orphan outputs** require `editable: true`.

Disabled while the cell is running.

### Output format picker

When `editable: true`, every command cell has a small dropdown next to **edit**:

```
[Run] [edit] [text ▾] [×]
                 ↳ csv
                 ↳ tsv
                 ↳ jsonl
```

Selecting a different format does two things atomically:

1. Updates the **command's** `out=` attribute (so future runs save with the new type).
2. Updates the **paired output block's** `type=` attribute (so the current render flips from `<pre>` to `<table>` or vice versa).

Use this when you ran a command, looked at the output, realised it was tabular, and want to reformat without re-running. Both ends of the cell stay consistent on disk.

Selecting `text` removes the `out=` attribute from the command entirely (since `text` is the default).

### Interrupting a run

When a cell is running, the **Interrupt** button in the header sends SIGINT to the foreground process group. Useful when:

- A command is taking longer than expected.
- A pipeline got stuck.
- A `sleep`, `cat`, or `tail -f` hangs the cell.

After interrupt, the cell's exit code typically becomes 130 (the conventional SIGINT exit) and the output block is saved with whatever was captured.

## Output types

### text and ANSI

The default. Anything that isn't tabular goes here. Output renders inside a `<pre>` block.

ANSI SGR escapes (16 colours, bold, underline) render as inline-styled HTML spans **on the first paint after a run**. The on-disk `.md` file always contains ANSI-stripped text, so a reload (or someone viewing the file on GitHub) shows plain text. Colour is a live-render nicety, not a storage format.

Cursor movement and screen-clear escapes (`\x1b[2J`, `\x1b[H`) are dropped — they don't translate to a static document.

### csv

````markdown
```sh out=csv
psql -c "select id, email, created_at from users" --csv
```
````

Output renders as a sortable HTML table:

- First row is the header.
- Click a column header to sort ascending; click again for descending.
- Numeric columns sort numerically (detected by attempting `parseFloat` on every value).
- Rows beyond 1000 are dropped from the rendered table with a "Showing 1000 of N" notice. The full data stays in the `.md` file.

Standard CSV quoting (double quotes, embedded commas, escaped quotes) is handled by Go's `encoding/csv`.

### tsv

Same as CSV but tab-separated. Useful for `awk`, `cut`, and SQL tools that emit tab-delimited output by default:

````markdown
```sh out=tsv
awk 'BEGIN{OFS="\t"} {print $1, $3, $5}' access.log
```
````

Commas inside cells are preserved as literal characters (they're not separators).

### jsonl

JSON Lines — one JSON object per line:

````markdown
```sh out=jsonl
kubectl get pods -o json | jq -c '.items[]'
```
````

Renders as a sortable table where:

- Columns are the union of top-level keys across all rows, sorted alphabetically.
- Cells for missing keys render empty.
- Nested objects and arrays render as their compact JSON string.

Rows that aren't valid JSON objects fall back to the text renderer.

### When to pick which

- **text** for human-readable command output: paths, log lines, status messages, anything you'd `cat` in a terminal.
- **csv** when the tool emits CSV: `psql --csv`, `mysql --batch`, most SQL clients.
- **tsv** for `awk`/`cut`/`grep -P`-style pipelines and SQL output that's tab-delimited.
- **jsonl** for tools that emit JSON-per-line: `kubectl ... | jq -c '.items[]'`, `gh api ... --jq '.[]'`, structured log files.

If you forget to set `out=` and the output looks tabular, click the format picker on the cell (requires `editable: true`). Both the cell's hint and the output's type update together.

## stdout vs stderr

clinote captures stdout and stderr **separately**:

- The shell-level redirect `{ command\n} 2> /tmp/clinote-stderr-...` sends stderr to a per-run temp file.
- Stdout still flows through the pty as the captured output.

When the command finishes, the server picks one stream to save:

- **exit = 0** → stdout (stderr is treated as noise: progress bars, warnings, `time(1)` output, etc.).
- **exit ≠ 0** → stderr (the error message — what you almost always want to see when something broke).
- **exit ≠ 0 with empty stderr** → fallback to stdout (e.g., `false` produces nothing on either stream).

Examples:

```sh
echo "good"; echo "warning" >&2          # exit=0 → output is "good"
echo "noise" >&2; echo "result"          # exit=0 → output is "result"
ls /nonexistent                          # exit≠0 → output is "ls: ... No such file"
```

If you want both streams in the output, redirect explicitly:

```sh
my-tool 2>&1
```

This merges stderr into stdout at the shell level, so clinote sees one stream and saves it normally.

This is a deliberate departure from the v1 spec (which merged the streams). The format picker doesn't change which stream was saved — that's decided at run time by the exit code.

## Front-matter reference

```yaml
---
title: Disk usage investigation     # string, free-form
created: 2026-05-26T14:30:00Z       # RFC 3339 timestamp
shell: bash                         # bash | zsh; default bash
editable: true                      # unlock sh-cell editing + delete + format picker
width: full                         # use full window width; default narrow column
---
```

Unknown fields are preserved on save — feel free to add your own (`tags:`, `owner:`, etc.).

## CLI reference

```sh
clinote [flags] path/to/notebook.md       # open an existing notebook
clinote new [flags] <path>                # create a notebook (refuses to overwrite)
clinote                                    # list .md files in cwd
```

Flags:

- `--no-browser` — print the URL but don't auto-open the browser.

Environment:

- `BROWSER` — set to `none`, `false`, `0`, or empty to suppress browser launch. Otherwise the system default is used (`open` on macOS, `xdg-open` on Linux, `start` on Windows).

The server binds to `127.0.0.1:0` (free port) and prints the URL to stdout. Press `Ctrl+C` to shut down.

## Limitations and gotchas

**The `.md` file is the source of truth on disk, but the server's in-memory copy is the source of truth during a session.** External edits to the file while clinote is running will be overwritten on the next save. Workflow:

1. Stop clinote (Ctrl+C).
2. Edit the file in your text editor.
3. Restart `clinote path/to/notebook.md`.

A file watcher would solve this — it's in the explicit FUTURE.md list and not implemented in v1.

**`exit N` terminates the persistent shell.** Use `return N` (inside a function), `false`, or `( ... ; exit N )` (in a subshell) if you need a non-zero status without killing the session. If the shell dies mid-session, all subsequent Run requests will fail until you restart clinote.

**Output capped at 1 MiB per cell.** Commands that produce more keep running — the excess is dropped (not buffered) and `truncated=true` is recorded on the output block. Useful to know when piping `find /`, `journalctl`, etc.

**Interactive TUI applications hang.** `vim`, `less`, `htop`, anything that draws its own UI to the terminal. The cell will run forever (or until you click **Interrupt**). For paging, use `cat`, `head`, `tail` instead.

**ANSI colour is live-only.** The `.md` file is plain text. After a reload, output renders in plain. Colour only appears in the brief window between "run completes" and "the next time you reload the page".

**zsh quirk: `setopt promptcr`.** Some zsh configurations emit `\r` before prompts, which can occasionally surface as stray carriage returns in output. Usually invisible; if you see weirdness, try `shell: bash` in your front matter.

**Sortable tables cap at 1000 displayed rows.** The full data is in the file; the browser just renders a manageable subset.

**No undo.** Edits and deletes are immediate and persistent. The `.md` file is your safety net — keep it in git if you care about history.

---

That's the full tour. The spec ([clinote-spec.md](../clinote-spec.md)) is authoritative if anything here is ambiguous. Open an issue if something doesn't match what you observe.
