// Package render converts output-block bodies into safe HTML for the browser.
// Text output gets ANSI-to-HTML conversion; CSV and JSONL render as sortable
// tables. Each renderer escapes user content.
package render

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"sort"
	"strconv"
	"strings"
)

// MaxRenderRows caps the rows rendered into the browser table for CSV/JSONL
// (§7.4). The full data remains in the .md file.
const MaxRenderRows = 1000

// Output renders body as HTML, choosing by declaredType, or sniffing if empty.
func Output(body []byte, declaredType string) template.HTML {
	typ := declaredType
	if typ == "" {
		typ = Sniff(body)
	}
	switch typ {
	case "csv":
		return CSV(body)
	case "jsonl":
		return JSONL(body)
	default:
		return Text(body)
	}
}

// Sniff infers the output type from body per §7.6.
func Sniff(body []byte) string {
	lines := nonEmptyLines(body)
	if len(lines) == 0 {
		return "text"
	}
	allJSONObjects := true
	for _, l := range lines {
		// JSONL is only useful for tabulation when every row is an object.
		if !looksLikeJSONObject(l) {
			allJSONObjects = false
			break
		}
		var obj map[string]any
		if err := json.Unmarshal(l, &obj); err != nil {
			allJSONObjects = false
			break
		}
	}
	if allJSONObjects {
		return "jsonl"
	}
	if len(lines) >= 2 && isConsistentCSV(lines) {
		return "csv"
	}
	return "text"
}

func nonEmptyLines(body []byte) [][]byte {
	raw := bytes.Split(body, []byte("\n"))
	out := raw[:0]
	for _, l := range raw {
		if len(bytes.TrimSpace(l)) > 0 {
			out = append(out, l)
		}
	}
	return out
}

func looksLikeJSONObject(l []byte) bool {
	trimmed := bytes.TrimSpace(l)
	return len(trimmed) >= 2 && trimmed[0] == '{' && trimmed[len(trimmed)-1] == '}'
}

func isConsistentCSV(lines [][]byte) bool {
	var cols int
	for i, l := range lines {
		rec, err := csv.NewReader(bytes.NewReader(l)).Read()
		if err != nil {
			return false
		}
		if i == 0 {
			if len(rec) < 2 {
				return false
			}
			cols = len(rec)
		} else if len(rec) != cols {
			return false
		}
	}
	return true
}

// Text renders plain text output, converting ANSI SGR sequences to HTML spans
// and escaping the rest. The output is meant to live inside <pre>.
func Text(body []byte) template.HTML {
	return template.HTML(ansiToHTML(body))
}

// CSV renders a CSV body as a sortable HTML table. The first row is the header;
// rows beyond MaxRenderRows are dropped with a notice appended.
func CSV(body []byte) template.HTML {
	rec, err := csv.NewReader(bytes.NewReader(body)).ReadAll()
	if err != nil || len(rec) == 0 {
		return Text(body)
	}
	header := rec[0]
	return renderTable(header, rec[1:], len(rec)-1)
}

// JSONL renders a JSONL body. Columns are the union of top-level keys, sorted
// alphabetically; nested values render as compact JSON strings.
func JSONL(body []byte) template.HTML {
	lines := bytes.Split(body, []byte("\n"))
	rows := []map[string]any{}
	seen := map[string]bool{}
	cols := []string{}
	for _, l := range lines {
		l = bytes.TrimSpace(l)
		if len(l) == 0 {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal(l, &obj); err != nil {
			return Text(body)
		}
		rows = append(rows, obj)
		for k := range obj {
			if !seen[k] {
				seen[k] = true
				cols = append(cols, k)
			}
		}
	}
	if len(rows) == 0 {
		return Text(body)
	}
	sort.Strings(cols)
	stringRows := make([][]string, len(rows))
	for i, r := range rows {
		row := make([]string, len(cols))
		for j, k := range cols {
			if v, ok := r[k]; ok {
				row[j] = jsonCellString(v)
			}
		}
		stringRows[i] = row
	}
	return renderTable(cols, stringRows, len(rows))
}

func jsonCellString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}

func renderTable(header []string, rows [][]string, total int) template.HTML {
	displayed := rows
	truncated := false
	if len(displayed) > MaxRenderRows {
		displayed = displayed[:MaxRenderRows]
		truncated = true
	}
	var b strings.Builder
	b.WriteString(`<table class="output-table">`)
	b.WriteString(`<thead><tr>`)
	for i, h := range header {
		fmt.Fprintf(&b, `<th data-col="%d">%s</th>`, i, html.EscapeString(h))
	}
	b.WriteString(`</tr></thead><tbody>`)
	for _, row := range displayed {
		b.WriteString(`<tr>`)
		for _, cell := range row {
			fmt.Fprintf(&b, `<td>%s</td>`, html.EscapeString(cell))
		}
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</tbody></table>`)
	if truncated {
		fmt.Fprintf(&b, `<div class="notice">Showing %d of %d rows</div>`, MaxRenderRows, total)
	}
	return template.HTML(b.String())
}
