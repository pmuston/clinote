package notebook

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func findFirstCommandIdx(t *testing.T, nb *Notebook) int {
	t.Helper()
	for i, b := range nb.Blocks {
		if _, ok := b.(CommandBlock); ok {
			return i
		}
	}
	t.Fatal("no command block found")
	return -1
}

func standardAttrs() map[string]string {
	return map[string]string{
		"type": "text",
		"exit": "0",
		"ran":  "2026-05-26T14:31:12Z",
		"dur":  "0.1s",
	}
}

// SetOutput on a command that already has output: output is replaced,
// surrounding bytes unchanged.
func TestSetOutputReplacesAdjacent(t *testing.T) {
	nb := loadFixture(t, "06_command_adjacent_output.md")
	idx := findFirstCommandIdx(t, nb)
	if err := nb.SetOutput(idx, "REPLACED\n", standardAttrs()); err != nil {
		t.Fatalf("SetOutput: %v", err)
	}
	out := string(nb.Serialize())
	if !strings.Contains(out, "REPLACED") {
		t.Errorf("expected REPLACED in output, got:\n%s", out)
	}
	if strings.Contains(out, "\nhi\n") {
		t.Errorf("old output 'hi' should have been replaced; got:\n%s", out)
	}
	// Exactly one output block remains.
	_, outs, _ := countBlocks(nb)
	if outs != 1 {
		t.Errorf("expected 1 output block after replace, got %d", outs)
	}
}

func TestSetOutputReplacesBlankSeparated(t *testing.T) {
	nb := loadFixture(t, "07_command_blank_then_output.md")
	idx := findFirstCommandIdx(t, nb)
	if err := nb.SetOutput(idx, "REPLACED\n", standardAttrs()); err != nil {
		t.Fatalf("SetOutput: %v", err)
	}
	out := string(nb.Serialize())
	if !strings.Contains(out, "REPLACED") {
		t.Errorf("expected REPLACED, got:\n%s", out)
	}
	_, outs, _ := countBlocks(nb)
	if outs != 1 {
		t.Errorf("expected 1 output, got %d", outs)
	}
}

// SetOutput on a command with no following output inserts with a blank line.
func TestSetOutputInsertsAfterCommand(t *testing.T) {
	nb := loadFixture(t, "05_command_no_output.md")
	idx := findFirstCommandIdx(t, nb)
	if err := nb.SetOutput(idx, "first output\n", standardAttrs()); err != nil {
		t.Fatalf("SetOutput: %v", err)
	}
	_, outs, _ := countBlocks(nb)
	if outs != 1 {
		t.Errorf("expected 1 output after insert, got %d", outs)
	}
	out := string(nb.Serialize())
	if !strings.Contains(out, "first output") {
		t.Errorf("expected inserted output in source, got:\n%s", out)
	}

	// Verify pairing: command and new output are now adjacent (whitespace-only between).
	var cmdEnd, outStart int = -1, -1
	for _, b := range nb.Blocks {
		switch x := b.(type) {
		case CommandBlock:
			cmdEnd = x.End
		case OutputBlock:
			if outStart < 0 {
				outStart = x.Start
			}
		}
	}
	if cmdEnd < 0 || outStart < 0 {
		t.Fatalf("missing blocks; cmdEnd=%d outStart=%d", cmdEnd, outStart)
	}
	if !isOnlyWhitespace(nb.Source[cmdEnd:outStart]) {
		t.Errorf("expected whitespace-only between cmd and output, got %q", nb.Source[cmdEnd:outStart])
	}
}

// SetOutput on a command followed by prose (orphan case) inserts a new paired
// output BETWEEN the command and the prose. The orphaned output stays put.
func TestSetOutputInsertsBetweenCommandAndProse(t *testing.T) {
	nb := loadFixture(t, "08_command_prose_output_orphan.md")
	idx := findFirstCommandIdx(t, nb)
	if err := nb.SetOutput(idx, "fresh\n", standardAttrs()); err != nil {
		t.Fatalf("SetOutput: %v", err)
	}
	cmds, outs, _ := countBlocks(nb)
	if cmds != 1 {
		t.Errorf("expected 1 cmd, got %d", cmds)
	}
	if outs != 2 {
		t.Errorf("expected 2 outputs (new + orphan), got %d", outs)
	}

	// The first output (immediately after the command) must contain "fresh".
	var firstCmdIdx int = -1
	for i, b := range nb.Blocks {
		if _, ok := b.(CommandBlock); ok {
			firstCmdIdx = i
			break
		}
	}
	next, ok := nb.Blocks[firstCmdIdx+1].(OutputBlock)
	if !ok {
		t.Fatalf("expected OutputBlock right after command at idx %d, got %T", firstCmdIdx+1, nb.Blocks[firstCmdIdx+1])
	}
	body := next.Body(nb.Source)
	if !strings.Contains(body, "fresh") {
		t.Errorf("expected 'fresh' in new output body, got %q", body)
	}
}

func TestSetProseReplacesOnlyProseBytes(t *testing.T) {
	nb := loadFixture(t, "04_prose_only.md")
	// Find first prose block.
	idx := -1
	for i, b := range nb.Blocks {
		if _, ok := b.(ProseBlock); ok {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("no prose block")
	}
	if err := nb.SetProse(idx, "Replaced prose.\n"); err != nil {
		t.Fatalf("SetProse: %v", err)
	}
	out := string(nb.Serialize())
	if out != "Replaced prose.\n" {
		t.Errorf("got %q", out)
	}
}

func TestSetProseAroundCommands(t *testing.T) {
	// fixture 5 has prose-then-command. Replacing prose shouldn't disturb the command.
	nb := loadFixture(t, "05_command_no_output.md")
	idx := -1
	for i, b := range nb.Blocks {
		if _, ok := b.(ProseBlock); ok {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("no prose block")
	}
	if err := nb.SetProse(idx, "New prose.\n\n"); err != nil {
		t.Fatalf("SetProse: %v", err)
	}
	out := string(nb.Serialize())
	if !strings.HasPrefix(out, "New prose.\n\n") {
		t.Errorf("prose not replaced at expected position: %q", out)
	}
	if !strings.Contains(out, "```sh\necho hi\n```") {
		t.Errorf("command body should be unchanged, got:\n%s", out)
	}
}

func TestSetOutputErrorsOnNonCommand(t *testing.T) {
	nb := loadFixture(t, "04_prose_only.md")
	if err := nb.SetOutput(0, "x", standardAttrs()); err == nil {
		t.Error("expected error setting output on prose block")
	}
}

func TestSetProseErrorsOnNonProse(t *testing.T) {
	nb := loadFixture(t, "05_command_no_output.md")
	cmdIdx := findFirstCommandIdx(t, nb)
	if err := nb.SetProse(cmdIdx, "x"); err == nil {
		t.Error("expected error setting prose on command block")
	}
}

func TestRenderOutputBlockFenceEscalation(t *testing.T) {
	// Body with 3 backticks → fence must be 4.
	body := "before\n```\ninner\n```\nafter\n"
	out := string(renderOutputBlock(body, standardAttrs()))
	if !strings.HasPrefix(out, "````output ") {
		t.Errorf("expected 4-backtick fence, got prefix: %q", out[:20])
	}
	// Body with 5 backticks → fence must be 6.
	body6 := "x `````\n"
	out6 := string(renderOutputBlock(body6, standardAttrs()))
	if !strings.HasPrefix(out6, "``````output ") {
		t.Errorf("expected 6-backtick fence, got prefix: %q", out6[:20])
	}
	// Empty body still gets minimum fence of 3.
	out0 := string(renderOutputBlock("", standardAttrs()))
	if !strings.HasPrefix(out0, "```output ") {
		t.Errorf("expected 3-backtick fence on empty body, got prefix: %q", out0)
	}
}

func TestRenderOutputBlockAttrOrder(t *testing.T) {
	attrs := map[string]string{
		"zebra":     "z",
		"dur":       "1s",
		"alpha":     "a",
		"exit":      "0",
		"type":      "text",
		"ran":       "2026-05-26T14:31:12Z",
		"truncated": "true",
	}
	out := string(renderOutputBlock("body\n", attrs))
	// Find the info line.
	line := out[:strings.IndexByte(out, '\n')]
	// Expected order: type, exit, ran, dur, truncated, alpha, zebra
	wantOrder := []string{"type=text", "exit=0", "ran=2026-05-26T14:31:12Z", "dur=1s", "truncated=true", "alpha=a", "zebra=z"}
	prev := 0
	for _, frag := range wantOrder {
		i := strings.Index(line, frag)
		if i < 0 {
			t.Errorf("missing %q in info line: %q", frag, line)
			continue
		}
		if i < prev {
			t.Errorf("attribute %q appears before previous attr (line: %q)", frag, line)
		}
		prev = i
	}
}

func TestRenderOutputBlockBareKey(t *testing.T) {
	attrs := map[string]string{"type": "text", "exit": "0", "ran": "2026-05-26T14:31:12Z", "dur": "1s", "barekey": ""}
	out := string(renderOutputBlock("x\n", attrs))
	line := out[:strings.IndexByte(out, '\n')]
	if !strings.Contains(line, " barekey") {
		t.Errorf("expected bare key in info line, got: %q", line)
	}
	if strings.Contains(line, "barekey=") {
		t.Errorf("bare key should not render with =: %q", line)
	}
}

func TestWriteFileAtomic(t *testing.T) {
	nb := loadFixture(t, "06_command_adjacent_output.md")
	idx := findFirstCommandIdx(t, nb)
	if err := nb.SetOutput(idx, "atomic\n", standardAttrs()); err != nil {
		t.Fatalf("SetOutput: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "out.md")
	if err := nb.WriteFile(path); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !bytes.Equal(got, nb.Serialize()) {
		t.Errorf("on-disk bytes differ from Serialize()")
	}
	// No leftover temp files.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".clinote-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

// After SetOutput, Serialize should still produce well-formed bytes that
// re-parse cleanly (round-trip preserves the new state).
func TestSetOutputThenRoundTrip(t *testing.T) {
	nb := loadFixture(t, "05_command_no_output.md")
	idx := findFirstCommandIdx(t, nb)
	if err := nb.SetOutput(idx, "round\n", standardAttrs()); err != nil {
		t.Fatalf("SetOutput: %v", err)
	}
	bytes1 := nb.Serialize()
	nb2, err := Parse(bytes.NewReader(bytes1))
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	bytes2 := nb2.Serialize()
	if !bytes.Equal(bytes1, bytes2) {
		t.Errorf("post-edit serialization is not round-trip stable")
	}
}
