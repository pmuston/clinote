package server

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"strings"

	"github.com/pmuston/clinote/internal/notebook"
	"github.com/pmuston/clinote/internal/render"
	"github.com/yuin/goldmark"
)

// unitKind discriminates the three things that can appear in the rendered
// notebook: prose paragraphs, command cells (possibly with paired output), and
// orphaned output blocks.
type unit struct {
	Kind string // "prose", "cell", "orphan"
	Idx  int    // block index in nb.Blocks for prose/cell; for "orphan", the index of the output block

	// Editable mirrors FrontMatter.Editable so the cell template can show / hide
	// the edit / delete / format-picker controls without needing access to the
	// outer page scope.
	Editable bool

	// StartInEditMode is true when a newly-added cell / prose should render
	// directly in its editor (instead of view mode). Used by handleAddCell so
	// the user can type immediately.
	StartInEditMode bool

	// prose
	ProseHTML template.HTML
	Raw       string // for edit textarea

	// cell
	Command   string
	HasOutput bool
	Running   bool
	// SelfPoll is true when this cell's spinner should poll /cell/:idx for its
	// own completion. During a batch it is false: the whole body is being
	// polled instead, and two pollers racing on the same cell would swap
	// conflicting fragments into the page.
	SelfPoll bool
	// BatchActive disables per-cell controls while a batch owns the runner.
	BatchActive bool
	// OutHint mirrors the command's `out=` attribute (or "text" if absent).
	// Drives the type picker so it reflects the command's declared intent
	// even when there's no paired output yet.
	OutHint string

	// output (used by cell or orphan)
	OutputHTML template.HTML
	OutputType string
	ExitCode   string
	Duration   string
	Truncated  bool
	Failed     bool
}

// bodyData is what the notebook-body fragment renders from. It is shared by
// the full page and by the poll endpoint, so a batch in progress looks the
// same either way.
type bodyData struct {
	Units []unit
	// BatchActive drives the self-terminating poller: while true the body
	// carries a polling element, and the first response after the batch ends
	// omits it, which stops the polling.
	BatchActive bool
	BatchStatus string
	// CommandCount seeds the run-all confirmation prompt.
	CommandCount int
}

type pageData struct {
	Title string
	Path  string
	Wide  bool
	Body  bodyData
	// MissingEnv lists front-matter `requires:` variables absent from the
	// environment. Reported as a banner, never enforced — you should be able
	// to open a notebook to read it without its credentials to hand.
	MissingEnv []string
}

// missingEnv returns the required variables that are unset or empty.
//
// The check reads the server's own environment, which is what the runner
// passes to the shell (cmd.Env derives from os.Environ). The shell also sources
// the user's rc file, so a variable exported only in ~/.bashrc is visible to
// cells while reported missing here — the common case, exporting before launch,
// is accurate.
func missingEnv(required []string) []string {
	var missing []string
	for _, name := range required {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if os.Getenv(name) == "" {
			missing = append(missing, name)
		}
	}
	return missing
}

func (s *Server) buildPageData() (pageData, error) {
	body, err := s.buildBodyData()
	if err != nil {
		return pageData{}, err
	}
	return pageData{
		Title:      s.title(),
		Path:       s.path,
		Wide:       s.nb.FrontMatter.Width == "full",
		Body:       body,
		MissingEnv: missingEnv(s.nb.FrontMatter.Requires),
	}, nil
}

// buildBodyData assembles the notebook body. Caller must hold s.mu.
func (s *Server) buildBodyData() (bodyData, error) {
	units, err := s.buildUnits(-1)
	if err != nil {
		return bodyData{}, err
	}
	return bodyData{
		Units:        units,
		BatchActive:  s.batch.active,
		BatchStatus:  s.batchStatus(),
		CommandCount: len(commandIndices(s.nb)),
	}, nil
}

// buildUnits assembles the rendered units from the current notebook. If
// liveIdx >= 0, the unit for the command at liveIdx uses any live-ANSI bytes
// in s.liveANSI (and clears them) so colours appear after a run.
func (s *Server) buildUnits(liveIdx int) ([]unit, error) {
	var out []unit
	blocks := s.nb.Blocks
	for i := 0; i < len(blocks); i++ {
		b := blocks[i]
		switch v := b.(type) {
		case notebook.ProseBlock:
			u, err := s.proseUnit(i, v)
			if err != nil {
				return nil, err
			}
			out = append(out, u)
		case notebook.CommandBlock:
			u, err := s.cellUnit(i, v, liveIdx)
			if err != nil {
				return nil, err
			}
			out = append(out, u)
			// Skip the paired output block (if any) — it was folded into the cell.
			if i+1 < len(blocks) {
				if _, ok := blocks[i+1].(notebook.OutputBlock); ok {
					i++
				}
			}
		case notebook.OutputBlock:
			// Reached only when an output block has no preceding command (orphan).
			out = append(out, s.orphanUnit(i, v))
		}
	}
	return out, nil
}

func (s *Server) proseUnit(idx int, p notebook.ProseBlock) (unit, error) {
	src := s.nb.Source[p.Start:p.End]
	var buf bytes.Buffer
	if err := goldmark.New().Convert(src, &buf); err != nil {
		return unit{}, fmt.Errorf("goldmark: %w", err)
	}
	return unit{
		Kind:      "prose",
		Idx:       idx,
		ProseHTML: template.HTML(buf.String()),
		Raw:       string(src),
	}, nil
}

func (s *Server) cellUnit(idx int, c notebook.CommandBlock, liveIdx int) (unit, error) {
	hint := c.Attrs["out"]
	if hint == "" {
		hint = "text"
	}
	u := unit{
		Kind:        "cell",
		Idx:         idx,
		Editable:    s.nb.FrontMatter.Editable,
		Command:     c.Body(s.nb.Source),
		Running:     s.activeIdx == idx,
		SelfPoll:    !s.batch.active,
		BatchActive: s.batch.active,
		OutHint:     hint,
	}
	// Find paired output (immediately following, only whitespace between).
	if pair, ok := pairedOutput(s.nb, idx); ok {
		u.HasOutput = true
		fillOutputFields(&u, pair, s.nb.Source)

		// If this is the live-render moment and we have ANSI bytes for this idx,
		// use them. Otherwise fall back to on-disk (ANSI-stripped) body.
		body := []byte(pair.Body(s.nb.Source))
		if liveIdx == idx {
			if ansi, ok := s.liveANSI[idx]; ok {
				body = ansi
				delete(s.liveANSI, idx)
			}
		}
		u.OutputHTML = render.Output(body, u.OutputType)
	}
	return u, nil
}

func (s *Server) orphanUnit(idx int, o notebook.OutputBlock) unit {
	u := unit{
		Kind:     "orphan",
		Idx:      idx,
		Editable: s.nb.FrontMatter.Editable,
	}
	fillOutputFields(&u, o, s.nb.Source)
	u.OutputHTML = render.Output([]byte(o.Body(s.nb.Source)), u.OutputType)
	return u
}

func fillOutputFields(u *unit, o notebook.OutputBlock, src []byte) {
	u.OutputType = o.Type()
	if ec, ok := o.ExitCode(); ok {
		u.ExitCode = fmt.Sprintf("%d", ec)
		u.Failed = ec != 0
	} else {
		u.ExitCode = "?"
	}
	if d, ok := o.Duration(); ok {
		u.Duration = d.String()
	}
	u.Truncated = o.Truncated()
}

func pairedOutput(nb *notebook.Notebook, cmdIdx int) (notebook.OutputBlock, bool) {
	if cmdIdx+1 >= len(nb.Blocks) {
		return notebook.OutputBlock{}, false
	}
	cb, ok := nb.Blocks[cmdIdx].(notebook.CommandBlock)
	if !ok {
		return notebook.OutputBlock{}, false
	}
	ob, ok := nb.Blocks[cmdIdx+1].(notebook.OutputBlock)
	if !ok {
		return notebook.OutputBlock{}, false
	}
	between := nb.Source[cb.End:ob.Start]
	for _, ch := range between {
		if ch != ' ' && ch != '\t' && ch != '\n' && ch != '\r' {
			return notebook.OutputBlock{}, false
		}
	}
	return ob, true
}
