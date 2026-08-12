% clinote 1 "2026-08-12" "clinote 2.2.1" "clinote Manual"

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

Each cell offers **Run**, **Run below** — this cell and every cell after it, for a
pipeline whose first stage is too expensive to repeat — a format dropdown, and
move and delete. **Interrupt** sends SIGINT to a running command, which recovers a
hung cell without stopping the server.

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

**-allow-local-files**
: Serve files from the notebook's own directory, so an image link in its prose
resolves. Off by default. A notebook declares the need with `local-files: true`
(§2.4) but cannot grant it: a notebook is the part someone else may have written,
and a file that could authorise reading its neighbours would be authorising
itself. Serving is confined to that directory — paths escaping it, dot-prefixed
components and directories are refused, and files are sent with headers that stop
the browser executing them.

Flags for **migrate**:

**-dry-run**
: Report what would change and write nothing.

**-in-place**
: Rewrite the notebook, keeping the original as _name_`.v1.bak`. Without this a
new file _name_`.v2.md` is written and the original left alone.

# NOTEBOOK FORMAT

The format belongs to notekit; its specification is authoritative. In outline:

Front matter must carry `notekit: 1`. `title` and `shell` are honoured, and
unknown keys are preserved. Four further keys affect the reader:

**width**
: `full` uses the whole window rather than a reading column.

**editable**
: `false` withholds editing — the source, prose, and adding, deleting or moving
cells. It never gates running, so a notebook handed to someone to work through
still runs; a guard rail against the accidental edit, not a control, since anyone
can edit the file in their editor.

**local-files**
: `true` declares that the notebook displays files from its own directory. It
requests only; see **-allow-local-files**.

**requires**
: Names the environment variables the notebook needs, as an inline list —
`requires: [NEO4J_PW, NEO4J_URL]`. The page reports which are unset and never
blocks: a notebook should open for reading without the credentials to hand, and a
cell that needs one fails on its own terms with a better message. Only names are
read, and only whether each is non-empty, so no value reaches the notebook. A YAML
block list is not readable here and is reported as a mistake.

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

    ```output {format=csv, run="2026-07-16T09:41:07Z", tool="clinote/2.2"}
    size,path
    1.2G,./data
    ```

# RESULT KINDS

Declared on the cell as `{format=…}`, or chosen from the dropdown in each cell's
header. Picking one rewrites the cell's `{format=…}` **and relabels the result
already on the page**, so output that turns out to be a table becomes one without
re-running an expensive command. A result body is the bytes the command produced
and `format` says how to read them, so relabelling changes no data; an `error` block
is left alone.

The kinds:

**text**
: The default, written by omitting the key. Plain preformatted text. ANSI colour
renders live only; the file keeps stripped text.

**csv**, **tsv**
: Rendered as a sortable table, header row required. CSV is RFC 4180. TSV is not
CSV with a different delimiter: it has no quoting at all, so a field cannot contain
a tab or a newline and nothing is unescaped — `he said "hi", ok` is exactly those
characters. That is what makes it the easy one to emit from a shell (`cut`,
`awk -F'\t'`, `psql -A -F$'\t'`) for data that would need quoting as CSV.

**jsonl**
: One JSON object per line, rendered as a table whose columns are the union of
keys.

Nothing that exists only in the browser — colour, sortability — survives to disk.
The durable form must stand alone.

# PROGRESS DISPLAYS

Cells run under a pty — which is what makes working directory and shell state persist
— so every tool sees a terminal and anything with a spinner or a progress bar draws
one. **-term** `dumb` does not prevent it: most such tools test whether stdout is a
terminal, not what `TERM` says.

A display that redraws one line is replayed, so the result holds the line as it finally
read rather than every frame concatenated. Overwriting is column by column as in a
terminal, so a tool that omits an erase-to-end-of-line leaves the tail of a longer
frame behind — `Downloading 100%` followed by a carriage return and `Done` reads
`Doneloading 100%`, which is what the terminal shows too.

A display that redraws *several* lines by moving the cursor up is not replayed. That
needs a screen to be modelled rather than a line.

To stop a tool drawing progress at all:

    export CI=1                  # honoured widely; the shell persists, so set it once
    some-tool --progress=plain   # or --quiet, --no-progress
    some-tool | cat              # stdout becomes a pipe, so `[ -t 1 ]` is false

`| cat` reports cat's exit status; add `set -o pipefail` if the cell must still fail.

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
Interrupt button. A full-screen program produces a screen and a notebook records a
stream, so this is a boundary rather than a gap.
- Cells run under a pty, so `[ -t 1 ]` is true and tools draw progress displays.
See **PROGRESS DISPLAYS**.
- Output is capped per cell; the excess is dropped and the block marked
`truncated`.
- `exit N` in a cell terminates the persistent shell. Use `false`, `return` inside
a function, or a subshell.
- Files beside the notebook are served only with **-allow-local-files**, so an
image link will not render until that is given.
- Run all and Run below do not stop at the first failure. The cells are submitted
to a scheduler that serialises them, so the rest are queued by the time one fails.
clinote v1 halted the batch on a non-zero exit.

# SEE ALSO

Full documentation: <https://pmuston.github.io/clinote>

The notekit format specification:
<https://github.com/pmuston/notekit>

# AUTHOR

Paul Muston
