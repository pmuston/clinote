package notebook

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRoundTrip walks every fixture in testdata/ and asserts that a parsed-then-
// serialized notebook is byte-identical to the original source. This is the
// load-bearing invariant for the rewriter (§4.4).
func TestRoundTrip(t *testing.T) {
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			path := filepath.Join("testdata", e.Name())
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			nb, err := Parse(bytes.NewReader(src))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got := nb.Serialize()
			if !bytes.Equal(src, got) {
				t.Fatalf("round-trip mismatch\n--- want (%d bytes) ---\n%s\n--- got (%d bytes) ---\n%s",
					len(src), src, len(got), got)
			}
		})
	}
}

func loadFixture(t *testing.T, name string) *Notebook {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	nb, err := Parse(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return nb
}

func countBlocks(nb *Notebook) (cmds, outs, prose int) {
	for _, b := range nb.Blocks {
		switch b.(type) {
		case CommandBlock:
			cmds++
		case OutputBlock:
			outs++
		case ProseBlock:
			prose++
		}
	}
	return
}

func TestEmpty(t *testing.T) {
	nb := loadFixture(t, "01_empty.md")
	if nb.FrontMatter.Present {
		t.Error("expected no front matter")
	}
	if len(nb.Blocks) != 0 {
		t.Errorf("expected 0 blocks, got %d", len(nb.Blocks))
	}
}

func TestFrontMatterOnly(t *testing.T) {
	nb := loadFixture(t, "02_frontmatter_only.md")
	if !nb.FrontMatter.Present {
		t.Fatal("expected front matter present")
	}
	if nb.FrontMatter.Title != "Just front matter" {
		t.Errorf("title = %q", nb.FrontMatter.Title)
	}
	if nb.FrontMatter.Shell != "bash" {
		t.Errorf("shell = %q", nb.FrontMatter.Shell)
	}
}

func TestBodyOnly(t *testing.T) {
	nb := loadFixture(t, "03_body_only.md")
	if nb.FrontMatter.Present {
		t.Error("expected no front matter")
	}
	_, _, prose := countBlocks(nb)
	if prose != 1 {
		t.Errorf("expected 1 prose block, got %d", prose)
	}
}

func TestCommandNoOutput(t *testing.T) {
	nb := loadFixture(t, "05_command_no_output.md")
	cmds, outs, _ := countBlocks(nb)
	if cmds != 1 || outs != 0 {
		t.Errorf("cmds=%d outs=%d", cmds, outs)
	}
}

func TestCommandAdjacentOutput(t *testing.T) {
	nb := loadFixture(t, "06_command_adjacent_output.md")
	// Expect: CommandBlock, OutputBlock, optional trailing ProseBlock.
	// Adjacent means no prose between them.
	var idxCmd, idxOut = -1, -1
	for i, b := range nb.Blocks {
		switch b.(type) {
		case CommandBlock:
			idxCmd = i
		case OutputBlock:
			idxOut = i
		}
	}
	if idxCmd < 0 || idxOut < 0 {
		t.Fatalf("missing blocks: cmd=%d out=%d", idxCmd, idxOut)
	}
	if idxOut != idxCmd+1 {
		t.Errorf("expected output directly after command, got cmd=%d out=%d", idxCmd, idxOut)
	}
}

func TestOrphanedOutput(t *testing.T) {
	// Fixture 8: command, prose, output — pairing must NOT connect them.
	// Structurally: CommandBlock followed by a ProseBlock followed by an OutputBlock.
	nb := loadFixture(t, "08_command_prose_output_orphan.md")
	cmds, outs, prose := countBlocks(nb)
	if cmds != 1 || outs != 1 || prose < 1 {
		t.Fatalf("expected 1 cmd, 1 out, ≥1 prose; got cmds=%d outs=%d prose=%d", cmds, outs, prose)
	}
	// Verify a ProseBlock sits between command and output (pairing fails).
	var cmdIdx, outIdx = -1, -1
	for i, b := range nb.Blocks {
		switch b.(type) {
		case CommandBlock:
			cmdIdx = i
		case OutputBlock:
			outIdx = i
		}
	}
	if !(cmdIdx < outIdx) {
		t.Fatalf("expected cmd before output: cmd=%d out=%d", cmdIdx, outIdx)
	}
	hasIntervening := false
	for i := cmdIdx + 1; i < outIdx; i++ {
		if _, ok := nb.Blocks[i].(ProseBlock); ok {
			hasIntervening = true
		}
	}
	if !hasIntervening {
		t.Error("expected intervening prose between command and orphaned output")
	}
}

func TestTwoCommandsOutputBetween(t *testing.T) {
	nb := loadFixture(t, "09_two_commands_output_between.md")
	cmds, outs, _ := countBlocks(nb)
	if cmds != 2 || outs != 1 {
		t.Errorf("cmds=%d outs=%d", cmds, outs)
	}
}

func TestPythonBlockTreatedAsProse(t *testing.T) {
	nb := loadFixture(t, "10_python_between_commands.md")
	cmds, outs, _ := countBlocks(nb)
	if cmds != 2 {
		t.Errorf("expected 2 sh commands, got %d", cmds)
	}
	if outs != 0 {
		t.Errorf("expected 0 output blocks, got %d", outs)
	}
}

func TestOutputWithTripleBackticks(t *testing.T) {
	nb := loadFixture(t, "11_output_with_triple_backticks.md")
	_, outs, _ := countBlocks(nb)
	if outs != 1 {
		t.Fatalf("expected 1 output block, got %d", outs)
	}
	var ob OutputBlock
	for _, b := range nb.Blocks {
		if o, ok := b.(OutputBlock); ok {
			ob = o
		}
	}
	body := ob.Body(nb.Source)
	if !strings.Contains(body, "```") {
		t.Errorf("output body should contain ```, got %q", body)
	}
}

func TestOutputWithFiveBackticks(t *testing.T) {
	nb := loadFixture(t, "12_output_with_five_backticks.md")
	_, outs, _ := countBlocks(nb)
	if outs != 1 {
		t.Fatalf("expected 1 output block, got %d", outs)
	}
}

func TestFrontMatterUnknownFieldPreserved(t *testing.T) {
	nb := loadFixture(t, "13_frontmatter_unknown_field.md")
	if !nb.FrontMatter.Present {
		t.Fatal("expected front matter")
	}
	raw := string(nb.FrontMatter.Raw)
	if !strings.Contains(raw, "custom_field: preserved-on-round-trip") {
		t.Errorf("expected unknown field in raw front matter, got: %q", raw)
	}
	if !strings.Contains(raw, "tags:") {
		t.Errorf("expected tags field in raw front matter, got: %q", raw)
	}
	if nb.FrontMatter.Shell != "zsh" {
		t.Errorf("shell = %q", nb.FrontMatter.Shell)
	}
}

func TestBareInfoToken(t *testing.T) {
	nb := loadFixture(t, "14_bare_info_token.md")
	cmds, _, _ := countBlocks(nb)
	if cmds != 1 {
		t.Fatalf("expected 1 command, got %d", cmds)
	}
	var cb CommandBlock
	for _, b := range nb.Blocks {
		if c, ok := b.(CommandBlock); ok {
			cb = c
		}
	}
	v, ok := cb.Attrs["out"]
	if !ok {
		t.Fatalf("expected attr `out` present; got %v", cb.Attrs)
	}
	if v != "" {
		t.Errorf("expected empty value for bare token, got %q", v)
	}
}

func TestMultilineCommand(t *testing.T) {
	nb := loadFixture(t, "15_multiline_command.md")
	cmds, _, _ := countBlocks(nb)
	if cmds != 1 {
		t.Fatalf("expected 1 command, got %d", cmds)
	}
	var cb CommandBlock
	for _, b := range nb.Blocks {
		if c, ok := b.(CommandBlock); ok {
			cb = c
		}
	}
	body := cb.Body(nb.Source)
	if !strings.Contains(body, "<<EOF") || !strings.Contains(body, "tr '[:lower:]'") {
		t.Errorf("multiline body not captured: %q", body)
	}
}
