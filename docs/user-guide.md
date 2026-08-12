# clinote user guide

clinote v2 — a shell notebook built on [notekit](https://github.com/pmuston/notekit).

The format is notekit's and its
[specification](https://github.com/pmuston/notekit/blob/main/notekit-format-spec.md)
is the authority; this guide covers using clinote and the parts specific to running
shell commands. For a one-page overview see [about.md](about.md).

## Contents

- [What clinote is for](#what-clinote-is-for)
- [Install](#install)
- [Your first notebook](#your-first-notebook)
- [Anatomy of a notebook](#anatomy-of-a-notebook)
- [Working in the browser](#working-in-the-browser)
- [The persistent shell](#the-persistent-shell)
- [Progress bars and spinners](#progress-bars-and-spinners)
- [Defining macros](#defining-macros)
- [Result kinds](#result-kinds)
- [stdout and stderr](#stdout-and-stderr)
- [Secrets](#secrets)
- [Declaring what a notebook needs](#declaring-what-a-notebook-needs)
- [Migrating from v1](#migrating-from-v1)
- [Worked example: a query-to-figure pipeline](#worked-example-a-query-to-figure-pipeline)
- [CLI reference](#cli-reference)
- [Limitations and gotchas](#limitations-and-gotchas)

## What clinote is for

Capturing an investigation, a runbook, or a shell experiment as a file that still
makes sense in a year. Commands and their results live together in plain
CommonMark, so the notebook greps like text, diffs like text, and renders on
GitHub with nothing app-specific showing.

Good for: reproducing an investigation later; iterating on a pipeline with
previous attempts still visible; runbooks where each step is runnable; sending a
colleague "here's how I did it".

Not for: CI or headless execution (there is no batch mode yet); collaboration;
interactive TUI programs.

## Install

```sh
brew tap pmuston/tap
brew trust pmuston/tap   # required for third-party taps
brew install pmuston/tap/clinote
```

Or from source: `go install github.com/pmuston/clinote/cmd/clinote@latest`.

Check which you have:

```sh
clinote version
# clinote v2.2.1 (a1b2c3d4e5f6)
```

A `, modified` suffix means the binary was built from a dirty tree.

## Your first notebook

```sh
clinote new my-notebook.md
```

That writes a starter notebook and serves it, printing a URL. **clinote does not
open a browser** — copy the URL yourself. (v1 did; v2 leaves it to you.)

```markdown
---
notekit: 1
title: My notebook
notekit-tool: clinote
---

## First command

```sh
echo "hello from clinote"
```
```

Click **Run**. The result is written into the same file:

````markdown
```output {run="2026-08-11T09:51:02Z", tool="clinote/2.2"}
hello from clinote
```
````

That is the whole loop.

## Anatomy of a notebook

**Front matter** — `notekit: 1` marks the file as a notebook and is required.
`notekit-tool: clinote` records which tool wrote it (advisory; it never decides
whether a notebook opens). `title` and `shell` are honoured. Unknown keys are
preserved. Four keys change how a reader treats the notebook:

| Key | Effect |
|---|---|
| `width: full` | Use the whole window rather than a reading column. |
| `editable: false` | Withhold editing: the source, prose, and adding, deleting or moving cells. |
| `local-files: true` | Declare that the notebook displays files from its own directory. |
| `requires: [NAME, …]` | Name the environment variables the notebook needs. See [below](#declaring-what-a-notebook-needs). |

`editable: false` **never gates running** — a notebook handed to someone to work
through is meant to be run, and running still writes results. It is a guard rail
against the accidental edit, not a control: anyone can edit the file in their
editor, and should be able to.

`local-files: true` only *declares*. The grant is `-allow-local-files` on the
command line, because a notebook is exactly the part someone else may have
written, and a file that could authorise reading its neighbours would be
authorising itself. Without the flag the page says so rather than showing a broken
image.

**A cell** is a heading of level 2–6, optional prose, then a fenced code block:

````markdown
## Largest directories

```sh
du -d1 -h | sort -hr | head -5
```
````

**The heading is not optional, and this is the rule that catches people out.** A
heading's section holds exactly **one** source fence. A second fence in the same
section is prose — it will never run, and `notefmt` does not warn about it
([notekit#1](https://github.com/pmuston/notekit/issues/1)). Give every cell its own
heading.

A level-1 heading cannot own a cell either, so `# Title` at the top of a file needs
a `##` below it before the first cell.

**Results** sit immediately after the source fence and are replaced whole on every
run. Success writes `output`; failure writes `error` with the exit status.

## Working in the browser

- **Run** on each cell.
- **Run all** runs every cell from the top.
- **Run below** runs this cell and everything after it. The case it exists for: a
  pipeline whose first stage is an expensive query, where you are iterating on what
  comes after it.
- **The format dropdown** changes how a cell's result is read — see
  [Result kinds](#result-kinds).
- **Add** and **Delete** for cells; prose is editable in place.
- **Interrupt** sends SIGINT to a running command — the way to recover a hung cell
  without stopping the server.

**Run all and Run below do not stop at the first failure.** The cells are submitted
to a scheduler that serialises them, so by the time one fails the rest are queued
already. clinote v1 halted the batch on a non-zero exit; if a later stage would run
against stale inputs, watch it rather than assuming a red block stopped things.

The UI works without JavaScript, and cell bodies are editable directly.

Ctrl-C in the terminal stops clinote itself.

## The persistent shell

One `bash -i` or `zsh -i` runs under a pty for the life of the server, so state
carries between cells:

````markdown
## Go somewhere

```sh
cd /var/log
```

## Confirm

```sh
pwd
```
````

The second cell prints `/var/log`. The same holds for environment variables, shell
functions, aliases and `set` options. The session ends when clinote stops.

## Progress bars and spinners

clinote runs cells under a pty, which is what makes `cd` persist and lets tools that
need a terminal work at all. The cost is that **every tool thinks it is talking to a
person**: `[ -t 1 ]` is true in a cell, so anything with a progress display draws one.
`-term dumb` does not prevent this — most spinner libraries check whether stdout is a
terminal, not what `TERM` says.

A spinner redraws one line by returning to column zero between frames. clinote replays
that, so the file keeps the line as it finally read rather than every frame end to end:

````markdown
```output {run="…", tool="clinote/2.2"}
hello
```
````

That is usually what you want. Two cases where it still is not:

**A tool that redraws several lines at once** — `docker pull` with its per-layer bars,
`cargo`, `bazel` — moves the cursor up between lines. Replaying that needs a whole
screen modelled rather than a line, which clinote does not do; those still land as
successive frames.

**A tool that omits the erase.** Overwriting happens column by column, exactly as in a
terminal, so a short frame leaves the tail of a longer one behind:
`Downloading 100%` then `\rDone` reads `Doneloading 100%`. Your terminal shows the
same thing — it is why well-behaved tools emit an erase-to-end-of-line — but it can
look like clinote mangled the output when it did not.

For both, the fix is to stop the tool drawing progress at all. In order of how often it
works:

```sh
export CI=1                    # honoured by a great many CLIs; set it once
some-tool --progress=plain     # or --quiet, --no-progress; check the tool
some-tool | cat                # stdout is a pipe, so `[ -t 1 ]` is false
```

Because the shell persists, `export CI=1` in a setup cell covers the whole notebook.
`| cat` is the reliable fallback when a tool has no flag — it is the same trick that
makes tools behave in a shell script.

Note that `| cat` changes the exit status to `cat`'s. Add `set -o pipefail` in a setup
cell if a cell must still fail when the command does.

## Defining macros

Because the shell persists, define a function once and call it from any later
cell. Put the definitions in the first cell:

````markdown
## Setup

```sh
cy() { curl -s -XPOST localhost:8080/cypher -d "$1"; echo; }
echo "macros loaded"
```
````

Run it once at the start of a session; every cell below can call `cy '…'`.

**Prefer functions to aliases.** Functions take arguments and resolve at execution
time. An alias defined in a cell cannot be used *later in that same cell*, because
the shell expands aliases when it parses the block — it works from the next cell
onward, and that split is confusing.

Your `~/.bashrc` or `~/.zshrc` functions are available too (clinote runs an
interactive shell), but those do not travel with the notebook.

## Result kinds

Declare one on the cell:

````markdown
```sh {format=csv}
psql -c "select id, email from users" --csv
```
````

| Kind | Durable form | In the browser |
|---|---|---|
| *(absent)* / `text` | plain text | preformatted; ANSI colour live only |
| `csv` | RFC 4180 with a header row | sortable table |
| `tsv` | tab-separated | sortable table |
| `jsonl` | one JSON object per line | sortable table, columns from the keys |

Anything live-only — colour, sortability — degrades to nothing on disk. The file is
the artifact; the browser is a courtesy.

**Changing the kind after a run.** Every cell has a dropdown in its header. Picking
a kind rewrites `{format=…}` on the cell *and relabels the result already on the
page*, so output you have just realised was a table becomes one without re-running.
That matters when the command was expensive; when it was not, re-running works too.

Relabelling is honest rather than a fudge: a result body is the bytes the command
produced, and `format` says how to read them, not what they are. An `error` block is
left alone — a failure is not a table however the cell is labelled.

`tsv` is not `csv` with a different delimiter. It has no quoting at all, so a field
cannot contain a tab or a newline and nothing is unescaped: a cell reading
`he said "hi", ok` is exactly those characters, quotes and comma included. That is
what makes it the easy one to emit from a shell — `cut`, `awk -F'\t'`,
`psql -A -F$'\t'` — for data that would need quoting as CSV.

## stdout and stderr

Both interleave as produced, as in a terminal. Exit zero writes an `output` block;
non-zero writes an `error` block with the status:

````markdown
```error {status=127, run="2026-08-11T09:44:12Z", tool="clinote/2.2"}
zsh: command not found: dv
```
````

v1 captured the streams separately and picked one by exit code, hiding stderr on
success. The format has one result body, so that is gone. If a command is noisy on
success, quieten it in the shell: `cmd 2>/dev/null`.

This also means a command that writes its real output to stderr and exits zero —
`tool --version` does this surprisingly often — shows up correctly rather than
blank.

## Secrets

Never set a credential in a cell:

```sh
export NEO4J_PW=hunter2      # DON'T — this text is saved into the .md
```

Cell bodies are written to disk verbatim. Export it before launching instead; the
shell inherits clinote's environment:

```sh
export NEO4J_PW=…
clinote notebooks/graph.md
```

A guard cell as the first cell fails loudly if you forget:

```sh
: "${NEO4J_PW:?export it before launching clinote}"
echo "NEO4J_PW set (${#NEO4J_PW} chars)"
```

`${#VAR}` reports the length only — enough to tell a real value from an empty one
or a leftover placeholder, which otherwise surface as a confusing auth error
several cells later. The `${VAR:?}` form fails the cell without killing the
session, because the shell is interactive.

The guard cell pairs with `requires:`, below: the front matter says what is needed
so a reader knows before running anything, and the guard makes a cell fail cleanly
if it is missing anyway.

## Declaring what a notebook needs

List the environment variables a notebook depends on and the page says which are
missing before you run anything:

```yaml
---
notekit: 1
title: Graph queries
requires: [NEO4J_PW, NEO4J_URL]
---
```

Only names are read, and only whether each is non-empty — no value ever reaches the
notebook, which is the point. Handing someone a notebook that names its
prerequisites is better than handing them one that fails on cell four.

**It never blocks.** A notebook should open for reading without your credentials to
hand, and a cell that needs one fails on its own terms with a better message than a
refusal at the front door. Pair it with a guard cell (above) for the loud failure.

**Write it inline.** A YAML block list —

```yaml
requires:
  - NEO4J_PW
```

— is invisible to the reader: notekit parses front matter without a YAML
marshaller, deliberately, so that a notebook round-trips byte for byte. The block
form comes back empty. clinote reports that as a mistake and names the inline form
rather than silently reporting nothing, but the fix is yours to make.

## Migrating from v1

v1 notebooks are a different format. Opening one tells you so:

```
clinote: s88.md is a clinote v1 notebook
         convert it with: clinote migrate s88.md
```

```sh
clinote migrate -dry-run notebooks/*.md   # report only
clinote migrate notebooks/s88.md          # writes notebooks/s88.v2.md
clinote migrate -in-place notes.md        # keeps notes.md.v1.bak
```

A dry run reports what will change:

```
s88.md: 1 cells
  1 heading(s) invented — rename freely, it costs nothing:
    cell 1: ## cyq  (from command)
  1 failed result(s) became error blocks
  1 duration(s) dropped — the format has no `dur` key
```

**Headings.** v1 needed none and v2 needs one per cell, so most are invented — from
the command's name where possible (`## cyq`), otherwise `## Cell N`. Rename them
freely afterwards: notekit ties a cell's identity to its `id`, not its heading, so
renaming costs nothing.

**Failures** become `error` blocks. v1 wrote them as `output` with a non-zero
`exit`, which also left a `type=jsonl` on results that were never JSONL.

**`dur=` is dropped** — the format records `run` and `tool`, not elapsed time.

**Provenance stays honest.** Migrated results keep their original timestamp and are
marked `tool="clinote/1"`, because that is what produced them.

Migration **refuses to write if the cell count changed**. That is not a formality:
a stranded fence becomes prose silently, so counting is the only thing standing
between a bad heading choice and a quietly gutted notebook.

## Worked example: a query-to-figure pipeline

A four-stage chain — query, map, validate, render. Start it with the credential in
the environment:

```sh
export NEO4J_PW=…
clinote notebooks/jacket-loop.md
```

````markdown
---
notekit: 1
title: Jacket loop — R101_JKT
shell: bash
notekit-tool: clinote
---

# Jacket loop — R101_JKT

## Setup

```sh
: "${NEO4J_PW:?export it before launching clinote}"
STEM=jacket-loop
mkdir -p queries
echo "ready"
```

## The query

```sh
cat > "queries/$STEM.cypher" <<'EOF'
MATCH p = (em:EquipmentModule {name: 'R101_JKT'})
          -[:CONTAINS]->(cm)-[:HAS_ATTRIBUTE]->(a)
RETURN p ORDER BY a.name
EOF
echo "wrote queries/$STEM.cypher"
```

## Rows

```sh {format=jsonl}
cyq --password "$NEO4J_PW" --format jsonl -f "queries/$STEM.cypher" \
  | tee "$STEM.jsonl"
```

## Map

```sh
gfig map -m s88.gfigmap --source "cyq -f queries/$STEM.cypher" \
  < "$STEM.jsonl" > "$STEM.gfig"
echo "wrote $STEM.gfig"
```

## Validate

```sh
gfig check "$STEM.gfig" && echo "check: clean"
```

## Render

```sh
gfig render "$STEM.gfig" > "$STEM.svg"
echo "rendered $STEM.svg"
```
````

**Why it is shaped this way.**

The query cell writes the `.cypher` file, so the notebook is the source of truth
for the query rather than something to keep in sync with a file beside it.

Splitting the pipe — `cyq … | tee $STEM.jsonl`, then a separate cell reading the
file — turns the expensive query into a cache. Tuning `s88.gfigmap` re-runs only
the map stage, never the database. It is also the only way to *see* the
intermediate: piped straight into `gfig map` it would exist nowhere. Tagging that
cell `{format=jsonl}` renders it as a sortable table.

`gfig check` is its own cell so a failure stops there, with `gfig check`'s message
in an `error` block, rather than rendering a broken figure.

**Showing the figure.** Add `local-files: true` to the front matter and start
clinote with `-allow-local-files`; then `![jacket loop](jacket-loop.svg)` renders.
The notebook declares the need and the flag grants it, because a notebook is the
part someone else may have written — see the [front-matter
reference](#front-matter-reference).

## CLI reference

```sh
clinote [flags] [notebook.md]      # serve; with no path, the one in this directory
clinote new [flags] <notebook.md>  # create, then serve
clinote migrate [flags] <path>...  # convert v1 notebooks
clinote version                    # also --version, -v
```

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `127.0.0.1:8080` | Address to listen on. |
| `-shell` | your `$SHELL` if bash/zsh, else bash | Shell for cells. |
| `-term` | `dumb` | `TERM` for the shell; a real value enables colour auto-detection. |
| `-poll` | `500ms` | How often the browser polls a running cell. |
| `-list` | — | List candidate notebooks and exit. |
| `-allow-local-files` | off | Serve files from the notebook's directory, confined to it. |

`migrate` flags: `-dry-run` (report only), `-in-place` (rewrite, keeping
`<name>.v1.bak`).

Exit status: `0` clean, `1` server failure, `2` usage or I/O failure.

## Limitations and gotchas

**External edits are overwritten.** The in-memory notebook is authoritative during
a session. Stop clinote, edit, restart.

**`exit N` kills the persistent shell.** Use `false`, `return N` inside a function,
or a subshell `( … ; exit N )`.

**TUI programs hang the cell.** `vim`, `less`, `htop` — use Interrupt to recover,
and prefer `cat`, `head`, `tail`. This is not a gap to be filled: a full-screen program
produces a *screen*, and a notebook records a *stream*. The cell never exits, so there
would be nothing to write down even if the screen were captured.

**Output is capped per cell.** The excess is dropped and the block marked
`truncated`.

**ANSI colour is live-only.** The file holds plain text; `-term` with a real
terminal type enables colour in the browser during a session.

**Images need the grant.** `![chart](chart.svg)` renders only when the notebook
says `local-files: true` and clinote is started with `-allow-local-files`.

**A second fence in a section is silently prose.** The single most likely way to
lose work; give every cell its own heading.
