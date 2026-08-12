# clinote

A personal lab notebook for shell commands. One markdown file is one notebook;
commands and their results live together as plain CommonMark — readable,
grep-able, and correct on GitHub.

## Purpose

Capture an investigation, runbook, or experiment as a sequence of runnable shell
cells with their output baked in. The browser is a convenient runner; the `.md`
file is the artifact. Send it to a colleague, commit it to git, open it next
month — it is all still there.

## How v2 is built

clinote v2 is an executor plus a `main` on top of
[notekit](https://github.com/pmuston/notekit). The on-disk format, byte-range
splice, run scheduling, capture limits and the browser UI belong to that library;
what remains here is the pty shell and the wiring. notekit exists because three
notebook tools independently converged on the same design, and clinote was one of
them.

The practical consequence: notebooks are interchangeable with the kit's other
tools, and the contract between them is conformance to the format rather than
shared code. CI enforces exactly that — clinote writes notebooks, notekit's own
checker judges them.

## Features

- **Persistent shell per notebook** — `cd`, environment variables and functions
  carry between cells for the life of the server.
- **Results written back into the file** — an `output` fence on success, a
  first-class `error` fence carrying the exit status on failure.
- **Typed results** — `{format=csv}`, `{format=tsv}` or `{format=jsonl}` render as
  sortable tables in the browser and stay plain text on disk. A dropdown changes a
  cell's kind and relabels the result already on the page, so output that turns out
  to be a table becomes one without re-running an expensive command.
- **Run, Run all, Run below, Interrupt** — Run below covers the pipeline whose
  first stage is too costly to repeat. Interrupt sends SIGINT to the running
  command, so a hung cell need not cost you the session.
- **Front matter that travels with the notebook** — `width: full`, `editable:
  false`, `local-files: true` and `requires: [NAME, …]`, the last reporting which
  environment variables are unset without ever reading a value.
- **Cell identity and sidecars** — from the kit: durable cell `id`s, and an
  artifact lifecycle that survives heading renames and reordering.
- **`clinote migrate`** — converts clinote v1 notebooks, refusing to write if the
  cell count changed.
- **Works without JavaScript**, and cell bodies are editable in the browser.
- **Single static binary**, no build step, no database.

## Non-goals

Multi-user or hosted operation, multi-language kernels (the kit's answer to a
second language is a second tool), terminal emulation, and any knowledge of
specific CLI tools.

Deferred rather than rejected: headless/CI execution, cross-cell named results,
streaming output.

## At a glance

```sh
brew tap pmuston/tap
brew trust pmuston/tap
brew install pmuston/tap/clinote

clinote new notebook.md
# → prints a URL; open it, click Run, output appears in the file
```

v1 notebooks use an incompatible format; `clinote migrate` converts them.

For the full tour, see [user-guide.md](user-guide.md).
