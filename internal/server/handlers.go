package server

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/pmuston/clinote/internal/notebook"
	"github.com/pmuston/clinote/internal/runner"
)

func (s *Server) handleIndex(c echo.Context) error {
	s.mu.Lock()
	data, err := s.buildPageData()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	return s.tmpl.ExecuteTemplate(c.Response(), "index", data)
}

// handleRun kicks off a goroutine to execute the command at the given block
// index. Returns a 409 if a run is already in flight.
func (s *Server) handleRun(c echo.Context) error {
	idx, err := strconv.Atoi(c.Param("idx"))
	if err != nil {
		return c.String(http.StatusBadRequest, "bad idx")
	}

	s.mu.Lock()
	if s.activeIdx >= 0 {
		s.mu.Unlock()
		return c.String(http.StatusConflict, "another run in flight")
	}
	if idx < 0 || idx >= len(s.nb.Blocks) {
		s.mu.Unlock()
		return c.String(http.StatusBadRequest, "idx out of range")
	}
	cmd, ok := s.nb.Blocks[idx].(notebook.CommandBlock)
	if !ok {
		s.mu.Unlock()
		return c.String(http.StatusBadRequest, "block is not a command")
	}
	s.activeIdx = idx
	body := cmd.Body(s.nb.Source)
	// Capture the command's `out=` hint at start time. The output block's
	// `type=` reflects this so the renderer picks CSV / JSONL when declared.
	// Snapshot it now so a concurrent prose / add-cell mutation can't shift
	// the lookup before the run finishes.
	outType := cmd.Attrs["out"]
	if outType == "" {
		outType = "text"
	}
	s.mu.Unlock()

	go s.executeRun(idx, body, outType)

	// Immediate response: re-render the cell with the running spinner.
	return s.respondCell(c, idx)
}

func (s *Server) executeRun(idx int, command, outType string) {
	res, err := s.runner.Run(context.Background(), command)
	s.mu.Lock()
	defer s.mu.Unlock()

	if err != nil {
		// On runner error, mark as failed by writing a placeholder output.
		// Errors are always text — the declared out= type doesn't apply to
		// an error message.
		attrs := map[string]string{
			"type": "text",
			"exit": "-1",
			"ran":  nowFunc().UTC().Format(time.RFC3339),
			"dur":  "0s",
		}
		body := "runner error: " + err.Error() + "\n"
		_ = s.nb.SetOutput(idx, body, attrs)
		_ = s.nb.WriteFile(s.path)
		s.activeIdx = -1
		s.runResults[idx] = &runner.Result{Output: []byte(body), ExitCode: -1, Started: nowFunc(), Duration: 0}
		s.liveANSI[idx] = []byte(body)
		return
	}

	// Pick the stream based on exit code:
	//   exit=0 → stdout (stderr was probably progress / warnings)
	//   exit≠0 → stderr (the error message), falling back to stdout if empty
	body := pickOutputStream(res)

	attrs := map[string]string{
		"type": outType,
		"exit": strconv.Itoa(res.ExitCode),
		"ran":  res.Started.UTC().Format(time.RFC3339),
		"dur":  formatDuration(res.Duration),
	}
	if res.Truncated {
		attrs["truncated"] = "true"
	}
	// The on-disk body is already ANSI-stripped (the runner stripped it).
	if err := s.nb.SetOutput(idx, string(body), attrs); err != nil {
		s.activeIdx = -1
		return
	}
	_ = s.nb.WriteFile(s.path)

	// liveANSI normally would hold WITH-ANSI bytes; the runner already strips
	// ANSI so we store the picked body for the one-shot live render.
	s.liveANSI[idx] = body
	s.runResults[idx] = &res
	s.activeIdx = -1
}

// pickOutputStream selects which captured stream becomes the cell's saved
// output. Success → stdout; failure → stderr (with stdout fallback when
// stderr is empty, e.g. `false` returns exit=1 with no output on either).
func pickOutputStream(res runner.Result) []byte {
	if res.ExitCode == 0 {
		return res.Stdout
	}
	if len(res.Stderr) > 0 {
		return res.Stderr
	}
	return res.Stdout
}

// formatDuration produces a stable, human-friendly string: ms for sub-second,
// seconds (1dp) otherwise. Avoids the "0s" round-down for fast commands.
func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return strconv.FormatInt(int64(d/time.Microsecond), 10) + "µs"
	}
	if d < time.Second {
		return strconv.FormatInt(int64(d/time.Millisecond), 10) + "ms"
	}
	secs := float64(d) / float64(time.Second)
	return strconv.FormatFloat(secs, 'f', 1, 64) + "s"
}

func (s *Server) handleCell(c echo.Context) error {
	idx, err := strconv.Atoi(c.Param("idx"))
	if err != nil {
		return c.String(http.StatusBadRequest, "bad idx")
	}
	return s.respondCell(c, idx)
}

// respondCell renders the single cell fragment (command + paired output OR
// command + spinner) for HTMX swap-by-id.
func (s *Server) respondCell(c echo.Context, idx int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if idx < 0 || idx >= len(s.nb.Blocks) {
		return c.String(http.StatusNotFound, "no such cell")
	}
	cb, ok := s.nb.Blocks[idx].(notebook.CommandBlock)
	if !ok {
		return c.String(http.StatusBadRequest, "not a command cell")
	}
	u, err := s.cellUnit(idx, cb, idx)
	if err != nil {
		return err
	}
	return s.renderFragment(c, "cell", u)
}

func (s *Server) handleInterrupt(c echo.Context) error {
	if err := s.runner.Interrupt(); err != nil {
		return c.String(http.StatusInternalServerError, err.Error())
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *Server) handleProseView(c echo.Context) error {
	idx, err := strconv.Atoi(c.Param("idx"))
	if err != nil {
		return c.String(http.StatusBadRequest, "bad idx")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if idx < 0 || idx >= len(s.nb.Blocks) {
		return c.String(http.StatusNotFound, "no such block")
	}
	p, ok := s.nb.Blocks[idx].(notebook.ProseBlock)
	if !ok {
		return c.String(http.StatusBadRequest, "not a prose block")
	}
	u, err := s.proseUnit(idx, p)
	if err != nil {
		return err
	}
	return s.renderFragment(c, "prose", u)
}

func (s *Server) handleProseEdit(c echo.Context) error {
	idx, err := strconv.Atoi(c.Param("idx"))
	if err != nil {
		return c.String(http.StatusBadRequest, "bad idx")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if idx < 0 || idx >= len(s.nb.Blocks) {
		return c.String(http.StatusNotFound, "no such block")
	}
	p, ok := s.nb.Blocks[idx].(notebook.ProseBlock)
	if !ok {
		return c.String(http.StatusBadRequest, "not a prose block")
	}
	u, err := s.proseUnit(idx, p)
	if err != nil {
		return err
	}
	return s.renderFragment(c, "prose-edit", u)
}

func (s *Server) handleProseSave(c echo.Context) error {
	idx, err := strconv.Atoi(c.Param("idx"))
	if err != nil {
		return c.String(http.StatusBadRequest, "bad idx")
	}
	text := c.FormValue("text")
	// Ensure trailing newline so following blocks aren't fused into this prose line.
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	s.mu.Lock()
	if err := s.nb.SetProse(idx, text); err != nil {
		s.mu.Unlock()
		return c.String(http.StatusBadRequest, err.Error())
	}
	if err := s.nb.WriteFile(s.path); err != nil {
		s.mu.Unlock()
		return c.String(http.StatusInternalServerError, err.Error())
	}
	// Re-fetch the (possibly re-indexed) block. After SetProse, block count is
	// unchanged, so idx is still valid and still refers to the prose.
	p, ok := s.nb.Blocks[idx].(notebook.ProseBlock)
	if !ok {
		s.mu.Unlock()
		return c.String(http.StatusInternalServerError, "prose missing after save")
	}
	u, err := s.proseUnit(idx, p)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return s.renderFragment(c, "prose", u)
}

// proseNewPlaceholder is the minimal content used when scaffolding a new prose
// block via "+ prose". It renders as nothing in HTML (HTML comment), so a
// user who cancels their first edit is left with an invisible block they can
// remove via the delete control.
const proseNewPlaceholder = "<!-- new -->"

// handleCellFormat updates the output type for the cell at idx. Mutates BOTH
// the command's `out=` attribute (so future runs save with the new type) and
// the paired output block's `type=` attribute, if one exists (so the current
// render reflects the change). Requires FrontMatter.Editable.
func (s *Server) handleCellFormat(c echo.Context) error {
	idx, err := strconv.Atoi(c.Param("idx"))
	if err != nil {
		return c.String(http.StatusBadRequest, "bad idx")
	}
	newType := c.FormValue("type")
	switch newType {
	case "text", "csv", "tsv", "jsonl":
	default:
		return c.String(http.StatusBadRequest, "invalid type: "+newType)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.nb.FrontMatter.Editable {
		return c.String(http.StatusForbidden, "format changes are disabled; set `editable: true` in front matter to enable")
	}
	if s.activeIdx == idx {
		return c.String(http.StatusConflict, "cannot change format while the cell is running")
	}
	if idx < 0 || idx >= len(s.nb.Blocks) {
		return c.String(http.StatusNotFound, "no such cell")
	}
	if _, ok := s.nb.Blocks[idx].(notebook.CommandBlock); !ok {
		return c.String(http.StatusBadRequest, "not a command cell")
	}

	// 1) Update the command's out= attr.
	if err := s.nb.SetCommandOutType(idx, newType); err != nil {
		return c.String(http.StatusInternalServerError, err.Error())
	}
	// 2) If a paired output exists, rewrite its type= attr.
	if ob, ok := pairedOutput(s.nb, idx); ok {
		attrs := make(map[string]string, len(ob.Attrs))
		for k, v := range ob.Attrs {
			attrs[k] = v
		}
		attrs["type"] = newType
		body := ob.Body(s.nb.Source)
		if err := s.nb.SetOutput(idx, body, attrs); err != nil {
			return c.String(http.StatusInternalServerError, err.Error())
		}
	}
	if err := s.nb.WriteFile(s.path); err != nil {
		return c.String(http.StatusInternalServerError, err.Error())
	}

	cb, _ := s.nb.Blocks[idx].(notebook.CommandBlock)
	u, err := s.cellUnit(idx, cb, -1)
	if err != nil {
		return err
	}
	return s.renderFragment(c, "cell", u)
}

// handleAddCell appends a new block to the notebook. The `kind` form value
// selects "sh" (default) or "prose". The new block is rendered in edit mode
// so the user can type immediately — for sh cells this requires editable:true
// (otherwise the new cell renders in view mode like before). Returns the
// freshly-rendered notebook body so HTMX can swap it into #notebook.
func (s *Server) handleAddCell(c echo.Context) error {
	kind := c.FormValue("kind")
	if kind == "" {
		kind = c.QueryParam("kind")
	}
	s.mu.Lock()
	var err error
	switch kind {
	case "prose":
		// Prose has no fence-delimited structure, so an empty body isn't a
		// parseable block. Use a minimal HTML comment as the placeholder —
		// invisible in rendered markdown, and we blank it from the textarea
		// below so the user sees an empty editor.
		err = s.nb.AppendProse(proseNewPlaceholder)
	case "", "sh":
		err = s.nb.AppendCell("")
	default:
		s.mu.Unlock()
		return c.String(http.StatusBadRequest, "unknown kind: "+kind)
	}
	if err != nil {
		s.mu.Unlock()
		return c.String(http.StatusInternalServerError, err.Error())
	}
	if err := s.nb.WriteFile(s.path); err != nil {
		s.mu.Unlock()
		return c.String(http.StatusInternalServerError, err.Error())
	}
	newIdx := lastBlockIdxOfKind(s.nb, kind)
	units, err := s.buildUnits(-1)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	// Mark the freshly-added block to render in edit mode. For sh cells this
	// only takes effect when editable=true (the edit form requires it server-
	// side); otherwise we leave it in view mode.
	if newIdx >= 0 {
		for i := range units {
			if units[i].Idx == newIdx {
				if units[i].Kind == "prose" || (units[i].Kind == "cell" && s.nb.FrontMatter.Editable) {
					units[i].StartInEditMode = true
					if units[i].Kind == "prose" {
						// Blank out the placeholder so the textarea opens empty.
						units[i].Raw = ""
					}
				}
				break
			}
		}
	}
	s.mu.Unlock()
	return s.renderFragment(c, "notebook-body", units)
}

// lastBlockIdxOfKind returns the highest block index matching kind ("sh" or
// "prose"). Used right after an append to find the newly-added block.
func lastBlockIdxOfKind(nb *notebook.Notebook, kind string) int {
	for i := len(nb.Blocks) - 1; i >= 0; i-- {
		switch nb.Blocks[i].(type) {
		case notebook.CommandBlock:
			if kind == "" || kind == "sh" {
				return i
			}
		case notebook.ProseBlock:
			if kind == "prose" {
				return i
			}
		}
	}
	return -1
}

// handleBlockDelete removes a block. For sh cells / orphan output blocks it
// requires editable:true; prose can always be deleted (matches prose editing
// being always-on). For a command block, the paired output is removed too so
// the file doesn't end up with an orphan.
func (s *Server) handleBlockDelete(c echo.Context) error {
	idx, err := strconv.Atoi(c.Param("idx"))
	if err != nil {
		return c.String(http.StatusBadRequest, "bad idx")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if idx < 0 || idx >= len(s.nb.Blocks) {
		return c.String(http.StatusNotFound, "no such block")
	}
	switch s.nb.Blocks[idx].(type) {
	case notebook.CommandBlock, notebook.OutputBlock:
		if !s.nb.FrontMatter.Editable {
			return c.String(http.StatusForbidden, "delete is disabled; set `editable: true` in front matter to enable")
		}
		if s.activeIdx == idx {
			return c.String(http.StatusConflict, "cannot delete a running cell")
		}
	case notebook.ProseBlock:
		// Always allowed.
	default:
		return c.String(http.StatusBadRequest, "unknown block type")
	}
	if err := s.nb.DeleteBlock(idx); err != nil {
		return c.String(http.StatusInternalServerError, err.Error())
	}
	if err := s.nb.WriteFile(s.path); err != nil {
		return c.String(http.StatusInternalServerError, err.Error())
	}
	units, err := s.buildUnits(-1)
	if err != nil {
		return err
	}
	return s.renderFragment(c, "notebook-body", units)
}

// handleCellEdit returns the editor form for a command cell. Requires
// FrontMatter.Editable == true.
func (s *Server) handleCellEdit(c echo.Context) error {
	idx, err := strconv.Atoi(c.Param("idx"))
	if err != nil {
		return c.String(http.StatusBadRequest, "bad idx")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.nb.FrontMatter.Editable {
		return c.String(http.StatusForbidden, "command editing is disabled; set `editable: true` in front matter to enable")
	}
	if idx < 0 || idx >= len(s.nb.Blocks) {
		return c.String(http.StatusNotFound, "no such cell")
	}
	cb, ok := s.nb.Blocks[idx].(notebook.CommandBlock)
	if !ok {
		return c.String(http.StatusBadRequest, "not a command cell")
	}
	u := unit{
		Kind:     "cell",
		Idx:      idx,
		Editable: true,
		Command:  cb.Body(s.nb.Source),
	}
	return s.renderFragment(c, "cell-edit", u)
}

// handleCellSave persists an edited command body. Requires
// FrontMatter.Editable == true.
func (s *Server) handleCellSave(c echo.Context) error {
	idx, err := strconv.Atoi(c.Param("idx"))
	if err != nil {
		return c.String(http.StatusBadRequest, "bad idx")
	}
	text := c.FormValue("text")
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.nb.FrontMatter.Editable {
		return c.String(http.StatusForbidden, "command editing is disabled")
	}
	if err := s.nb.SetCommand(idx, text); err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}
	if err := s.nb.WriteFile(s.path); err != nil {
		return c.String(http.StatusInternalServerError, err.Error())
	}
	cb, ok := s.nb.Blocks[idx].(notebook.CommandBlock)
	if !ok {
		return c.String(http.StatusInternalServerError, "cell missing after save")
	}
	u, err := s.cellUnit(idx, cb, -1)
	if err != nil {
		return err
	}
	return s.renderFragment(c, "cell", u)
}

// handlePicker lists .md files in the cwd. v1 reuses this only when invoked
// without a path; once a path is bound to the server, the picker isn't hit.
func (s *Server) handlePicker(c echo.Context) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(cwd)
	if err != nil {
		return err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".md") {
			files = append(files, e.Name())
		}
	}
	data := struct {
		Files []string
		Cwd   string
	}{files, cwd}
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	return s.tmpl.ExecuteTemplate(c.Response(), "picker", data)
}
