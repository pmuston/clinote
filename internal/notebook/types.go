package notebook

import (
	"strconv"
	"time"
)

type Notebook struct {
	Source      []byte
	FrontMatter FrontMatter
	Blocks      []Block

	edits []edit
}

type FrontMatter struct {
	Title   string
	Created time.Time
	Shell   string

	Raw     []byte
	Present bool
}

type Block interface {
	Span() (start, end int)
	isBlock()
}

type ProseBlock struct {
	Start, End int
}

func (p ProseBlock) Span() (int, int) { return p.Start, p.End }
func (p ProseBlock) isBlock()         {}

type CommandBlock struct {
	Start, End int
	BodyStart  int
	BodyEnd    int
	InfoString string
	Attrs      map[string]string
}

func (c CommandBlock) Span() (int, int) { return c.Start, c.End }
func (c CommandBlock) isBlock()         {}

func (c CommandBlock) Body(src []byte) string {
	return string(src[c.BodyStart:c.BodyEnd])
}

type OutputBlock struct {
	Start, End int
	BodyStart  int
	BodyEnd    int
	InfoString string
	Attrs      map[string]string
}

func (o OutputBlock) Span() (int, int) { return o.Start, o.End }
func (o OutputBlock) isBlock()         {}

func (o OutputBlock) Body(src []byte) string {
	return string(src[o.BodyStart:o.BodyEnd])
}

func (o OutputBlock) Type() string {
	if t, ok := o.Attrs["type"]; ok && t != "" {
		return t
	}
	return "text"
}

func (o OutputBlock) ExitCode() (int, bool) {
	v, ok := o.Attrs["exit"]
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	return n, true
}

func (o OutputBlock) Ran() (time.Time, bool) {
	v, ok := o.Attrs["ran"]
	if !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func (o OutputBlock) Duration() (time.Duration, bool) {
	v, ok := o.Attrs["dur"]
	if !ok {
		return 0, false
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, false
	}
	return d, true
}

func (o OutputBlock) Truncated() bool {
	_, ok := o.Attrs["truncated"]
	return ok
}

type edit struct {
	start, end int
	replace    []byte
}
