package render

import (
	"strconv"
	"strings"
)

// sgrState captures the current SGR formatting. The renderer emits a <span> any
// time the state transitions between default and non-default or between two
// distinct non-default states.
type sgrState struct {
	fg, bg    int
	bold      bool
	underline bool
}

func defaultState() sgrState { return sgrState{fg: -1, bg: -1} }

func (s sgrState) isDefault() bool {
	return s == defaultState()
}

func (s sgrState) styleAttr() string {
	var parts []string
	if c := sgrColor(s.fg); c != "" {
		parts = append(parts, "color:"+c)
	}
	if s.bg >= 0 {
		bgCode := s.bg
		if bgCode >= 40 && bgCode <= 47 {
			bgCode -= 10
		} else if bgCode >= 100 && bgCode <= 107 {
			bgCode -= 10
		}
		if c := sgrColor(bgCode); c != "" {
			parts = append(parts, "background-color:"+c)
		}
	}
	if s.bold {
		parts = append(parts, "font-weight:bold")
	}
	if s.underline {
		parts = append(parts, "text-decoration:underline")
	}
	return strings.Join(parts, ";")
}

func sgrColor(n int) string {
	switch n {
	case 30:
		return "#000"
	case 31:
		return "#a00"
	case 32:
		return "#0a0"
	case 33:
		return "#a50"
	case 34:
		return "#00a"
	case 35:
		return "#a0a"
	case 36:
		return "#0aa"
	case 37:
		return "#aaa"
	case 90:
		return "#555"
	case 91:
		return "#f55"
	case 92:
		return "#5f5"
	case 93:
		return "#ff5"
	case 94:
		return "#55f"
	case 95:
		return "#f5f"
	case 96:
		return "#5ff"
	case 97:
		return "#fff"
	}
	return ""
}

// ansiToHTML converts a byte slice containing SGR escape sequences to HTML.
// All non-escape bytes are appended after HTML-escaping `<`, `>`, `&`. Non-SGR
// escape sequences (cursor movement, clear screen) are silently dropped per §7.3.
func ansiToHTML(body []byte) string {
	var out strings.Builder
	state := defaultState()
	spanOpen := false

	flush := func(next sgrState) {
		if next == state {
			return
		}
		if spanOpen {
			out.WriteString("</span>")
			spanOpen = false
		}
		if !next.isDefault() {
			out.WriteString(`<span style="`)
			out.WriteString(next.styleAttr())
			out.WriteString(`">`)
			spanOpen = true
		}
		state = next
	}

	i := 0
	for i < len(body) {
		if body[i] == 0x1b && i+1 < len(body) && body[i+1] == '[' {
			j := i + 2
			for j < len(body) {
				c := body[j]
				if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
					break
				}
				j++
			}
			if j >= len(body) {
				break
			}
			if body[j] == 'm' {
				flush(applySGR(state, string(body[i+2:j])))
			}
			i = j + 1
			continue
		}
		if body[i] == 0x1b {
			i++
			continue
		}
		switch body[i] {
		case '<':
			out.WriteString("&lt;")
		case '>':
			out.WriteString("&gt;")
		case '&':
			out.WriteString("&amp;")
		default:
			out.WriteByte(body[i])
		}
		i++
	}
	if spanOpen {
		out.WriteString("</span>")
	}
	return out.String()
}

func applySGR(s sgrState, params string) sgrState {
	if params == "" {
		return defaultState()
	}
	for _, p := range strings.Split(params, ";") {
		n, err := strconv.Atoi(p)
		if err != nil {
			continue
		}
		switch {
		case n == 0:
			s = defaultState()
		case n == 1:
			s.bold = true
		case n == 4:
			s.underline = true
		case n == 22:
			s.bold = false
		case n == 24:
			s.underline = false
		case n >= 30 && n <= 37, n >= 90 && n <= 97:
			s.fg = n
		case n == 39:
			s.fg = -1
		case n >= 40 && n <= 47, n >= 100 && n <= 107:
			s.bg = n
		case n == 49:
			s.bg = -1
		}
	}
	return s
}
