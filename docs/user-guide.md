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
- [Defining macros](#defining-macros)
- [Result kinds](#result-kinds)
- [stdout and stderr](#stdout-and-stderr)
- [Secrets](#secrets)
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

Not yet released. Build from source:

```sh
git clone https://github.com/pmuston/clinote
cd clinote && go install ./cmd/clinote
```

Homebrew still serves **v1** until 2.0.0 ships — `brew install pmuston/tap/clinote`
gets you the previous tool, which uses a different, incompatible format.

Check which you have:

```sh
clinote version
# clinote v2.0.0 (b93d09ef6e6c)
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
```output {run="2026-08-11T09:51:02Z", tool="clinote/2.0"}
hello from clinote
```
````

That is the whole loop.

## Anatomy of a notebook

**Front matter** — `notekit: 1` marks the file as a notebook and is required.
`notekit-tool: clinote` records which tool wrote it (advisory; it never decides
whether a notebook opens). `title` and `shell` are honoured. Unknown keys are
preserved.

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
- **Add** and **Delete** for cells; prose is editable in place.
- **Interrupt** sends SIGINT to a running command — the way to recover a hung cell
  without stopping the server.

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

To change a cell's kind, edit the cell. v1's dropdown is not in v2.

## stdout and stderr

Both interleave as produced, as in a terminal. Exit zero writes an `output` block;
non-zero writes an `error` block with the status:

````markdown
```error {status=127, run="2026-08-11T09:44:12Z", tool="clinote/2.0"}
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

v1's `requires:` front-matter banner is not in v2, so the guard cell is the whole
mechanism for now.

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

**One caveat.** The rendered `jacket-loop.svg` **will not display in the browser**:
v2 does not serve files sitting next to the notebook. v1 did. Open the file
directly for now.

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

`migrate` flags: `-dry-run` (report only), `-in-place` (rewrite, keeping
`<name>.v1.bak`).

Exit status: `0` clean, `1` server failure, `2` usage or I/O failure.

## Limitations and gotchas

**External edits are overwritten.** The in-memory notebook is authoritative during
a session. Stop clinote, edit, restart.

**`exit N` kills the persistent shell.** Use `false`, `return N` inside a function,
or a subshell `( … ; exit N )`.

**TUI programs hang the cell.** `vim`, `less`, `htop` — use Interrupt to recover,
and prefer `cat`, `head`, `tail`.

**Output is capped per cell.** The excess is dropped and the block marked
`truncated`.

**ANSI colour is live-only.** The file holds plain text; `-term` with a real
terminal type enables colour in the browser during a session.

**Images are not served.** `![chart](chart.svg)` will not render — see the worked
example's caveat.

**A second fence in a section is silently prose.** The single most likely way to
lose work; give every cell its own heading.
