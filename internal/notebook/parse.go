package notebook

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func Parse(r io.Reader) (*Notebook, error) {
	src, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	nb := &Notebook{Source: src}
	bodyOffset, err := parseFrontMatter(src, &nb.FrontMatter)
	if err != nil {
		return nil, err
	}
	nb.Blocks = parseBlocks(src, bodyOffset)
	return nb, nil
}

// parseFrontMatter detects and parses an optional YAML front matter block.
// Returns the byte offset where the body begins (0 if no front matter).
func parseFrontMatter(src []byte, fm *FrontMatter) (int, error) {
	const open = "---\n"
	if !bytes.HasPrefix(src, []byte(open)) {
		return 0, nil
	}
	yamlStart := len(open)
	pos := yamlStart
	for pos < len(src) {
		if pos+len(open) <= len(src) && bytes.Equal(src[pos:pos+len(open)], []byte(open)) {
			raw := src[yamlStart:pos]
			fm.Present = true
			fm.Raw = raw
			if len(bytes.TrimSpace(raw)) > 0 {
				var data struct {
					Title    string    `yaml:"title"`
					Created  time.Time `yaml:"created"`
					Shell    string    `yaml:"shell"`
					Editable bool      `yaml:"editable"`
					Width    string    `yaml:"width"`
				}
				if err := yaml.Unmarshal(raw, &data); err != nil {
					return 0, fmt.Errorf("front matter yaml: %w", err)
				}
				fm.Title = data.Title
				fm.Created = data.Created
				fm.Shell = data.Shell
				fm.Editable = data.Editable
				fm.Width = data.Width
			}
			return pos + len(open), nil
		}
		nl := bytes.IndexByte(src[pos:], '\n')
		if nl < 0 {
			break
		}
		pos += nl + 1
	}
	return 0, errors.New("front matter started with --- but not closed")
}

// parseBlocks scans the body and emits a linear sequence of blocks.
// Fenced code blocks with language `sh` become CommandBlocks; `output` become
// OutputBlocks. All other content (including fenced blocks with other languages)
// is folded into surrounding ProseBlocks. Whitespace-only gaps between blocks
// are not represented in Blocks — they exist in Source for round-trip but are
// not semantic content, and §2.4 treats them as part of cell pairing.
func parseBlocks(src []byte, bodyOffset int) []Block {
	var blocks []Block
	proseStart := bodyOffset
	pos := bodyOffset

	emitProse := func(end int) {
		if end <= proseStart {
			return
		}
		if isOnlyWhitespace(src[proseStart:end]) {
			return
		}
		blocks = append(blocks, ProseBlock{Start: proseStart, End: end})
	}

	for pos < len(src) {
		lineStart := pos
		lineLen := lineLength(src, pos)
		line := src[lineStart : lineStart+lineLen]

		fenceCount, info, ok := parseOpeningFence(line)
		if !ok {
			pos = lineStart + lineLen
			continue
		}

		// Found an opening fence — locate the closing fence.
		scanPos := lineStart + lineLen
		closeStart := -1
		closeEnd := -1
		for scanPos < len(src) {
			ls := scanPos
			ll := lineLength(src, scanPos)
			if isClosingFence(src[ls:ls+ll], fenceCount) {
				closeStart = ls
				closeEnd = ls + ll
				scanPos += ll
				break
			}
			scanPos += ll
		}
		if closeStart < 0 {
			// Unclosed fence: per CommonMark, the block extends to EOF.
			closeStart = len(src)
			closeEnd = len(src)
			scanPos = len(src)
		}

		lang, attrs := ParseInfoString(info)
		bodyStart := lineStart + lineLen
		bodyEnd := closeStart
		blockEnd := closeEnd

		switch lang {
		case "sh":
			emitProse(lineStart)
			blocks = append(blocks, CommandBlock{
				Start: lineStart, End: blockEnd,
				BodyStart:  bodyStart,
				BodyEnd:    bodyEnd,
				InfoString: suffixAfterLang(info),
				Attrs:      attrs,
			})
			proseStart = blockEnd
		case "output":
			emitProse(lineStart)
			blocks = append(blocks, OutputBlock{
				Start: lineStart, End: blockEnd,
				BodyStart:  bodyStart,
				BodyEnd:    bodyEnd,
				InfoString: suffixAfterLang(info),
				Attrs:      attrs,
			})
			proseStart = blockEnd
		default:
			// Unrecognised language: the entire fenced block stays inside the
			// surrounding prose. Don't close prose, don't open a new block.
		}
		pos = scanPos
	}

	emitProse(len(src))
	return blocks
}

// lineLength returns the number of bytes from pos to the end of its line,
// including the trailing newline if present. The last line of a file with no
// trailing newline returns len(src) - pos.
func lineLength(src []byte, pos int) int {
	end := pos
	for end < len(src) && src[end] != '\n' {
		end++
	}
	if end < len(src) {
		end++
	}
	return end - pos
}

// parseOpeningFence returns the fence char count and info string if line is a
// valid opening backtick fence (≥3 backticks, info string contains no backticks).
func parseOpeningFence(line []byte) (count int, info string, ok bool) {
	l := stripLineEnd(line)
	i := 0
	for i < len(l) && l[i] == '`' {
		i++
	}
	if i < 3 {
		return 0, "", false
	}
	info = strings.TrimSpace(string(l[i:]))
	if strings.ContainsRune(info, '`') {
		return 0, "", false
	}
	return i, info, true
}

// isClosingFence reports whether line is a valid closing fence for an opening
// fence of openCount backticks.
func isClosingFence(line []byte, openCount int) bool {
	l := stripLineEnd(line)
	i := 0
	for i < len(l) && l[i] == '`' {
		i++
	}
	if i < openCount {
		return false
	}
	for j := i; j < len(l); j++ {
		if l[j] != ' ' && l[j] != '\t' {
			return false
		}
	}
	return true
}

func stripLineEnd(line []byte) []byte {
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	return line
}

// suffixAfterLang returns the part of an info string after the language token,
// with leading whitespace stripped. For "sh out=text foo" returns "out=text foo".
func suffixAfterLang(info string) string {
	i := 0
	for i < len(info) && info[i] != ' ' && info[i] != '\t' {
		i++
	}
	for i < len(info) && (info[i] == ' ' || info[i] == '\t') {
		i++
	}
	return info[i:]
}

// Serialize returns the current on-disk representation of the notebook. If no
// edits have been applied since Parse, the result is byte-identical to Source.
func (nb *Notebook) Serialize() []byte {
	if len(nb.edits) == 0 {
		return nb.Source
	}
	// Apply edits in left-to-right order. Edits are assumed non-overlapping.
	// (The rewriter API guarantees this.)
	sorted := make([]edit, len(nb.edits))
	copy(sorted, nb.edits)
	// Simple insertion sort by start offset; edits are few.
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j-1].start > sorted[j].start; j-- {
			sorted[j-1], sorted[j] = sorted[j], sorted[j-1]
		}
	}
	var buf bytes.Buffer
	cursor := 0
	for _, e := range sorted {
		buf.Write(nb.Source[cursor:e.start])
		buf.Write(e.replace)
		cursor = e.end
	}
	buf.Write(nb.Source[cursor:])
	return buf.Bytes()
}
