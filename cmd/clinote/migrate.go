//go:build unix

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pmuston/clinote/internal/migrate"
)

// migrateCmd converts clinote v1 notebooks to the notekit format.
//
// It never overwrites by default. A migration is not reversible from the output
// alone — `dur=` is gone and orphaned results have become prose — so the original
// stays put unless asked for explicitly, and even then a copy is kept.
func migrateCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("clinote migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", false, "report what would change; write nothing")
	inPlace := fs.Bool("in-place", false, "rewrite the notebook, keeping the original as <name>.v1.bak")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "usage: clinote migrate [flags] <notebook.md>...\n\n"+
			"Converts a clinote v1 notebook to the notekit format. Writes\n"+
			"<name>.v2.md unless -in-place is given.\n\nflags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return exitUsage
	}

	status := exitOK
	for _, path := range fs.Args() {
		if code := migrateOne(path, *dryRun, *inPlace, stdout, stderr); code != exitOK {
			status = code
		}
	}
	return status
}

func migrateOne(path string, dryRun, inPlace bool, stdout, stderr io.Writer) int {
	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "clinote: %v\n", err)
		return exitUsage
	}

	got, rep, err := migrate.Convert(src)
	if err != nil {
		// Convert refuses rather than writing a notebook that lost cells.
		fmt.Fprintf(stderr, "clinote: %s: %v\n", path, err)
		return exitProblem
	}
	if rep.AlreadyV2 {
		fmt.Fprintf(stdout, "%s: already a notekit notebook, nothing to do\n", path)
		return exitOK
	}

	report(stdout, path, rep)

	if dryRun {
		fmt.Fprintf(stdout, "  (dry run — nothing written)\n")
		return exitOK
	}

	dest := path
	if inPlace {
		backup := path + ".v1.bak"
		if err := os.WriteFile(backup, src, 0o644); err != nil {
			fmt.Fprintf(stderr, "clinote: writing %s: %v\n", backup, err)
			return exitProblem
		}
		fmt.Fprintf(stdout, "  original kept at %s\n", backup)
	} else {
		dest = strings.TrimSuffix(path, ".md") + ".v2.md"
		if _, err := os.Stat(dest); err == nil {
			fmt.Fprintf(stderr, "clinote: %s already exists; move it or use -in-place\n", dest)
			return exitProblem
		}
	}

	if err := os.WriteFile(dest, got, 0o644); err != nil {
		fmt.Fprintf(stderr, "clinote: writing %s: %v\n", dest, err)
		return exitProblem
	}
	fmt.Fprintf(stdout, "  wrote %s\n", dest)
	return exitOK
}

// report prints what the migration did, leading with anything the user should
// look at rather than burying it under what went fine.
func report(w io.Writer, path string, rep migrate.Report) {
	fmt.Fprintf(w, "%s: %d cells\n", path, rep.CellsOut)

	if n := rep.InventedHeadings(); n > 0 {
		// Every cell needs its own heading in the notekit format, and v1 notebooks
		// have none. Renaming one later is free (it renames sidecar files and
		// rewrites links), so these only have to be plausible.
		fmt.Fprintf(w, "  %d heading(s) invented — rename freely, it costs nothing:\n", n)
		for i, h := range rep.Headings {
			if h.Source == migrate.HeadingExisting {
				continue
			}
			fmt.Fprintf(w, "    cell %d: ## %s  (%s)\n", i+1, h.Text, h.Source)
		}
	}
	if rep.Failures > 0 {
		fmt.Fprintf(w, "  %d failed result(s) became error blocks\n", rep.Failures)
	}
	if rep.OrphanedOutputs > 0 {
		fmt.Fprintf(w, "  %d orphaned output(s) left as prose "+
			"(prose separated them from their command in v1 too)\n", rep.OrphanedOutputs)
	}
	if rep.DroppedDurations > 0 {
		fmt.Fprintf(w, "  %d duration(s) dropped — the format has no `dur` key\n",
			rep.DroppedDurations)
	}
}

// looksLikeV1 reports whether a file clinote cannot open is a v1 notebook, so the
// refusal can point at the fix. "not a notekit notebook" is true but unhelpful when
// the file is one of your own.
func looksLikeV1(path string) bool {
	src, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if migrate.IsV2(src) {
		return false
	}
	// A v1 cell is an `sh` fence; a v1 result is an `output` fence with `exit=`.
	return strings.Contains(string(src), "\n```sh") ||
		strings.Contains(string(src), "\n```output ")
}
