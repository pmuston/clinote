package render

import (
	"strings"
	"testing"
)

func TestSniff(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", "text"},
		{"plain text", "hello\nworld\n", "text"},
		{"jsonl", `{"a":1}` + "\n" + `{"a":2}` + "\n", "jsonl"},
		{"jsonl one row", `{"a":1}` + "\n", "jsonl"},
		{"csv", "a,b,c\n1,2,3\n4,5,6\n", "csv"},
		{"tsv", "a\tb\tc\n1\t2\t3\n4\t5\t6\n", "tsv"},
		{"csv single column not enough", "1\n2\n3\n", "text"},
		{"csv inconsistent cols", "a,b\n1,2,3\n", "text"},
		{"tsv inconsistent cols", "a\tb\n1\t2\t3\n", "text"},
		{"mixed garbage", "hello\nworld\n{not json", "text"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Sniff([]byte(tt.in))
			if got != tt.want {
				t.Errorf("Sniff = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTextEscapesHTML(t *testing.T) {
	out := string(Text([]byte("<script>alert(1)</script>\n")))
	if strings.Contains(out, "<script>") {
		t.Errorf("HTML not escaped: %q", out)
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Errorf("expected escaped tags: %q", out)
	}
}

func TestTextANSIRedSpan(t *testing.T) {
	out := string(Text([]byte("\x1b[31mred\x1b[0m\n")))
	if !strings.Contains(out, `<span style="color:#a00">red</span>`) {
		t.Errorf("expected red span, got: %q", out)
	}
}

func TestTextANSIBoldUnderline(t *testing.T) {
	out := string(Text([]byte("\x1b[1;4mbold-under\x1b[0m")))
	if !strings.Contains(out, "font-weight:bold") {
		t.Errorf("missing bold style: %q", out)
	}
	if !strings.Contains(out, "text-decoration:underline") {
		t.Errorf("missing underline style: %q", out)
	}
}

func TestTextNoANSI(t *testing.T) {
	out := string(Text([]byte("just text\n")))
	if strings.Contains(out, "<span") {
		t.Errorf("unexpected span in plain output: %q", out)
	}
	if !strings.Contains(out, "just text") {
		t.Errorf("payload missing: %q", out)
	}
}

func TestTextDropsCursorMovement(t *testing.T) {
	// \x1b[2J = clear screen, \x1b[H = cursor home. Should be dropped.
	out := string(Text([]byte("\x1b[2J\x1b[Hhello")))
	if strings.ContainsRune(out, 0x1b) {
		t.Errorf("escape leaked: %q", out)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("payload missing: %q", out)
	}
}

func TestCSVRendersTable(t *testing.T) {
	body := "user_id,email\n1042,alice@example.com\n2099,bob@example.com\n"
	out := string(CSV([]byte(body)))
	if !strings.Contains(out, "<table") {
		t.Errorf("expected table, got: %q", out)
	}
	if !strings.Contains(out, "<th data-col=\"0\">user_id</th>") {
		t.Errorf("missing header cell with data-col: %q", out)
	}
	if !strings.Contains(out, "<td>alice@example.com</td>") {
		t.Errorf("missing data cell: %q", out)
	}
}

func TestCSVEscapesCells(t *testing.T) {
	body := "name\n<script>x</script>\n"
	out := string(CSV([]byte(body)))
	if strings.Contains(out, "<script>") {
		t.Errorf("CSV cell content not escaped: %q", out)
	}
}

func TestTSVRendersTable(t *testing.T) {
	body := "user_id\temail\trole\n1042\talice@example.com\teng\n2099\tbob@example.com\tmgr\n"
	out := string(TSV([]byte(body)))
	if !strings.Contains(out, "<table") {
		t.Errorf("expected table, got: %q", out)
	}
	if !strings.Contains(out, "<th data-col=\"0\">user_id</th>") {
		t.Errorf("missing first header: %q", out)
	}
	if !strings.Contains(out, "<td>alice@example.com</td>") {
		t.Errorf("missing data cell: %q", out)
	}
	// Commas in cells should NOT be treated as separators.
	bodyWithCommas := "label\tvalues\nfoo\ta, b, c\n"
	outC := string(TSV([]byte(bodyWithCommas)))
	if !strings.Contains(outC, "<td>a, b, c</td>") {
		t.Errorf("TSV must not split on commas inside fields: %q", outC)
	}
}

func TestOutputDispatchTSV(t *testing.T) {
	body := "a\tb\n1\t2\n"
	out := string(Output([]byte(body), "tsv"))
	if !strings.Contains(out, "<table") {
		t.Errorf("expected table for declared tsv: %q", out)
	}
}

func TestCSVTruncationNotice(t *testing.T) {
	var b strings.Builder
	b.WriteString("col\n")
	for i := 0; i < MaxRenderRows+5; i++ {
		b.WriteString("v\n")
	}
	out := string(CSV([]byte(b.String())))
	if !strings.Contains(out, "Showing 1000 of") {
		t.Errorf("expected truncation notice, got: %q", out[:200])
	}
}

func TestJSONLUnionColumns(t *testing.T) {
	body := `{"a":1,"b":2}` + "\n" + `{"b":3,"c":4}` + "\n"
	out := string(JSONL([]byte(body)))
	for _, col := range []string{"a", "b", "c"} {
		if !strings.Contains(out, ">"+col+"<") {
			t.Errorf("expected column %q in header: %q", col, out)
		}
	}
	// First row: a=1, b=2, c=empty
	if !strings.Contains(out, "<td>1</td><td>2</td><td></td>") {
		t.Errorf("first row mismatch: %q", out)
	}
}

func TestJSONLNestedAsJSON(t *testing.T) {
	body := `{"x":{"nested":true},"y":[1,2,3]}` + "\n"
	out := string(JSONL([]byte(body)))
	if !strings.Contains(out, `{&#34;nested&#34;:true}`) {
		t.Errorf("expected nested JSON as escaped cell: %q", out)
	}
	if !strings.Contains(out, `[1,2,3]`) {
		t.Errorf("expected array as compact JSON cell: %q", out)
	}
}

func TestOutputDispatch(t *testing.T) {
	csvBody := "a,b\n1,2\n"
	if !strings.Contains(string(Output([]byte(csvBody), "csv")), "<table") {
		t.Error("expected table for declared csv")
	}
	if !strings.Contains(string(Output([]byte("plain\n"), "text")), "plain") {
		t.Error("expected text passthrough")
	}
	if !strings.Contains(string(Output([]byte(csvBody), "")), "<table") {
		t.Error("expected table from sniffed csv")
	}
}
