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
	s.mu.Unlock()

	go s.executeRun(idx, body)

	// Immediate response: re-render the cell with the running spinner.
	return s.respondCell(c, idx)
}

func (s *Server) executeRun(idx int, command string) {
	res, err := s.runner.Run(context.Background(), command)
	s.mu.Lock()
	defer s.mu.Unlock()

	if err != nil {
		// On runner error, mark as failed by writing a placeholder output.
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

	attrs := map[string]string{
		"type": "text",
		"exit": strconv.Itoa(res.ExitCode),
		"ran":  res.Started.UTC().Format(time.RFC3339),
		"dur":  formatDuration(res.Duration),
	}
	if res.Truncated {
		attrs["truncated"] = "true"
	}
	// The on-disk body is the runner.Result.Output which is already ANSI-stripped.
	if err := s.nb.SetOutput(idx, string(res.Output), attrs); err != nil {
		// Shouldn't happen, but recover gracefully.
		s.activeIdx = -1
		return
	}
	_ = s.nb.WriteFile(s.path)

	// liveANSI here would normally hold WITH-ANSI bytes, but our runner already
	// strips ANSI before returning. For v1 we store the stripped body too so the
	// cell-rendering path is uniform; once the runner exposes raw bytes we can
	// substitute them in here.
	s.liveANSI[idx] = res.Output
	s.runResults[idx] = &res
	s.activeIdx = -1
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
