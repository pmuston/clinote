% clinote 1 "2026-08-11" "clinote 2.0.0" "clinote Manual"

<!--
  The first line above becomes the raw .TH arguments, in order:
      name  section  date  source  manual
  It is NOT pandoc `NAME(section)` syntax — go-md2man dumps it verbatim.
  Render with:
      go run github.com/cpuguy83/go-md2man/v2@latest -in docs/clinote.1.md -out clinote.1
  Keep the date and version in step with internal/version.
-->

# NAME

clinote - a shell notebook, served in the browser

# SYNOPSIS

**clinote** [_flags_] [_notebook.md_]

**clinote** **new** [_flags_] _notebook.md_

**clinote** **migrate** [_flags_] _notebook.md_...

**clinote** **version**

# DESCRIPTION

**clinote** serves a notekit notebook whose cells are shell commands. One
interactive shell runs under a pty for the lifetime of the server, so working
directory, environment variables and shell functions carry from one cell to the
next.

Running a cell writes its result back into the same file as an ordinary fenced
block. The notebook stays plain CommonMark: readable, grep-able, and correctly
rendered on GitHub.

With no notebook argument, clinote uses the one notekit notebook in the current
directory. It prints its URL and waits; it does **not** open a browser. Ctrl-C
stops it and destroys the shell.

This is clinote v2, built on the notekit library. Notebooks written by clinote v1
use a different, incompatible format — see **MIGRATING** below.

# COMMANDS

_notebook.md_
: Serve that notebook.

**new** _notebook.md_
: Create a notebook with one starter cell, then serve it. Refuses to overwrite.

**migrate** _notebook.md_...
: Convert clinote v1 notebooks to the notekit format. See **MIGRATING**.

**version**
: Print the version and build revision, then exit. Also **--version** and **-v**.
A `, modified` suffix means the binary was built from a dirty tree.

# OPTIONS

**-addr** _address_
: Address to listen on. Default `127.0.0.1:8080`.

**-shell** _name_
: Shell to run cells in: `bash` or `zsh`. Defaults to `$SHELL` when it names one
of those, otherwise `bash`.

**-term** _value_
: `TERM` for the shell. Default `dumb`, which stops the shell emitting
cursor-positioning sequences into captured output. A real terminal type lets
tools auto-detect colour, which the browser renders live and the file does not
keep.

**-poll** _duration_
: How often the browser polls a running cell. Default `500ms`.

**-list**
: List candidate notebooks in the current directory and exit.

Flags for **migrate**:

**-dry-run**
: Report what would change and write nothing.

**-in-place**
: Rewrite the notebook, keeping the original as _name_`.v1.bak`. Without this a
new file _name_`.v2.md` is written and the original left alone.

# NOTEBOOK FORMAT

The format belongs to notekit; its specification is authoritative. In outline:

Front matter must carry `notekit: 1`. `title` and `shell` are honoured, and
unknown keys are preserved.

A **cell** is an ATX heading of level 2–6, optional prose, then a fenced code
block tagged `sh`. A heading's section holds exactly **one** source fence: a
second fenced block in the same section is prose and will never run. Every cell
therefore needs its own heading, and a level-1 heading cannot own one.

A **result** occupies the region immediately after the source fence and is
replaced whole on each run. A cell that exits zero writes an `output` fence; a
non-zero exit writes an `error` fence carrying `status`.

    ```sh {format=csv}
    du -d1 -h | sort -hr | head -5
    ```

    ```output {format=csv, run="2026-07-16T09:41:07Z", tool="clinote/2.0"}
    size,path
    1.2G,./data
    ```

# RESULT KINDS

Declared on the cell as `{format=…}`:

**text**
: The default, written by omitting the key. Plain preformatted text. ANSI colour
renders live only; the file keeps stripped text.

**csv**, **tsv**
: Rendered as a sortable table. CSV is RFC 4180 with a header row.

**jsonl**
: One JSON object per line, rendered as a table whose columns are the union of
keys.

Nothing that exists only in the browser — colour, sortability — survives to disk.
The durable form must stand alone.

# OUTPUT

Standard output and standard error interleave as produced, as they would in a
terminal. The exit status decides the block: zero writes `output`, non-zero
writes `error` with a `status` key.

clinote v1 captured the two streams separately and chose one by exit code,
suppressing stderr on success. The format has a single result body, so that
behaviour is gone; quieten a noisy command in the shell instead, with
`cmd 2>/dev/null`.

# MIGRATING

A v1 notebook has no `notekit` front-matter key and no per-cell headings. Opening
one names the fix rather than merely refusing.

    clinote migrate -dry-run notebooks/*.md
    clinote migrate notebooks/s88.md

Migration adds the front-matter marker, converts `out=` to `{format=…}`, turns
failed results into `error` blocks, and gives every cell a heading — taken from
the command's name where there is none. Generated headings only need to be
plausible: a cell's identity is its `id`, not its heading, so renaming one later
costs nothing.

`dur=` is dropped, having no key in the format. Migrated results keep their
original timestamp and are marked `tool="clinote/1"`, because clinote v1 produced
them.

Migration re-parses its own output and **refuses to write if the cell count
changed**, which is the only guard against a heading mistake silently turning a
cell into prose.

# ENVIRONMENT

**SHELL**
: Chooses the default for **-shell** when it names `bash` or `zsh`.

Credentials should be exported before launching clinote — the shell inherits its
environment, so cells can use `"$VAR"` while the value never reaches the file.
Never `export` a secret inside a cell: cell bodies are written to disk verbatim.

# EXAMPLES

Serve a notebook on a chosen port:

    clinote -addr 127.0.0.1:9000 notebooks/disk-usage.md

Create one and start work:

    clinote new experiments/idea.md

Run a notebook that needs a credential:

    export NEO4J_PW=…
    clinote notebooks/graph.md

Convert every v1 notebook in a directory, reporting first:

    clinote migrate -dry-run notebooks/*.md
    clinote migrate notebooks/*.md

# EXIT STATUS

**0**
: Clean shutdown.

**1**
: The server failed, or a migration refused to write.

**2**
: Usage or I/O failure.

# LIMITATIONS

- Single user, one notebook per server process. There is no headless or CI mode.
- The in-memory notebook is authoritative; external edits during a session are
overwritten on the next save.
- Interactive TUI commands (`vim`, `less`, `htop`) hang the cell. Use the
Interrupt button.
- Output is capped per cell; the excess is dropped and the block marked
`truncated`.
- `exit N` in a cell terminates the persistent shell. Use `false`, `return` inside
a function, or a subshell.
- Files beside the notebook are not served, so an image link in a notebook will
not render in the browser.

# SEE ALSO

Full documentation: <https://pmuston.github.io/clinote>

The notekit format specification:
<https://github.com/pmuston/notekit>

# AUTHOR

Paul Muston
