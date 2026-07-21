package server

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
)

// handleNotebookFile serves files sitting next to the notebook, so a plain
// markdown image link resolves in the browser exactly as it does on GitHub:
//
//	![chart](chart.svg)
//
// The browser resolves that against "/", which is why this is registered as a
// root-level catch-all rather than under a prefix — a prefixed route would
// force "/assets/chart.svg" into the markdown and break GitHub rendering.
// Echo matches static and parameterised routes ahead of the catch-all, so the
// notebook's own endpoints keep priority.
//
// Scope is deliberately the notebook's own directory. Three things are refused:
// paths that escape it (directly or through a symlink), dotfile components
// (keeps .git/ and .env out of reach), and directories (no listings).
func (s *Server) handleNotebookFile(c echo.Context) error {
	raw := c.Param("*")
	rel, err := url.PathUnescape(raw)
	if err != nil {
		return c.String(http.StatusBadRequest, "bad path")
	}
	if rel == "" {
		return c.String(http.StatusNotFound, "not found")
	}

	// Refuse any dotfile component. This blocks ".." as a side effect, but the
	// real targets are ".git/config", ".env" and friends sharing the directory.
	for _, part := range strings.Split(rel, "/") {
		if strings.HasPrefix(part, ".") {
			return c.String(http.StatusNotFound, "not found")
		}
	}

	s.mu.Lock()
	base := filepath.Dir(s.path)
	s.mu.Unlock()

	// Clean against a rooted path first: this collapses any "../" before the
	// join, so the result cannot climb above base lexically.
	full := filepath.Join(base, filepath.Clean("/"+rel))

	// Then resolve symlinks and re-check containment — a symlink inside the
	// notebook directory could otherwise point anywhere on disk.
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		return c.String(http.StatusNotFound, "not found")
	}
	baseResolved, err := filepath.EvalSymlinks(base)
	if err != nil {
		return c.String(http.StatusInternalServerError, "cannot resolve notebook directory")
	}
	if resolved != baseResolved && !strings.HasPrefix(resolved, baseResolved+string(os.PathSeparator)) {
		return c.String(http.StatusNotFound, "not found")
	}

	info, err := os.Stat(resolved)
	if err != nil || info.IsDir() {
		return c.String(http.StatusNotFound, "not found")
	}

	// An SVG fetched by <img> can't run its scripts, but navigating straight to
	// the URL loads it as a document, where it can. The sandbox directive makes
	// it inert either way, so viewing a notebook someone sent you stays passive.
	c.Response().Header().Set("Content-Security-Policy", "sandbox")
	c.Response().Header().Set("X-Content-Type-Options", "nosniff")
	return c.File(resolved)
}
