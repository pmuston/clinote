package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="10" height="10"/></svg>`

// get issues a GET and returns the recorder.
func get(e interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestServesFileNextToNotebook(t *testing.T) {
	srv, e, path := makeServer(t, "![chart](chart.svg)\n")
	dir := filepath.Dir(path)
	if err := os.WriteFile(filepath.Join(dir, "chart.svg"), []byte(sampleSVG), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = srv

	rec := get(e, "/chart.svg")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<rect") {
		t.Errorf("body not served: %q", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "image/svg+xml") {
		t.Errorf("Content-Type = %q, want image/svg+xml", ct)
	}
	// SVG must be inert even when navigated to directly.
	if csp := rec.Header().Get("Content-Security-Policy"); csp != "sandbox" {
		t.Errorf("Content-Security-Policy = %q, want sandbox", csp)
	}
}

func TestServesFileInSubdirectory(t *testing.T) {
	_, e, path := makeServer(t, "x\n")
	dir := filepath.Dir(path)
	if err := os.MkdirAll(filepath.Join(dir, "img"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "img", "a.svg"), []byte(sampleSVG), 0o644); err != nil {
		t.Fatal(err)
	}
	if rec := get(e, "/img/a.svg"); rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestMissingFileIs404(t *testing.T) {
	_, e, _ := makeServer(t, "x\n")
	if rec := get(e, "/nope.svg"); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestDirectoryNotServed(t *testing.T) {
	_, e, path := makeServer(t, "x\n")
	dir := filepath.Dir(path)
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if rec := get(e, "/sub"); rec.Code != http.StatusNotFound {
		t.Errorf("directory served: status = %d, want 404", rec.Code)
	}
}

// The notebook directory is the boundary. Anything above it must be unreachable,
// whether asked for plainly or with the traversal percent-encoded.
func TestPathTraversalBlocked(t *testing.T) {
	_, e, path := makeServer(t, "x\n")
	dir := filepath.Dir(path)

	secret := filepath.Join(filepath.Dir(dir), "secret.txt")
	if err := os.WriteFile(secret, []byte("TOPSECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(secret) })

	for _, p := range []string{
		"/../secret.txt",
		"/..%2Fsecret.txt",
		"/%2e%2e%2fsecret.txt",
		"/foo/../../secret.txt",
	} {
		rec := get(e, p)
		if strings.Contains(rec.Body.String(), "TOPSECRET") {
			t.Errorf("%s LEAKED the file above the notebook directory", p)
		}
		if rec.Code == http.StatusOK {
			t.Errorf("%s returned 200; expected refusal", p)
		}
	}
}

// A symlink inside the notebook directory could otherwise point anywhere.
func TestSymlinkEscapeBlocked(t *testing.T) {
	_, e, path := makeServer(t, "x\n")
	dir := filepath.Dir(path)

	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("OUTSIDE"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	rec := get(e, "/link.txt")
	if strings.Contains(rec.Body.String(), "OUTSIDE") {
		t.Error("symlink escaped the notebook directory")
	}
	if rec.Code == http.StatusOK {
		t.Errorf("status = %d; expected refusal", rec.Code)
	}
}

func TestDotfilesBlocked(t *testing.T) {
	_, e, path := makeServer(t, "x\n")
	dir := filepath.Dir(path)

	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("[core]"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{"/.env", "/.git/config"} {
		rec := get(e, p)
		if rec.Code == http.StatusOK {
			t.Errorf("%s was served; dotfiles must be refused", p)
		}
		if strings.Contains(rec.Body.String(), "SECRET=1") || strings.Contains(rec.Body.String(), "[core]") {
			t.Errorf("%s leaked content", p)
		}
	}
}

// A root-level catch-all is the obvious way to shadow the real endpoints —
// this is the regression that would matter most.
func TestExistingRoutesStillWin(t *testing.T) {
	src := "Prose.\n\n```sh\necho hi\n```\n"
	_, e, path := makeServer(t, src)
	dir := filepath.Dir(path)

	// Files named after real routes must not be able to hijack them.
	for _, name := range []string{"picker", "interrupt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("HIJACKED"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		path string
		want string
	}{
		{"/", "cell-1"},
		{"/cell/1", "run-btn"},
		{"/prose/0", "prose"},
		{"/static/style.css", "--fg"},
		{"/picker", "Notebooks"},
	}
	for _, tc := range cases {
		rec := get(e, tc.path)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", tc.path, rec.Code)
			continue
		}
		if strings.Contains(rec.Body.String(), "HIJACKED") {
			t.Errorf("%s was hijacked by a file of the same name", tc.path)
		}
		if !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("%s: body missing %q", tc.path, tc.want)
		}
	}
}

// The end-to-end promise: the same markdown that renders on GitHub renders here.
func TestMarkdownImageLinkResolves(t *testing.T) {
	_, e, path := makeServer(t, "![chart](chart.svg)\n")
	dir := filepath.Dir(path)
	if err := os.WriteFile(filepath.Join(dir, "chart.svg"), []byte(sampleSVG), 0o644); err != nil {
		t.Fatal(err)
	}

	// The page must emit a relative <img src>, unchanged from the markdown.
	rec := get(e, "/")
	if !strings.Contains(rec.Body.String(), `<img src="chart.svg"`) {
		t.Fatalf("expected relative img src in page: %s", rec.Body.String())
	}
	// And that src must resolve.
	if rec := get(e, "/chart.svg"); rec.Code != http.StatusOK {
		t.Errorf("img src did not resolve: status = %d", rec.Code)
	}
}
