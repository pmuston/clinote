# clinote user guide

A walk-through of clinote from first install to power-user patterns. For a one-paragraph overview see the project [README](../README.md); for the v1 contract see [clinote-spec.md](../clinote-spec.md).

## Contents

- [What clinote is for](#what-clinote-is-for)
- [Install](#install)
- [Your first notebook](#your-first-notebook)
- [Anatomy of a notebook file](#anatomy-of-a-notebook-file)
- [Working in the browser](#working-in-the-browser)
  - [Running cells](#running-cells)
  - [Running a whole pipeline](#running-a-whole-pipeline)
  - [The persistent shell](#the-persistent-shell)
  - [Defining macros and reusable functions](#defining-macros-and-reusable-functions)
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
- [Secrets and required environment](#secrets-and-required-environment)
- [Images and other files](#images-and-other-files)
- [stdout vs stderr](#stdout-vs-stderr)
- [Worked example: a graph-to-diagram pipeline](#worked-example-a-graph-to-diagram-pipeline)
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

### Homebrew (macOS / Linux)

```sh
brew tap pmuston/tap
brew trust pmuston/tap      # required for third-party taps
brew install pmuston/tap/clinote
```

You get a pre-built static binary — no Go toolchain needed. Recent Homebrew
refuses to install from an untrusted third-party tap, and the error it prints
doesn't make the fix obvious, hence the `brew trust` line.

Upgrade later with `brew upgrade pmuston/tap/clinote`.

### From source

```sh
go install github.com/pmuston/clinote/cmd/clinote@latest
```

Requires Go 1.25+. The binary lands in `$GOBIN` (or `$GOPATH/bin`). Use this if
you don't have Homebrew or want to track `main`.

From a local clone (typical during development):

```sh
git clone https://github.com/pmuston/clinote
cd clinote
go install ./cmd/clinote
```

Check which build you're running at any time:

```sh
clinote version
# → clinote v0.1.0 (a1b2c3d4e5f6)
```

The revision is the commit the binary was built from. A `, modified` suffix
means it was built from a dirty working tree — expected for local builds, but
never for a released one.

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

### Running a whole pipeline

**Run all**, in the header, runs every cell from the top. **run ↓** on any cell
runs that cell and everything below it. Both ask for confirmation first, naming
how many cells will run.

Both **stop at the first cell that exits non-zero**. This isn't configurable,
and the reason is that notebooks are usually chains: carrying on past a failed
stage runs later cells against stale or missing inputs, which tends to produce
output that looks plausible and is wrong — worse than an error. If a particular
cell should survive failure, say so in the shell:

```sh
optional-step || true
```

**Interrupt** aborts the batch as well as killing the current cell. Continuing
to the next stage after you deliberately interrupted one would defeat the point.

While a batch runs, a progress bar shows which cell is going, and the per-cell
Run, edit, delete and format controls are disabled — the batch owns the shell,
and a single run attempted alongside it is refused.

**run ↓ is usually the one you want** while iterating. In a pipeline whose first
stage is an expensive query, re-running from stage two lets you tune later
stages against the cached intermediate without hitting the database again.

There's no staleness detection — clinote has no idea which cells consume which
files, and guessing from timestamps would be wrong in exactly the cases that
matter. If you want dependency-driven rebuilds, that's what `make` is for, and
a cell can run it.

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

### Defining macros and reusable functions

Because the shell persists, you can define shell functions once and call them from any later cell. This is the idiomatic way to make short "macros" for repetitive commands — no special clinote feature needed.

Put your definitions in the **first cell** of the notebook. A single cell can hold as many statements as you like:

````markdown
```sh
# Post a Cypher query to a local service:
cy() { curl -s -XPOST localhost:8080/cypher -d "$1"; echo; }

# Pretty-print JSON from a URL:
j() { curl -s "$1" | jq .; }

echo "macros loaded"
```
````

Run that cell once at the start of your session. Every cell below can now call `cy 'MATCH (n) RETURN n'` or `j http://localhost:8080/status`.

Two reasons to keep macros in a visible first cell rather than hiding them in config:

- **You can read them before you run them.** Opening a notebook doesn't execute anything until you click Run, so a definitions cell is auditable — you see exactly what will run (no surprise `rm -rf`). The trade-off of the self-contained `.md` is that macros travel with the file; keeping them in a cell you eyeball first keeps that safe.
- **They travel with the notebook.** Send the `.md` to a colleague and the macros come with it. (Functions in your `~/.bashrc` / `~/.zshrc` are also available in cells — clinote spawns an interactive shell, so it sources them like a normal terminal — but those don't travel with the file.)

#### Prefer functions over aliases

Use **functions**, not aliases, for macros. Two reasons:

1. Functions take arguments (`cy "$query"`); aliases don't, cleanly.
2. Aliases have a subtle gotcha in clinote. Each cell body is wrapped in a `{ … }` group (so clinote can capture stderr for the whole cell), and bash expands aliases at *parse* time — before the group runs. So an alias **defined in a cell can't be used later in that same cell**:

   ````markdown
   ```sh
   alias hi='echo HEY'
   hi
   ```
   ````

   …fails with `bash: hi: command not found`. The alias *is* defined once the cell finishes, so a **later** cell can use it — but that split behavior is confusing. Functions don't have this problem: they resolve at execution time and work whether you call them in the same cell or a later one.

If you re-run the definitions cell after restarting clinote, the shell is fresh, so the macros need to be re-loaded — that's the one manual step. It's also the safety feature: nothing runs until you choose to run it.

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

## Secrets and required environment

Notebooks that hit a database or API need credentials, and the one thing you
must not do is set them in a cell:

```sh
export NEO4J_PW=hunter2      # DON'T — this text is written into the .md file
```

Cell bodies are saved to disk verbatim, so that password is now in a file you
might commit or send to a colleague. Instead export it in your shell **before**
launching clinote:

```sh
export NEO4J_PW=…
clinote notebooks/jacket-loop.md
```

The runner inherits the environment clinote was started with, so every cell can
use `"$NEO4J_PW"` while the value never touches the notebook.

There is no `secret` cell tag — redaction is not implemented, so nothing will
scrub a credential that reaches an output block. Be wary of tools that echo
their own arguments on failure; where a tool can read a password from the
environment directly, prefer that to passing it on the command line.

### Declaring what a notebook needs

List required variables in the front matter:

```yaml
---
title: Jacket loop — R101_JKT
requires:
  - NEO4J_PW
---
```

If any are unset or empty, clinote shows a banner naming them. It is a report,
not a gate: cells still run, so you can open a notebook to read it without
having its credentials to hand.

This is deliberately a list of names and nothing more. It cannot run anything
when a notebook is opened — the property that matters if someone sends you one.

It doubles as documentation: a colleague opening the notebook learns what it
needs without hunting for the first cell that references a variable.

The check reads clinote's own environment. A variable exported only in your
`~/.bashrc` is visible to cells (the shell sources it) but still reported
missing here — exporting before launch, the usual case, reports accurately.

### Failing loudly in the notebook

The banner warns before you start; a guard cell stops the run if you started
anyway. Put this first:

```sh
: "${NEO4J_PW:?export it before launching clinote}"
echo "NEO4J_PW set (${#NEO4J_PW} chars)"
```

Unset, the cell exits non-zero and shows
`bash: NEO4J_PW: export it before launching clinote`. Set, you get
`NEO4J_PW set (17 chars)`.

The length matters more than it looks: it distinguishes a properly-set variable
from an empty one or a leftover placeholder, which otherwise surface as a
confusing authentication failure several cells later. `${#VAR}` reveals the
length only, never the value.

This form is safe in clinote specifically because the shell is interactive —
`${VAR:?}` would terminate a non-interactive script, but here it just fails the
cell and leaves your session intact.

## Images and other files

Files sitting next to the notebook are served by clinote, so an ordinary
markdown image link works in the browser exactly as it does on GitHub:

````markdown
```sh
mytool --format svg --out chart.svg
```

![chart](chart.svg)
````

Run the cell to produce `chart.svg`, and the image below it renders. Nothing
about the file format changes — that's plain CommonMark, so GitHub shows the
chart too. Relative subdirectories work as well (`![](img/chart.svg)`).

This suits SVG particularly well: the notebook stays small and greppable, the
`.md` diff stays readable when you regenerate a chart, and there's no 1 MiB
output cap to worry about. It works for PNG or anything else your tools emit.

The trade-off is that the notebook is no longer a single file — moving it means
moving its images too.

**Scope and safety.** Only the notebook's own directory is served. Paths that
try to climb above it are refused, including percent-encoded attempts and
symlinks pointing elsewhere; so are dotfiles, which keeps `.git/` and `.env`
out of reach if a notebook happens to live in a repo root. Served files carry
`Content-Security-Policy: sandbox`, so an SVG containing `<script>` stays inert
even if you navigate straight to its URL — opening a notebook someone sent you
never executes anything.

## stdout vs stderr

clinote captures stdout and stderr **separately**:

- The shell-level redirect `{ command\n} 2> /tmp/clinote-stderr-...` sends stderr to a per-run temp file.
- Stdout still flows through the pty as the captured output.

When the command finishes, the server picks one stream to save. Each branch
prefers the stream that normally carries the useful content, and falls back to
the other when the preferred one is empty — so a cell never renders blank while
the other stream has something to show:

- **exit = 0** → stdout. But when stdout is empty, stderr instead: plenty of
  commands succeed with all their output on stderr (`tool --version`, `--help`,
  informational messages). When stdout *does* have content, stderr is discarded,
  so genuine noise (progress bars, warnings, `time(1)`) stays hidden.
- **exit ≠ 0** → stderr (the error message — what you almost always want to see
  when something broke), falling back to stdout when stderr is empty (e.g.,
  `false` produces nothing on either).

Examples:

```sh
echo "good"; echo "warning" >&2          # exit=0, stdout present → "good"
echo "noise" >&2; echo "result"          # exit=0, stdout present → "result"
gq --version                             # exit=0, stdout empty   → the stderr version line
ls /nonexistent                          # exit≠0                 → "ls: ... No such file"
```

If you want both streams in the output, redirect explicitly:

```sh
my-tool 2>&1
```

This merges stderr into stdout at the shell level, so clinote sees one stream and saves it normally.

This is a deliberate departure from the v1 spec (which merged the streams). The format picker doesn't change which stream was saved — that's decided at run time by the exit code.

## Worked example: a graph-to-diagram pipeline

This pulls the pieces above into one notebook. The task: run a Cypher query
against Neo4j, map the result into a figure description, and render it to SVG —
a four-stage chain where each stage's output feeds the next.

Start it in your shell (so the credential never touches the file), then open it:

```sh
export NEO4J_PW=…
clinote notebooks/jacket-loop.md
```

The notebook:

````markdown
---
title: Jacket loop — R101_JKT
shell: bash
editable: true
width: full
requires:
  - NEO4J_PW
---

# Jacket loop — R101_JKT

Renders the equipment module `R101_JKT` and its attributes as an S88 figure.

## Setup

```sh
: "${NEO4J_PW:?export it before launching clinote}"
STEM=jacket-loop
mkdir -p queries
echo "ready — NEO4J_PW set (${#NEO4J_PW} chars)"
```

## The query

Editing this cell regenerates the `.cypher` file, so the notebook is the source
of truth for the query — not a file you have to remember to keep in sync.

```sh
cat > "queries/$STEM.cypher" <<'EOF'
MATCH p = (em:EquipmentModule {name: 'R101_JKT'})
          -[:CONTAINS]->(cm)-[:HAS_ATTRIBUTE]->(a)
RETURN p ORDER BY a.name
EOF
echo "wrote queries/$STEM.cypher"
```

## Rows

The query result is the thing you most want to eyeball when a figure looks
wrong. `tee` lands it on disk as `$STEM.jsonl`; `out=jsonl` renders it as a
sortable table right here.

```sh out=jsonl
cyq --password "$NEO4J_PW" --format jsonl -f "queries/$STEM.cypher" \
  | tee "$STEM.jsonl"
```

## Map

Reads the cached `.jsonl` rather than re-querying, so tuning `s88.gfigmap`
re-runs this stage alone — with **run ↓** — without touching the database.

```sh
gfig map -m s88.gfigmap --source "cyq -f queries/$STEM.cypher" \
  < "$STEM.jsonl" > "$STEM.gfig"
echo "wrote $STEM.gfig"
```

## Validate

A gate of its own: on success you see the confirmation, on failure the run
stops here and shows `gfig check`'s stderr instead of marching on to render a
broken figure.

```sh
gfig check "$STEM.gfig" && echo "check: clean"
```

## Render

```sh
gfig render "$STEM.gfig" > "$STEM.svg"
echo "rendered $STEM.svg"
```

![jacket loop](jacket-loop.svg)
````

### How it runs

- **First time:** click **Run all**. It stops at the first non-zero exit, so a
  bad query or a failed `gfig check` halts the chain instead of producing a
  wrong diagram.
- **Iterating on the map:** edit `s88.gfigmap`, then **run ↓** on the *Map*
  cell. Stages below re-run against the cached `.jsonl`; Neo4j is untouched.
- **New module:** change `R101_JKT` in the query cell (and the `title`), Run
  all. The notebook is a template — the query lives in it, not beside it.

### Why it's shaped this way

- The **guard cell** plus `requires:` catch a missing `NEO4J_PW` two ways: the
  banner before you start, the cell if you started regardless. `${#NEO4J_PW}`
  confirms it's set without printing it.
- **Splitting the pipe** (`cyq … | tee $STEM.jsonl`, then a separate cell that
  reads the file) turns the expensive query into a cache. It's also the only
  way to *see* the intermediate — piped straight into `gfig map` it would exist
  nowhere.
- The `.svg`, `.gfig` and `.jsonl` are **build artifacts on disk**, not baked
  into the `.md`. The notebook stays small and its diffs stay readable when you
  regenerate; the figure still shows because files next to the notebook are
  served. The trade-off is that moving the notebook means moving its artifacts.
- No credential is ever written to the file, because it comes from the
  environment, not a cell.

## Front-matter reference

```yaml
---
title: Disk usage investigation     # string, free-form
created: 2026-05-26T14:30:00Z       # RFC 3339 timestamp
shell: bash                         # bash | zsh; default bash
editable: true                      # unlock sh-cell editing + delete + format picker
width: full                         # use full window width; default narrow column
requires:                           # env vars the notebook needs; warns if unset
  - NEO4J_PW
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
