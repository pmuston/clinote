package notebook

import "strings"

// ParseInfoString parses a fenced-code-block info string into a language tag
// and an attribute map. Tokens are whitespace-separated. A token of the form
// `k=v` becomes attrs[k]=v; a bare token `k` becomes attrs[k]="".
func ParseInfoString(info string) (lang string, attrs map[string]string) {
	attrs = map[string]string{}
	fields := strings.Fields(info)
	if len(fields) == 0 {
		return "", attrs
	}
	lang = fields[0]
	for _, tok := range fields[1:] {
		if i := strings.IndexByte(tok, '='); i >= 0 {
			attrs[tok[:i]] = tok[i+1:]
		} else {
			attrs[tok] = ""
		}
	}
	return lang, attrs
}
