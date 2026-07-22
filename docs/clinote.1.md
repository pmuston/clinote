% clinote 1 "2026-07-22" "clinote 0.1.6" "clinote Manual"

<!--
  The first line above becomes the raw .TH arguments, in order:
      name  section  date  source  manual
  It is NOT pandoc `NAME(section)` syntax — go-md2man dumps it verbatim.
  Render with:
      go run github.com/cpuguy83/go-md2man/v2@latest -in docs/clinote.1.md -out clinote.1
  Keep the date, version, and flag list in sync with the release.
-->

# NAME

clinote - a personal lab notebook for shell commands, in your browser

# SYNOPSIS

**clinote** [**--no-browser**] [_path/to/notebook.md_]

**clinote** **new** [**--no-browser**] _path_

**clinote** **version**

# DESCRIPTION

**clinote** opens a Markdown notebook in a browser-based UI. One `.md` file is
one notebook. A single interactive shell is bound to the notebook for the
lifetime of the server, so state — working directory, environment variables,
shell functions — flows from one cell to the next.

Fenced ` ```sh ` code blocks are command cells. Running one executes its body in
the persistent shell and splices the result back into the same `.md` file as an
adjacent ` ```output ` block. The on-disk file stays plain CommonMark:
readable, grep-able, and correctly rendered on GitHub.

The server binds to `127.0.0.1` on a free port, prints the URL to standard
output, and opens the default browser unless told not to. It runs until
interrupted with Ctrl-C.

With no path, **clinote** lists the `.md` files in the current directory. With
**new**, it scaffolds a starter notebook and opens it.

# COMMANDS

_path_
: Open the notebook at _path_ and serve it.

**new** _path_
: Create a starter notebook at _path_ (refusing to overwrite an existing file)
and open it. The scaffold sets `editable: true` and `width: full`.

**version**
: Print the version and build revision, and exit.

# OPTIONS

**--no-browser**
: Print the URL but do not open a browser. Also honoured via the `BROWSER`
environment variable (see ENVIRONMENT).

**-v**, **--version**, **version**
: Print the version and build revision, and exit. A `, modified` suffix on the
revision means the binary was built from a dirty tree.

**-h**, **--help**
: Print usage, including the documentation URL, and exit.

# NOTEBOOK FORMAT

A notebook is UTF-8 Markdown with optional YAML front matter. Three kinds of
content are meaningful:

**Prose**
: Any Markdown that is not an `sh` or `output` fenced block.

**Command cells**
: Fenced blocks tagged `sh`. The body is sent verbatim to the shell. An
`out=csv|tsv|jsonl` attribute on the info line hints the output type.

**Output cells**
: Fenced blocks tagged `output`, written by clinote. They carry `type`, `exit`,
`ran`, `dur`, and, when the 1 MiB cap is hit, `truncated=true`.

An output block belongs to the command block directly above it only when
whitespace alone separates them; intervening prose orphans it. Pairing is
strictly positional — there are no cell IDs.

# FRONT MATTER

**title**
: Free-form string, shown in the header.

**created**
: RFC 3339 timestamp.

**shell**
: `bash` or `zsh`. Defaults to `bash`.

**editable**
: `true` unlocks in-browser editing of command bodies, the output-format picker,
and block deletion. Defaults to `false` — the safe choice for a shared notebook.

**width**
: `full` uses the whole window width; otherwise a narrow reading column.

**requires**
: A list of environment variable names the notebook needs. Any that are unset or
empty are named in a banner. It reports; it never blocks, and cannot execute
anything.

Unknown fields are preserved on save.

# OUTPUT TYPES

Declared by `out=` on the command, or the output block's `type=`.

**text**
: Default. ANSI colours render live after a run; the saved file is plain text.

**csv**, **tsv**
: Rendered as a sortable table. Numeric columns sort numerically.

**jsonl**
: One JSON object per line, rendered as a table whose columns are the union of
keys.

# RUNNING

Each command cell has a **Run** button. **Run all** (header) runs every cell
from the top; **run ↓** (per cell) runs that cell and everything below it. Both
stop at the first non-zero exit, on the assumption that a notebook is a chain
and later stages would run against stale inputs. Use `cmd || true` for a cell
that should survive failure.

The **Interrupt** button sends SIGINT to the running command; it is the way to
recover a hung cell without stopping the server. Ctrl-C in the terminal stops
clinote itself.

# FILES

Files next to the notebook are served, so a Markdown image link resolves in the
browser as it does on GitHub:

    ![chart](chart.svg)

Only the notebook's own directory is reachable. Paths above it — including
percent-encoded traversal and symlinks pointing outside — are refused, as are
dotfiles. Served files carry a sandboxing `Content-Security-Policy`, so an SVG
containing a script is inert.

# ENVIRONMENT

**BROWSER**
: If set to `none`, `false`, `0`, or empty, the browser is not opened. Otherwise
the system default is used.

Credentials should be exported before launching clinote — the shell inherits the
environment, so cells can use `"$VAR"` while the value never touches the file.
Never `export` a secret inside a cell; cell bodies are written to disk verbatim.

# EXAMPLES

Open a notebook:

    clinote notebooks/disk-usage.md

Create one and start editing:

    clinote new experiments/idea.md

Run a notebook that needs a credential, without writing it to the file:

    export NEO4J_PW=…
    clinote notebooks/graph.md

# EXIT STATUS

**0**
: Success.

**1**
: Runtime error (message on stderr).

**2**
: Usage error.

# LIMITATIONS

- The in-memory notebook is authoritative during a session; external edits to
the file are overwritten on the next save.
- Interactive TUI commands (`vim`, `less`, `htop`) hang the cell; use Interrupt.
- Output is capped at 1 MiB per cell.
- `exit N` in a cell terminates the persistent shell; use `false` or a subshell.
- ANSI colour is a live-render nicety; a reloaded notebook shows plain text.

# SEE ALSO

Full documentation: <https://pmuston.github.io/clinote>

# AUTHOR

Paul Muston
