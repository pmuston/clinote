package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/pmuston/clinote/internal/notebook"
	"github.com/pmuston/clinote/internal/runner"
)

func makeServer(t *testing.T, src string) (*Server, *echo.Echo, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	nb, err := notebook.Parse(f)
	f.Close()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	r, err := runner.New("bash")
	if err != nil {
		t.Fatalf("runner: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	srv, err := New(path, nb, r)
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	e := echo.New()
	e.HideBanner = true
	srv.Register(e)
	return srv, e, path
}

// §8.3 #1: GET / returns HTML containing expected cell structure.
func TestIndexRendersCells(t *testing.T) {
	src := "Prologue.\n\n```sh\necho hello\n```\n"
	_, e, _ := makeServer(t, src)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="cell-1"`) {
		t.Errorf("expected cell div with idx 1, got:\n%s", body)
	}
	if !strings.Contains(body, "echo hello") {
		t.Errorf("expected command body in output: %s", body)
	}
	if !strings.Contains(body, `hx-post="/run/1"`) {
		t.Errorf("expected run button targeting /run/1: %s", body)
	}
}

// §8.3 #2: POST /run/:idx + poll → spinner → output.
func TestRunPollCycleAndDiskUpdate(t *testing.T) {
	src := "```sh\necho hello-test\n```\n"
	srv, e, path := makeServer(t, src)

	// Find the command idx (should be 0 here since no leading prose).
	cmdIdx := -1
	for i, b := range srv.nb.Blocks {
		if _, ok := b.(notebook.CommandBlock); ok {
			cmdIdx = i
			break
		}
	}
	if cmdIdx < 0 {
		t.Fatal("no command block")
	}

	// Kick off the run.
	req := httptest.NewRequest(http.MethodPost, "/run/0", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("run: status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "spinner") {
		t.Errorf("expected spinner in initial response: %s", rec.Body.String())
	}

	// Poll until output appears or timeout.
	deadline := time.Now().Add(3 * time.Second)
	var final string
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, "/cell/0", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		body := rec.Body.String()
		if !strings.Contains(body, "spinner") {
			final = body
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if final == "" {
		t.Fatal("output never replaced spinner")
	}
	if !strings.Contains(final, "hello-test") {
		t.Errorf("expected output payload in cell: %s", final)
	}
	if !strings.Contains(final, "exit=0") {
		t.Errorf("expected exit=0 footer: %s", final)
	}

	// On-disk file is updated.
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(onDisk, []byte("hello-test")) {
		t.Errorf("file not updated: %s", onDisk)
	}
	if !bytes.Contains(onDisk, []byte("```output")) {
		t.Errorf("output block missing from file: %s", onDisk)
	}
}

// §8.3 #3: concurrent POST /run returns 409.
func TestConcurrentRunReturns409(t *testing.T) {
	src := "```sh\nsleep 1\n```\n"
	_, e, _ := makeServer(t, src)

	// First run kicks off (sleeps 1s).
	req1 := httptest.NewRequest(http.MethodPost, "/run/0", nil)
	rec1 := httptest.NewRecorder()
	e.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first run status = %d", rec1.Code)
	}

	// Immediately try a second run; should 409.
	req2 := httptest.NewRequest(http.MethodPost, "/run/0", nil)
	rec2 := httptest.NewRecorder()
	e.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusConflict {
		t.Errorf("expected 409 on concurrent run, got %d body=%s", rec2.Code, rec2.Body.String())
	}

	// Wait for the first run to complete so the runner can be Closed cleanly.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, "/cell/0", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if !strings.Contains(rec.Body.String(), "spinner") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("first run never finished")
}

func TestProseEditSavesFile(t *testing.T) {
	src := "Original prose.\n\n```sh\necho ok\n```\n"
	srv, e, path := makeServer(t, src)

	// Find prose idx.
	proseIdx := -1
	for i, b := range srv.nb.Blocks {
		if _, ok := b.(notebook.ProseBlock); ok {
			proseIdx = i
			break
		}
	}
	if proseIdx < 0 {
		t.Fatal("no prose block")
	}

	form := url.Values{}
	form.Set("text", "Replaced prose.\n\n")
	req := httptest.NewRequest(http.MethodPost, "/prose/0", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(onDisk, []byte("Replaced prose.")) {
		t.Errorf("prose not persisted: %s", onDisk)
	}
}

func TestStaticAssetsServed(t *testing.T) {
	_, e, _ := makeServer(t, "")
	for _, p := range []string{"/static/htmx.min.js", "/static/style.css", "/static/sortable.js"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s status = %d", p, rec.Code)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("%s empty body", p)
		}
	}
}
