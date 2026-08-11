// Package migrate converts clinote v1 notebooks to the notekit format.
//
// The reader here is deliberately one-way and much smaller than v1's parser was.
// v1 needed byte-identical round-trip, so it carried splice and rewrite machinery;
// migration only ever reads, and everything it does not recognise passes through as
// prose. That is also why this package can be deleted once v1 notebooks are gone,
// rather than becoming a second format implementation to keep in step.
package migrate

import (
	"bytes"
	"strings"
)

// v1Kind classifies a run of source bytes.
type v1Kind int

const (
	v1Prose v1Kind = iota
	v1Command
	v1Output
)

// v1Block is one run of v1 source: prose, an `sh` cell, or an `output` block.
type v1Block struct {
	Kind v1Kind
	Text string // the whole block including fences, for prose passthrough

	// Fenced blocks only.
	Attrs map[string]string // parsed info-string attributes
	Body  string            // fence contents, excluding the fence lines
}

// v1Doc is a parsed v1 notebook.
type v1Doc struct {
	FrontMatter string // the YAML between the --- lines, empty if absent
	HasFront    bool
	Blocks      []v1Block
}

// parseV1 scans a v1 notebook.
//
// v1's rules: an optional `---` front matter, then fenced blocks tagged `sh` or
// `output` are cells and results; every other byte — including fenced blocks in
// other languages — is prose.
func parseV1(src []byte) v1Doc {
	var d v1Doc
	body := src

	if bytes.HasPrefix(src, []byte("---\n")) {
		if end := bytes.Index(src[4:], []byte("\n---\n")); end >= 0 {
			d.HasFront = true
			d.FrontMatter = string(src[4 : 4+end+1])
			body = src[4+end+len("\n---\n"):]
		}
	}

	lines := splitLines(string(body))
	var prose strings.Builder
	flushProse := func() {
		if prose.Len() > 0 {
			d.Blocks = append(d.Blocks, v1Block{Kind: v1Prose, Text: prose.String()})
			prose.Reset()
		}
	}

	for i := 0; i < len(lines); i++ {
		fence, lang, info, ok := openingFence(lines[i])
		if !ok || (lang != "sh" && lang != "output") {
			prose.WriteString(lines[i])
			continue
		}

		// Find the closing fence. An unclosed fence runs to EOF, matching v1.
		close := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if closingFence(lines[j], fence) {
				close = j
				break
			}
		}

		flushProse()
		blk := v1Block{
			Attrs: parseV1Info(info),
			Body:  strings.Join(lines[i+1:min(close, len(lines))], ""),
		}
		if lang == "sh" {
			blk.Kind = v1Command
		} else {
			blk.Kind = v1Output
		}
		end := min(close+1, len(lines))
		blk.Text = strings.Join(lines[i:end], "")
		d.Blocks = append(d.Blocks, blk)
		i = close
	}
	flushProse()
	return d
}

// splitLines keeps line terminators, so prose passes through byte-for-byte.
func splitLines(s string) []string {
	var out []string
	for len(s) > 0 {
		n := strings.IndexByte(s, '\n')
		if n < 0 {
			out = append(out, s)
			break
		}
		out = append(out, s[:n+1])
		s = s[n+1:]
	}
	return out
}

// openingFence reports a backtick fence, its length, language and remaining info.
func openingFence(line string) (fence int, lang, info string, ok bool) {
	t := strings.TrimRight(line, "\r\n")
	n := 0
	for n < len(t) && t[n] == '`' {
		n++
	}
	if n < 3 {
		return 0, "", "", false
	}
	rest := strings.TrimSpace(t[n:])
	if strings.ContainsRune(rest, '`') {
		return 0, "", "", false
	}
	lang, info, _ = strings.Cut(rest, " ")
	return n, lang, strings.TrimSpace(info), true
}

func closingFence(line string, open int) bool {
	t := strings.TrimRight(line, "\r\n")
	n := 0
	for n < len(t) && t[n] == '`' {
		n++
	}
	return n >= open && strings.TrimSpace(t[n:]) == ""
}

// parseV1Info parses v1's bare `key=value` info string. A token with no `=` is a
// flag, stored with an empty value, which is how v1 recorded `truncated`.
func parseV1Info(info string) map[string]string {
	attrs := map[string]string{}
	for _, tok := range strings.Fields(info) {
		k, v, found := strings.Cut(tok, "=")
		if !found {
			attrs[k] = ""
			continue
		}
		attrs[k] = v
	}
	return attrs
}

// pairedOutput reports the index of the output block that belongs to the command at
// i, or -1. v1's rule: an output belongs to the command above it when only
// whitespace separates them.
func (d v1Doc) pairedOutput(i int) int {
	for j := i + 1; j < len(d.Blocks); j++ {
		switch d.Blocks[j].Kind {
		case v1Output:
			return j
		case v1Prose:
			if strings.TrimSpace(d.Blocks[j].Text) == "" {
				continue // blank lines keep the pairing
			}
			return -1
		default:
			return -1
		}
	}
	return -1
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
