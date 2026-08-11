package migrate

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pmuston/notekit/doc"
)

// ToolV1 is the provenance written onto migrated results.
//
// Migrated results were produced by clinote v1, and saying otherwise would put a
// false `tool` into a format whose whole point is that the file is the record. The
// original `ran=` timestamp is carried across for the same reason.
const ToolV1 = "clinote/1"

// HeadingSource says where a cell's heading came from, so --dry-run can show the
// user which ones were invented and are worth a better name.
type HeadingSource int

const (
	HeadingExisting HeadingSource = iota // reused a heading already in the file
	HeadingCommand                       // derived from the command's first word
	HeadingOrdinal                       // fell back to "Cell N"
)

func (h HeadingSource) String() string {
	switch h {
	case HeadingExisting:
		return "existing"
	case HeadingCommand:
		return "from command"
	default:
		return "placeholder"
	}
}

// Heading records one cell's heading and where it came from.
type Heading struct {
	Text   string
	Source HeadingSource
}

// Report is what changed, and what could not come across.
type Report struct {
	AlreadyV2 bool

	CellsIn  int
	CellsOut int // counted by re-parsing the output, not by trusting the writer

	Headings []Heading

	DroppedDurations int // `dur=` has no home in the format
	OrphanedOutputs  int // v1 outputs separated from their command by prose
	Failures         int // v1 results with a non-zero exit, now `error` blocks
}

// InventedHeadings counts headings this migration had to make up.
func (r Report) InventedHeadings() int {
	var n int
	for _, h := range r.Headings {
		if h.Source != HeadingExisting {
			n++
		}
	}
	return n
}

// IsV2 reports whether src is already a notekit notebook, making migration a no-op.
func IsV2(src []byte) bool {
	nb, err := doc.Parse(src)
	return err == nil && nb != nil
}

// Convert rewrites a v1 notebook in the notekit format.
//
// It verifies its own work: the output is re-parsed and its cells counted, and a
// mismatch is an error rather than a written file. That check is not belt and
// braces. v2 admits one source fence per heading section and silently treats any
// other as prose, and `notefmt` does not warn about it (notekit#1) — so a migration
// that got the headings wrong would drop cells *and* pass conformance. Counting is
// the only thing standing between that and a quietly gutted notebook.
func Convert(src []byte) ([]byte, Report, error) {
	var rep Report

	if IsV2(src) {
		rep.AlreadyV2 = true
		return src, rep, nil
	}

	v1 := parseV1(src)
	out := &strings.Builder{}
	out.WriteString(frontMatter(v1))

	// A section can own one source fence. Any ATX heading starts a new section
	// (§4.1), but only levels 2–6 can own a cell, so a cell under `# Title` still
	// needs one of its own.
	sectionOwner := 0 // heading level that opened the current section; 0 = none
	sectionTaken := false
	cellNo := 0

	// A command consumes its own result — and the blank lines separating them —
	// by looking ahead, so those blocks must not also be emitted when the loop
	// reaches them. Without this the result lands twice: once converted, once as
	// the original v1 block.
	consumed := map[int]bool{}

	for i, blk := range v1.Blocks {
		if consumed[i] {
			continue
		}
		switch blk.Kind {
		case v1Prose:
			out.WriteString(blk.Text)
			if lvl, text := lastHeading(blk.Text); lvl > 0 {
				sectionOwner, sectionTaken = lvl, false
				_ = text
			}

		case v1Output:
			// Reached only when this output was not consumed with its command,
			// i.e. prose intervened. v1 called it orphaned and never ran it; it
			// stays in the file as prose.
			rep.OrphanedOutputs++
			out.WriteString(blk.Text)

		case v1Command:
			cellNo++
			rep.CellsIn++

			h := chooseHeading(blk, v1.Blocks, i, sectionOwner, sectionTaken, cellNo)
			rep.Headings = append(rep.Headings, h)
			if h.Source != HeadingExisting {
				ensureBlankLine(out)
				out.WriteString("## " + h.Text + "\n\n")
				sectionOwner, sectionTaken = 2, false
			}
			sectionTaken = true

			out.WriteString(sourceFence(blk))

			if j := v1.pairedOutput(i); j >= 0 {
				res, dropped, failed, err := resultBlock(v1.Blocks[j])
				if err != nil {
					return nil, rep, err
				}
				if dropped {
					rep.DroppedDurations++
				}
				if failed {
					rep.Failures++
				}
				out.WriteString("\n" + res)
				// The result and the whitespace that separated it are now
				// written in their v2 form; skip the originals.
				for k := i + 1; k <= j; k++ {
					consumed[k] = true
				}
			}
		}
	}

	got := []byte(out.String())

	// Self-check: parse what we wrote and count. See the doc comment above.
	nb, err := doc.Parse(got)
	if err != nil {
		return nil, rep, fmt.Errorf("migrated notebook does not parse: %w", err)
	}
	rep.CellsOut = len(nb.Cells())
	if rep.CellsOut != rep.CellsIn {
		return nil, rep, fmt.Errorf(
			"refusing to write: %d cells in, %d cells out — migration would lose %d",
			rep.CellsIn, rep.CellsOut, rep.CellsIn-rep.CellsOut)
	}
	return got, rep, nil
}

// frontMatter emits the v2 front matter, preserving v1's keys verbatim.
//
// The existing YAML is passed through as text rather than re-serialised: unknown
// keys survive, and so does whatever formatting the user chose.
func frontMatter(d v1Doc) string {
	var b strings.Builder
	b.WriteString("---\nnotekit: 1\n")
	if d.HasFront {
		body := d.FrontMatter
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		b.WriteString(body)
	}
	if !strings.Contains(d.FrontMatter, "notekit-tool:") {
		b.WriteString("notekit-tool: clinote\n")
	}
	b.WriteString("---\n")
	if len(d.Blocks) > 0 && !strings.HasPrefix(d.Blocks[0].Text, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

// sourceFence renders the v1 command as a v2 source fence, carrying `out=` across
// as `{format=…}`. `text` is the default and is written by omission.
func sourceFence(blk v1Block) string {
	info := "sh"
	if f := blk.Attrs["out"]; f != "" && f != "text" {
		info = "sh {format=" + f + "}"
	}
	body := blk.Body
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	fence := strings.Repeat("`", doc.FenceLen(body))
	return fence + info + "\n" + body + fence + "\n"
}

// resultBlock converts a v1 output block, splitting on exit code: success stays an
// `output`, failure becomes a first-class `error` (§7). v1 wrote both as `output`
// with an `exit` attribute, so a failed run kept a `type` it never really had.
func resultBlock(blk v1Block) (text string, droppedDur, failed bool, err error) {
	rb := doc.ResultBlock{Form: doc.ResultOutput, Body: blk.Body, Tool: ToolV1}

	if _, ok := blk.Attrs["dur"]; ok {
		droppedDur = true
	}
	if _, ok := blk.Attrs["truncated"]; ok {
		rb.Truncated = true
	}
	if ran := blk.Attrs["ran"]; ran != "" {
		if t, perr := time.Parse(time.RFC3339, ran); perr == nil {
			rb.Run = t
		}
	}

	if code, cerr := strconv.Atoi(blk.Attrs["exit"]); cerr == nil && code != 0 {
		rb.Form = doc.ResultError
		rb.Status = &code
		failed = true
	} else if f := blk.Attrs["type"]; f != "" && f != "text" {
		rb.Format = f
	}

	text, err = rb.String()
	return text, droppedDur, failed, err
}

// chooseHeading picks a heading for a cell, preferring one already in the file.
func chooseHeading(blk v1Block, blocks []v1Block, i, owner int, taken bool, n int) Heading {
	if owner >= 2 && owner <= 6 && !taken {
		if lvl, text := lastHeading(precedingProse(blocks, i)); lvl == owner && text != "" {
			return Heading{Text: text, Source: HeadingExisting}
		}
	}
	if name := commandName(blk.Body); name != "" {
		return Heading{Text: name, Source: HeadingCommand}
	}
	return Heading{Text: "Cell " + strconv.Itoa(n), Source: HeadingOrdinal}
}

// precedingProse returns the prose immediately before block i, or "".
//
// Only the immediately preceding block counts: a heading further back belongs to a
// section something else has already claimed.
func precedingProse(blocks []v1Block, i int) string {
	if i > 0 && blocks[i-1].Kind == v1Prose {
		return blocks[i-1].Text
	}
	return ""
}

// lastHeading returns the level and text of the final ATX heading in s.
func lastHeading(s string) (level int, text string) {
	for _, line := range splitLines(s) {
		t := strings.TrimRight(line, "\r\n")
		n := 0
		for n < len(t) && t[n] == '#' {
			n++
		}
		if n == 0 || n > 6 || (n < len(t) && t[n] != ' ') {
			continue
		}
		level, text = n, strings.TrimSpace(strings.TrimLeft(t, "#"))
	}
	return level, text
}

// commandName derives a heading from the command itself — the first word of the
// first line that runs something. A generated name only has to be plausible: v2
// renames a heading without re-running or orphaning anything (§8.1), so improving
// one later costs nothing.
func commandName(body string) string {
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, f := range strings.Fields(line) {
			// Step over leading assignments and `sudo`-alikes.
			if strings.Contains(f, "=") || f == "sudo" || f == "exec" || f == "time" {
				continue
			}
			f = strings.Trim(f, "\"'`$(){}")
			if f == "" {
				continue
			}
			if i := strings.LastIndexByte(f, '/'); i >= 0 && i+1 < len(f) {
				f = f[i+1:] // /usr/bin/du -> du
			}
			return f
		}
	}
	return ""
}

func ensureBlankLine(b *strings.Builder) {
	s := b.String()
	switch {
	case s == "":
	case strings.HasSuffix(s, "\n\n"):
	case strings.HasSuffix(s, "\n"):
		b.WriteString("\n")
	default:
		b.WriteString("\n\n")
	}
}
