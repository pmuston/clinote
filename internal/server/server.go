// Package server hosts the HTMX-driven UI: it renders the notebook, accepts
// run/edit requests, and saves the notebook back to disk after each mutation.
package server

import (
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/pmuston/clinote/internal/notebook"
	"github.com/pmuston/clinote/internal/runner"
)

type Server struct {
	mu     sync.Mutex
	path   string
	nb     *notebook.Notebook
	runner *runner.Runner

	// activeIdx tracks the cell currently being run; -1 means no run in flight.
	// Guarded by mu.
	activeIdx int
	// liveANSI holds with-ANSI output bytes from the most recent run, keyed by
	// command-cell index. The first /cell/:idx after run completion consumes
	// the entry so colours appear in the live render before the page reloads
	// against the ANSI-stripped on-disk bytes.
	liveANSI map[int][]byte
	// runResults caches the most recent run result per cell so /cell/:idx can
	// render the freshly-completed output via the live-render path.
	runResults map[int]*runner.Result

	tmpl *template.Template
}

func New(path string, nb *notebook.Notebook, r *runner.Runner) (*Server, error) {
	t, err := template.ParseFS(templatesFS(), "*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	return &Server{
		path:       path,
		nb:         nb,
		runner:     r,
		activeIdx:  -1,
		liveANSI:   map[int][]byte{},
		runResults: map[int]*runner.Result{},
		tmpl:       t,
	}, nil
}

// Register wires the server's routes onto an echo instance.
func (s *Server) Register(e *echo.Echo) {
	e.GET("/", s.handleIndex)
	e.POST("/run/:idx", s.handleRun)
	e.GET("/cell/:idx", s.handleCell)
	e.POST("/interrupt", s.handleInterrupt)
	e.GET("/prose/:idx", s.handleProseView)
	e.GET("/prose/:idx/edit", s.handleProseEdit)
	e.POST("/prose/:idx", s.handleProseSave)
	e.POST("/add-cell", s.handleAddCell)
	e.POST("/block/:idx/delete", s.handleBlockDelete)
	e.GET("/cell/:idx/edit", s.handleCellEdit)
	e.POST("/cell/:idx/edit", s.handleCellSave)
	e.POST("/cell/:idx/format", s.handleCellFormat)
	e.GET("/picker", s.handlePicker)
	e.GET("/static/*", echo.WrapHandler(http.StripPrefix("/static/", http.FileServer(http.FS(staticFS())))))
}

// renderTemplate executes the named template into w with data.
func (s *Server) renderTemplate(w io.Writer, name string, data any) error {
	return s.tmpl.ExecuteTemplate(w, name, data)
}

// renderFragment is a small helper for handlers that return a single HTML
// fragment for HTMX swaps.
func (s *Server) renderFragment(c echo.Context, name string, data any) error {
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	var sb strings.Builder
	if err := s.renderTemplate(&sb, name, data); err != nil {
		return err
	}
	return c.HTML(http.StatusOK, sb.String())
}

func (s *Server) title() string {
	if s.nb.FrontMatter.Title != "" {
		return s.nb.FrontMatter.Title
	}
	return s.path
}

// nowFunc is overridable for tests.
var nowFunc = time.Now
