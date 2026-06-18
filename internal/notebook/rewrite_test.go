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

func TestSetCommand(t *testing.T) {
	nb := loadFixture(t, "06_command_adjacent_output.md")
	idx := findFirstCommandIdx(t, nb)
	if err := nb.SetCommand(idx, "echo replaced\n"); err != nil {
		t.Fatalf("SetCommand: %v", err)
	}
	out := string(nb.Serialize())
	if !strings.Contains(out, "```sh\necho replaced\n```") {
		t.Errorf("expected new command body, got:\n%s", out)
	}
	if strings.Contains(out, "echo hi") {
		t.Errorf("old command body should be gone:\n%s", out)
	}
	// Info string and pairing preserved: the paired output block should still
	// follow with the same content.
	if !strings.Contains(out, "```output type=text exit=0") {
		t.Errorf("output block should be preserved:\n%s", out)
	}
}

func TestSetCommandWithAttrs(t *testing.T) {
	// Make sure SetCommand on a cell with attrs (e.g., `sh out=csv`) preserves
	// the info string.
	src := "```sh out=csv\nold\n```\n"
	nb, err := Parse(bytes.NewReader([]byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if err := nb.SetCommand(0, "new\n"); err != nil {
		t.Fatal(err)
	}
	out := string(nb.Serialize())
	want := "```sh out=csv\nnew\n```\n"
	if out != want {
		t.Errorf("got %q\nwant %q", out, want)
	}
}

func TestSetCommandErrorsOnNonCommand(t *testing.T) {
	nb := loadFixture(t, "04_prose_only.md")
	if err := nb.SetCommand(0, "x"); err == nil {
		t.Error("expected error on prose block")
	}
}

func TestSetCommandOutType(t *testing.T) {
	// Adds out= when not present.
	src := "```sh\necho hi\n```\n"
	nb, _ := Parse(bytes.NewReader([]byte(src)))
	if err := nb.SetCommandOutType(0, "csv"); err != nil {
		t.Fatal(err)
	}
	if got := string(nb.Serialize()); got != "```sh out=csv\necho hi\n```\n" {
		t.Errorf("got %q", got)
	}

	// Replaces existing out=.
	src2 := "```sh out=csv\necho hi\n```\n"
	nb2, _ := Parse(bytes.NewReader([]byte(src2)))
	if err := nb2.SetCommandOutType(0, "tsv"); err != nil {
		t.Fatal(err)
	}
	if got := string(nb2.Serialize()); got != "```sh out=tsv\necho hi\n```\n" {
		t.Errorf("got %q", got)
	}

	// Removes out= when set to text or empty.
	src3 := "```sh out=csv\necho hi\n```\n"
	nb3, _ := Parse(bytes.NewReader([]byte(src3)))
	if err := nb3.SetCommandOutType(0, "text"); err != nil {
		t.Fatal(err)
	}
	if got := string(nb3.Serialize()); got != "```sh\necho hi\n```\n" {
		t.Errorf("got %q", got)
	}

	// Preserves other attrs (and unknown attrs).
	src4 := "```sh out=text extra=foo unknown\nbody\n```\n"
	nb4, _ := Parse(bytes.NewReader([]byte(src4)))
	if err := nb4.SetCommandOutType(0, "csv"); err != nil {
		t.Fatal(err)
	}
	got := string(nb4.Serialize())
	// out should be first; other attrs preserved (extra, unknown stay).
	if !strings.HasPrefix(got, "```sh out=csv ") {
		t.Errorf("expected out=csv first: %q", got)
	}
	if !strings.Contains(got, "extra=foo") || !strings.Contains(got, "unknown") {
		t.Errorf("other attrs lost: %q", got)
	}
}

func TestSetCommandOutTypePreservesFenceEscalation(t *testing.T) {
	// 4-backtick opening fence (because body contains 3-backtick run) must stay 4.
	src := "````sh\nthis body has ``` inside\n````\n"
	nb, _ := Parse(bytes.NewReader([]byte(src)))
	if err := nb.SetCommandOutType(0, "csv"); err != nil {
		t.Fatal(err)
	}
	got := string(nb.Serialize())
	if !strings.HasPrefix(got, "````sh out=csv\n") {
		t.Errorf("4-backtick fence not preserved: %q", got)
	}
	// Closing fence still 4.
	if !strings.HasSuffix(got, "````\n") {
		t.Errorf("closing fence corrupted: %q", got)
	}
}

func TestAppendProse(t *testing.T) {
	nb := loadFixture(t, "05_command_no_output.md")
	if err := nb.AppendProse("Concluding thoughts.\n"); err != nil {
		t.Fatalf("AppendProse: %v", err)
	}
	out := string(nb.Serialize())
	if !strings.HasSuffix(strings.TrimRight(out, "\n"), "Concluding thoughts.") {
		t.Errorf("prose not appended at end:\n%s", out)
	}
	// Original command preserved.
	if !strings.Contains(out, "```sh\necho hi\n```") {
		t.Errorf("original command lost:\n%s", out)
	}
}

func TestFrontMatterWidth(t *testing.T) {
	src := "---\ntitle: T\nwidth: full\n---\n\nbody\n"
	nb, err := Parse(bytes.NewReader([]byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if nb.FrontMatter.Width != "full" {
		t.Errorf("expected Width=full, got %q", nb.FrontMatter.Width)
	}

	src2 := "---\ntitle: T\n---\n\nbody\n"
	nb2, _ := Parse(bytes.NewReader([]byte(src2)))
	if nb2.FrontMatter.Width != "" {
		t.Errorf("expected empty Width when absent, got %q", nb2.FrontMatter.Width)
	}

	// Round-trip preserves the field.
	if !bytes.Equal(nb.Serialize(), []byte(src)) {
		t.Errorf("round-trip lost width field: %s", nb.Serialize())
	}
}

func TestFrontMatterEditable(t *testing.T) {
	src := "---\ntitle: T\neditable: true\n---\n\nbody\n"
	nb, err := Parse(bytes.NewReader([]byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if !nb.FrontMatter.Editable {
		t.Errorf("expected Editable=true")
	}

	src2 := "---\ntitle: T\n---\n\nbody\n"
	nb2, err := Parse(bytes.NewReader([]byte(src2)))
	if err != nil {
		t.Fatal(err)
	}
	if nb2.FrontMatter.Editable {
		t.Errorf("expected Editable=false when absent")
	}
}

func TestAppendCell(t *testing.T) {
	// Start from prose only.
	nb := loadFixture(t, "04_prose_only.md")
	cmdsBefore, _, _ := countBlocks(nb)
	if cmdsBefore != 0 {
		t.Fatalf("setup: expected 0 commands, got %d", cmdsBefore)
	}
	if err := nb.AppendCell("echo from-append\n"); err != nil {
		t.Fatalf("AppendCell: %v", err)
	}
	cmds, _, _ := countBlocks(nb)
	if cmds != 1 {
		t.Errorf("expected 1 command after append, got %d", cmds)
	}
	out := string(nb.Serialize())
	if !strings.Contains(out, "```sh\necho from-append\n```") {
		t.Errorf("appended cell missing or malformed: %s", out)
	}
	// Original prose must still be there.
	if !strings.Contains(out, "Some prose.") {
		t.Errorf("original prose lost: %s", out)
	}
	// Result should be round-trip stable.
	nb2, err := Parse(bytes.NewReader([]byte(out)))
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if !bytes.Equal([]byte(out), nb2.Serialize()) {
		t.Error("appended notebook not round-trip stable")
	}
}

func TestDeleteBlockProse(t *testing.T) {
	nb := loadFixture(t, "04_prose_only.md")
	idx := -1
	for i, b := range nb.Blocks {
		if _, ok := b.(ProseBlock); ok {
			idx = i
			break
		}
	}
	if err := nb.DeleteBlock(idx); err != nil {
		t.Fatal(err)
	}
	if got := string(nb.Serialize()); got != "" {
		t.Errorf("expected empty notebook after deleting only prose, got %q", got)
	}
}

func TestDeleteBlockCommandRemovesPairedOutput(t *testing.T) {
	nb := loadFixture(t, "06_command_adjacent_output.md")
	idx := findFirstCommandIdx(t, nb)
	if err := nb.DeleteBlock(idx); err != nil {
		t.Fatal(err)
	}
	cmds, outs, _ := countBlocks(nb)
	if cmds != 0 || outs != 0 {
		t.Errorf("expected 0 cmds and 0 outs after deleting paired cell, got cmds=%d outs=%d", cmds, outs)
	}
}

func TestDeleteBlockOrphanLeavesOutputAlone(t *testing.T) {
	// fixture 8: cmd, prose, output (orphan).
	// Deleting the cmd should NOT remove the orphan output (separated by prose).
	nb := loadFixture(t, "08_command_prose_output_orphan.md")
	idx := findFirstCommandIdx(t, nb)
	if err := nb.DeleteBlock(idx); err != nil {
		t.Fatal(err)
	}
	cmds, outs, _ := countBlocks(nb)
	if cmds != 0 {
		t.Errorf("expected 0 cmds, got %d", cmds)
	}
	if outs != 1 {
		t.Errorf("expected orphan output to survive, got outs=%d", outs)
	}
}

func TestDeleteBlockErrorsOnInvalidIdx(t *testing.T) {
	nb := loadFixture(t, "05_command_no_output.md")
	if err := nb.DeleteBlock(-1); err == nil {
		t.Error("expected error on -1")
	}
	if err := nb.DeleteBlock(len(nb.Blocks)); err == nil {
		t.Error("expected error on out-of-range")
	}
}

func TestAppendCellEmptyBody(t *testing.T) {
	nb := loadFixture(t, "01_empty.md")
	if err := nb.AppendCell(""); err != nil {
		t.Fatal(err)
	}
	if got := string(nb.Serialize()); got != "```sh\n```\n" {
		t.Errorf("got %q", got)
	}
}

func TestAppendCellToEmpty(t *testing.T) {
	nb := loadFixture(t, "01_empty.md")
	if err := nb.AppendCell("ls -la\n"); err != nil {
		t.Fatalf("AppendCell: %v", err)
	}
	out := string(nb.Serialize())
	want := "```sh\nls -la\n```\n"
	if out != want {
		t.Errorf("got %q\nwant %q", out, want)
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
