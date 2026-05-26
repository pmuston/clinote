package notebook

import (
	"reflect"
	"testing"
)

func TestParseInfoString(t *testing.T) {
	tests := []struct {
		in        string
		wantLang  string
		wantAttrs map[string]string
	}{
		{"", "", map[string]string{}},
		{"   ", "", map[string]string{}},
		{"sh", "sh", map[string]string{}},
		{"sh out=text", "sh", map[string]string{"out": "text"}},
		{"sh out=csv ignored=foo", "sh", map[string]string{"out": "csv", "ignored": "foo"}},
		{"sh out", "sh", map[string]string{"out": ""}},
		{"output type=text exit=0 ran=2026-05-26T14:31:12Z dur=1.2s", "output", map[string]string{
			"type": "text", "exit": "0", "ran": "2026-05-26T14:31:12Z", "dur": "1.2s",
		}},
		{"output type=text truncated=true", "output", map[string]string{"type": "text", "truncated": "true"}},
		{"sh k=", "sh", map[string]string{"k": ""}},
		{"sh =v", "sh", map[string]string{"": "v"}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			lang, attrs := ParseInfoString(tt.in)
			if lang != tt.wantLang {
				t.Errorf("lang = %q, want %q", lang, tt.wantLang)
			}
			if !reflect.DeepEqual(attrs, tt.wantAttrs) {
				t.Errorf("attrs = %v, want %v", attrs, tt.wantAttrs)
			}
		})
	}
}
