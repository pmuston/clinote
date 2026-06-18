package notebook

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SetOutput replaces or inserts the output block following the CommandBlock at
// blockIdx. If an OutputBlock immediately follows (with whitespace-only between
// per §2.4), it is replaced in place; otherwise a new OutputBlock is inserted
// directly after the command with a single blank line of separation.
//
// On success, the notebook is re-parsed from the new bytes and block indices
// reflect the post-edit state.
func (nb *Notebook) SetOutput(blockIdx int, body string, attrs map[string]string) error {
	if blockIdx < 0 || blockIdx >= len(nb.Blocks) {
		return fmt.Errorf("block index %d out of range", blockIdx)
	}
	cmd, ok := nb.Blocks[blockIdx].(CommandBlock)
	if !ok {
		return fmt.Errorf("block at index %d is not a command block", blockIdx)
	}

	var existing *OutputBlock
	if blockIdx+1 < len(nb.Blocks) {
		if ob, ok := nb.Blocks[blockIdx+1].(OutputBlock); ok {
			if isOnlyWhitespace(nb.Source[cmd.End:ob.Start]) {
				existing = &ob
			}
		}
	}

	rendered := renderOutputBlock(body, attrs)

	var newSrc []byte
	if existing != nil {
		newSrc = spliceBytes(nb.Source, existing.Start, existing.End, rendered)
	} else {
		// Insert directly after the command with a blank line of separation.
		insert := append([]byte{'\n'}, rendered...)
		newSrc = spliceBytes(nb.Source, cmd.End, cmd.End, insert)
	}
	return nb.reparse(newSrc)
}

// SetCommandOutType updates only the `out` attribute on the CommandBlock at
// blockIdx. Other attributes and the body are preserved. An outType of ""
// or "text" removes the `out` attribute (text is the default).
//
// Used by the in-browser type picker so the command's declared hint stays in
// sync with the output block's actual `type=`.
func (nb *Notebook) SetCommandOutType(blockIdx int, outType string) error {
	if blockIdx < 0 || blockIdx >= len(nb.Blocks) {
		return fmt.Errorf("block index %d out of range", blockIdx)
	}
	cb, ok := nb.Blocks[blockIdx].(CommandBlock)
	if !ok {
		return fmt.Errorf("block at index %d is not a command block", blockIdx)
	}
	attrs := make(map[string]string, len(cb.Attrs))
	for k, v := range cb.Attrs {
		attrs[k] = v
	}
	if outType == "" || outType == "text" {
		delete(attrs, "out")
	} else {
		attrs["out"] = outType
	}
	return nb.setCommandAttrs(blockIdx, cb, attrs)
}

// setCommandAttrs rewrites the opening fence line (info string) of the
// CommandBlock at blockIdx using the given attrs. Preserves fence-character
// count and the body.
func (nb *Notebook) setCommandAttrs(blockIdx int, cb CommandBlock, attrs map[string]string) error {
	src := nb.Source
	// Count the leading backticks of the opening fence so we round-trip an
	// escalated fence faithfully (e.g. a 4-backtick fence).
	fenceLen := 0
	for fenceLen < len(src)-cb.Start && src[cb.Start+fenceLen] == '`' {
		fenceLen++
	}
	if fenceLen < 3 {
		return fmt.Errorf("unexpected fence length %d", fenceLen)
	}
	// The opening fence line ends just before BodyStart (which points at the
	// first byte of the body content after the opening fence's newline).
	openLineEnd := cb.BodyStart - 1 // index of the '\n'

	var sb strings.Builder
	sb.Grow(fenceLen + 16)
	for i := 0; i < fenceLen; i++ {
		sb.WriteByte('`')
	}
	sb.WriteString("sh")
	for _, k := range orderedCmdAttrKeys(attrs) {
		sb.WriteByte(' ')
		sb.WriteString(k)
		if v := attrs[k]; v != "" {
			sb.WriteByte('=')
			sb.WriteString(v)
		}
	}
	newSrc := spliceBytes(src, cb.Start, openLineEnd, []byte(sb.String()))
	return nb.reparse(newSrc)
}

// orderedCmdAttrKeys returns attribute keys in a stable order: `out` first
// (most relevant to humans), then any remaining keys alphabetically.
func orderedCmdAttrKeys(attrs map[string]string) []string {
	out := make([]string, 0, len(attrs))
	rest := make([]string, 0, len(attrs))
	for k := range attrs {
		if k == "out" {
			out = append(out, k)
		} else {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// SetCommand replaces the body of the CommandBlock at blockIdx with newBody.
// The info string and fence are preserved. A trailing newline is added if
// newBody doesn't end with one.
func (nb *Notebook) SetCommand(blockIdx int, newBody string) error {
	if blockIdx < 0 || blockIdx >= len(nb.Blocks) {
		return fmt.Errorf("block index %d out of range", blockIdx)
	}
	cb, ok := nb.Blocks[blockIdx].(CommandBlock)
	if !ok {
		return fmt.Errorf("block at index %d is not a command block", blockIdx)
	}
	if !strings.HasSuffix(newBody, "\n") {
		newBody += "\n"
	}
	newSrc := spliceBytes(nb.Source, cb.BodyStart, cb.BodyEnd, []byte(newBody))
	return nb.reparse(newSrc)
}

// AppendProse appends a new prose paragraph to the end of the notebook.
// Used by the in-browser "+ prose" button. An empty text is allowed for the
// open-in-editor flow.
func (nb *Notebook) AppendProse(text string) error {
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	sep := ""
	src := nb.Source
	if len(src) > 0 {
		switch {
		case !bytes.HasSuffix(src, []byte("\n")):
			sep = "\n\n"
		case !bytes.HasSuffix(src, []byte("\n\n")):
			sep = "\n"
		}
	}
	newSrc := append([]byte{}, src...)
	newSrc = append(newSrc, sep...)
	newSrc = append(newSrc, text...)
	return nb.reparse(newSrc)
}

// AppendCell appends a new sh command block to the end of the notebook with
// the given body. A blank line of separation is inserted if needed so the new
// fence starts at the beginning of a line. The notebook is re-parsed after
// the splice. An empty body is allowed and produces a fence with no command
// lines — useful when the caller is about to open the editor on the new cell.
func (nb *Notebook) AppendCell(body string) error {
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	cell := []byte("```sh\n" + body + "```\n")

	sep := ""
	src := nb.Source
	if len(src) > 0 {
		switch {
		case !bytes.HasSuffix(src, []byte("\n")):
			sep = "\n\n"
		case !bytes.HasSuffix(src, []byte("\n\n")):
			sep = "\n"
		}
	}
	newSrc := append([]byte{}, src...)
	newSrc = append(newSrc, sep...)
	newSrc = append(newSrc, cell...)
	return nb.reparse(newSrc)
}

// DeleteBlock removes the block at blockIdx from the notebook. If the block
// is a CommandBlock paired with an immediately-following OutputBlock (only
// whitespace between), the output is removed too so the file doesn't end up
// with an orphaned output. The notebook is re-parsed after the splice.
func (nb *Notebook) DeleteBlock(blockIdx int) error {
	if blockIdx < 0 || blockIdx >= len(nb.Blocks) {
		return fmt.Errorf("block index %d out of range", blockIdx)
	}
	start, end := nb.Blocks[blockIdx].Span()
	if cb, ok := nb.Blocks[blockIdx].(CommandBlock); ok && blockIdx+1 < len(nb.Blocks) {
		if ob, ok := nb.Blocks[blockIdx+1].(OutputBlock); ok {
			if isOnlyWhitespace(nb.Source[cb.End:ob.Start]) {
				end = ob.End
			}
		}
	}
	newSrc := make([]byte, 0, len(nb.Source)-(end-start))
	newSrc = append(newSrc, nb.Source[:start]...)
	newSrc = append(newSrc, nb.Source[end:]...)
	return nb.reparse(newSrc)
}

// SetProse replaces the bytes of the ProseBlock at blockIdx with newText.
// newText is taken verbatim — the caller is responsible for trailing newlines.
func (nb *Notebook) SetProse(blockIdx int, newText string) error {
	if blockIdx < 0 || blockIdx >= len(nb.Blocks) {
		return fmt.Errorf("block index %d out of range", blockIdx)
	}
	pb, ok := nb.Blocks[blockIdx].(ProseBlock)
	if !ok {
		return fmt.Errorf("block at index %d is not a prose block", blockIdx)
	}
	newSrc := spliceBytes(nb.Source, pb.Start, pb.End, []byte(newText))
	return nb.reparse(newSrc)
}

// WriteFile writes the current notebook bytes to path atomically.
func (nb *Notebook) WriteFile(path string) error {
	data := nb.Serialize()
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".clinote-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

func (nb *Notebook) reparse(newSrc []byte) error {
	parsed, err := Parse(bytes.NewReader(newSrc))
	if err != nil {
		return err
	}
	nb.Source = parsed.Source
	nb.FrontMatter = parsed.FrontMatter
	nb.Blocks = parsed.Blocks
	nb.edits = nil
	return nil
}

func spliceBytes(src []byte, start, end int, replacement []byte) []byte {
	out := make([]byte, 0, len(src)-end+start+len(replacement))
	out = append(out, src[:start]...)
	out = append(out, replacement...)
	out = append(out, src[end:]...)
	return out
}

func isOnlyWhitespace(b []byte) bool {
	for _, c := range b {
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			return false
		}
	}
	return true
}

// renderOutputBlock formats an output block ready to splice into a notebook.
// The fence length is one greater than the longest run of backticks in body
// (minimum 3). Attributes are written in stable order: type, exit, ran, dur,
// truncated, then any remaining keys alphabetically. A bare key (empty value)
// renders without `=`; otherwise as `k=v`.
func renderOutputBlock(body string, attrs map[string]string) []byte {
	fenceLen := maxBacktickRun(body) + 1
	if fenceLen < 3 {
		fenceLen = 3
	}
	fence := strings.Repeat("`", fenceLen)

	var buf bytes.Buffer
	buf.WriteString(fence)
	buf.WriteString("output")
	for _, k := range orderedAttrKeys(attrs) {
		buf.WriteByte(' ')
		buf.WriteString(k)
		if v := attrs[k]; v != "" {
			buf.WriteByte('=')
			buf.WriteString(v)
		}
	}
	buf.WriteByte('\n')
	buf.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		buf.WriteByte('\n')
	}
	buf.WriteString(fence)
	buf.WriteByte('\n')
	return buf.Bytes()
}

func maxBacktickRun(s string) int {
	max, cur := 0, 0
	for i := 0; i < len(s); i++ {
		if s[i] == '`' {
			cur++
			if cur > max {
				max = cur
			}
		} else {
			cur = 0
		}
	}
	return max
}

func orderedAttrKeys(attrs map[string]string) []string {
	stable := []string{"type", "exit", "ran", "dur", "truncated"}
	out := make([]string, 0, len(attrs))
	seen := map[string]bool{}
	for _, k := range stable {
		if _, ok := attrs[k]; ok {
			out = append(out, k)
			seen[k] = true
		}
	}
	rest := make([]string, 0)
	for k := range attrs {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}
