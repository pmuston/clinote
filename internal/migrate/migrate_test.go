package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pmuston/notekit/doc"
)

// TestFixturesMigrateAndConform runs every v1 fixture through migration and holds
// the output to the kit's own parser. These are the fixtures v1 was built against,
// so they carry its edge cases: escalated fences, orphaned outputs, bare info
// tokens, unknown front-matter keys, heredocs.
func TestFixturesMigrateAndConform(t *testing.T) {
	entries, err := os.ReadDir("testdata/v1")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("testdata/v1", e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			got, rep, err := Convert(src)
			if err != nil {
				t.Fatalf("convert: %v", err)
			}
			if _, err := doc.Parse(got); err != nil {
				t.Fatalf("output is not a notekit notebook: %v\n%s", err, got)
			}
			if rep.CellsIn != rep.CellsOut {
				t.Errorf("cells in %d, out %d", rep.CellsIn, rep.CellsOut)
			}
		})
	}
}

// Every v1 command must survive as a runnable cell. This is the property the
// self-check enforces, and the one that silently fails without it.
func TestNoCellIsLost(t *testing.T) {
	src := []byte("# Title\n\n```sh\necho one\n```\n\n```sh\necho two\n```\n\n" +
		"prose\n\n```sh\necho three\n```\n")

	got, rep, err := Convert(src)
	if err != nil {
		t.Fatal(err)
	}
	if rep.CellsIn != 3 || rep.CellsOut != 3 {
		t.Fatalf("in %d out %d, want 3 and 3:\n%s", rep.CellsIn, rep.CellsOut, got)
	}
	for _, want := range []string{"echo one", "echo two", "echo three"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("%q missing:\n%s", want, got)
		}
	}
}

// Consecutive cells cannot share a section, so each gets its own heading — this is
// the case a naive migration loses.
func TestConsecutiveCellsEachGetAHeading(t *testing.T) {
	src := []byte("## Setup\n\n```sh\necho one\n```\n\n```sh\necho two\n```\n")
	got, rep, err := Convert(src)
	if err != nil {
		t.Fatal(err)
	}
	if rep.CellsOut != 2 {
		t.Fatalf("want 2 cells, got %d:\n%s", rep.CellsOut, got)
	}
	// The first reuses "Setup"; the second must not.
	if rep.Headings[0].Source != HeadingExisting {
		t.Errorf("first cell should reuse the existing heading, got %v", rep.Headings[0].Source)
	}
	if rep.Headings[1].Source == HeadingExisting {
		t.Errorf("second cell cannot reuse a taken section")
	}
}

// A level-1 heading opens a section that cannot own a cell.
func TestLevelOneHeadingDoesNotOwnACell(t *testing.T) {
	src := []byte("# S88\n\n```sh\ncyq --version\n```\n")
	got, rep, err := Convert(src)
	if err != nil {
		t.Fatal(err)
	}
	if rep.CellsOut != 1 {
		t.Fatalf("want 1 cell, got %d:\n%s", rep.CellsOut, got)
	}
	if rep.Headings[0].Source == HeadingExisting {
		t.Error("a level-1 heading must not be reused as a cell heading")
	}
	if !strings.Contains(string(got), "# S88") {
		t.Error("the original title heading should survive")
	}
}

func TestHeadingFromCommand(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{"cyq --password x --query y\n", "cyq"},
		{"/usr/bin/du -sh .\n", "du"},
		{"# a comment first\ngfig render x\n", "gfig"},
		{"STEM=demo cyq -q x\n", "cyq"},
		{"\n", "Cell 1"},
	} {
		src := []byte("```sh\n" + tc.body + "```\n")
		_, rep, err := Convert(src)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Headings[0].Text != tc.want {
			t.Errorf("body %q: heading %q, want %q", tc.body, rep.Headings[0].Text, tc.want)
		}
	}
}

// Success keeps `output`; failure becomes a first-class `error` with its status.
func TestExitCodeSplitsOutputFromError(t *testing.T) {
	ok := []byte("```sh\ntrue\n```\n\n```output type=text exit=0 ran=2026-07-22T07:40:17Z dur=36ms\nfine\n```\n")
	got, _, err := Convert(ok)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "```output {run=") {
		t.Errorf("success should stay an output block:\n%s", got)
	}

	bad := []byte("```sh\nfalse\n```\n\n```output type=jsonl exit=3 ran=2026-07-22T07:40:17Z dur=36ms\nboom\n```\n")
	got, rep, err := Convert(bad)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "```error {status=3, run=") {
		t.Errorf("failure should become an error block:\n%s", got)
	}
	// A failed run is not jsonl; the stale type must not come across.
	if strings.Contains(string(got), "format=jsonl") {
		t.Errorf("an error block must not carry the source's format:\n%s", got)
	}
	if rep.Failures != 1 {
		t.Errorf("Failures = %d, want 1", rep.Failures)
	}
}

// A paired result is consumed by its command's look-ahead. It must not also be
// emitted when the loop reaches it — which shipped once, putting every result in
// the file twice: converted, then again in its v1 form.
func TestPairedResultIsWrittenOnce(t *testing.T) {
	src := []byte("```sh\ntrue\n```\n\n```output type=text exit=0 ran=2026-07-22T07:40:17Z dur=1ms\nonly once\n```\n")
	got, rep, err := Convert(src)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(got), "only once"); n != 1 {
		t.Errorf("result body appears %d times, want 1:\n%s", n, got)
	}
	// The v1 info string must not survive anywhere.
	if strings.Contains(string(got), "```output type=") {
		t.Errorf("a v1 output block was left in the file:\n%s", got)
	}
	// It was paired, so it is not an orphan.
	if rep.OrphanedOutputs != 0 {
		t.Errorf("OrphanedOutputs = %d, want 0: the result was paired", rep.OrphanedOutputs)
	}
}

func TestProvenanceIsHonest(t *testing.T) {
	src := []byte("```sh\ntrue\n```\n\n```output type=text exit=0 ran=2026-07-22T07:40:17Z dur=36ms\nx\n```\n")
	got, rep, err := Convert(src)
	if err != nil {
		t.Fatal(err)
	}
	// The result was produced by v1 and says so, and keeps its original timestamp.
	if !strings.Contains(string(got), `tool="clinote/1"`) {
		t.Errorf("migrated result should name the tool that produced it:\n%s", got)
	}
	if !strings.Contains(string(got), `run="2026-07-22T07:40:17Z"`) {
		t.Errorf("the original run time should carry across:\n%s", got)
	}
	if rep.DroppedDurations != 1 {
		t.Errorf("DroppedDurations = %d, want 1 (dur= has no home)", rep.DroppedDurations)
	}
}

func TestOutTypeBecomesFormat(t *testing.T) {
	got, _, err := Convert([]byte("```sh out=csv\nprintf 'a,b\\n'\n```\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "```sh {format=csv}") {
		t.Errorf("out=csv should become {format=csv}:\n%s", got)
	}
	// text is the default and is written by omission.
	got, _, err = Convert([]byte("```sh out=text\necho x\n```\n"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "format=text") {
		t.Errorf("out=text should be omitted, not written:\n%s", got)
	}
}

func TestFrontMatterPreservedAndMarked(t *testing.T) {
	src := []byte("---\ntitle: S88\nshell: bash\nrequires:\n - NEO4J_PW\ncustom: kept\n---\n\n```sh\necho x\n```\n")
	got, _, err := Convert(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"notekit: 1", "notekit-tool: clinote",
		"title: S88", "shell: bash", "NEO4J_PW", "custom: kept",
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("front matter lost %q:\n%s", want, got)
		}
	}
}

func TestAlreadyV2IsANoOp(t *testing.T) {
	src := []byte("---\nnotekit: 1\ntitle: T\n---\n\n## A\n\n```sh\necho x\n```\n")
	got, rep, err := Convert(src)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.AlreadyV2 {
		t.Error("should report the notebook is already v2")
	}
	if string(got) != string(src) {
		t.Errorf("a v2 notebook must be returned untouched:\n%s", got)
	}
}

// Migrating twice equals migrating once — the second pass is a no-op.
func TestConvertIsIdempotent(t *testing.T) {
	src := []byte("# T\n\n```sh\necho x\n```\n\n```output type=text exit=0 ran=2026-07-22T07:40:17Z\nx\n```\n")
	once, _, err := Convert(src)
	if err != nil {
		t.Fatal(err)
	}
	twice, rep, err := Convert(once)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.AlreadyV2 || string(once) != string(twice) {
		t.Errorf("second pass changed the file:\n%s", twice)
	}
}

// An output separated from its command by prose was never that command's result in
// v1, and must not silently become one.
func TestOrphanedOutputStaysProse(t *testing.T) {
	src := []byte("```sh\necho x\n```\n\nintervening prose\n\n" +
		"```output type=text exit=0 ran=2026-07-22T07:40:17Z\nstale\n```\n")
	got, rep, err := Convert(src)
	if err != nil {
		t.Fatal(err)
	}
	if rep.OrphanedOutputs != 1 {
		t.Errorf("OrphanedOutputs = %d, want 1", rep.OrphanedOutputs)
	}
	nb, err := doc.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(nb.Cells()); n != 1 {
		t.Fatalf("want 1 cell, got %d", n)
	}
	if len(nb.Cells()[0].Results) != 0 {
		t.Error("the orphaned output must not be adopted as this cell's result")
	}
}

// Fence-length safety has to survive the round trip: a body containing backticks
// needs a longer fence, and doc.ResultBlock computes it.
func TestBacktickBodiesKeepSafeFences(t *testing.T) {
	src, err := os.ReadFile("testdata/v1/12_output_with_five_backticks.md")
	if err != nil {
		t.Skip("fixture missing")
	}
	got, _, err := Convert(src)
	if err != nil {
		t.Fatal(err)
	}
	nb, err := doc.Parse(got)
	if err != nil {
		t.Fatalf("fence safety broken: %v\n%s", err, got)
	}
	if len(nb.Cells()) != 1 {
		t.Errorf("want 1 cell, got %d:\n%s", len(nb.Cells()), got)
	}
}
