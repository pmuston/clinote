package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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

func TestAddCellAppendsAndPersists(t *testing.T) {
	src := "Just some prose.\n"
	srv, e, path := makeServer(t, src)

	cmdsBefore, _, _ := func() (c, o, p int) {
		for _, b := range srv.nb.Blocks {
			switch b.(type) {
			case notebook.CommandBlock:
				c++
			case notebook.OutputBlock:
				o++
			case notebook.ProseBlock:
				p++
			}
		}
		return
	}()
	if cmdsBefore != 0 {
		t.Fatalf("setup: expected 0 commands, got %d", cmdsBefore)
	}

	req := httptest.NewRequest(http.MethodPost, "/add-cell", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	// Response includes the add-cell controls and the new (empty) sh fence.
	if !strings.Contains(rec.Body.String(), "+ sh cell") || !strings.Contains(rec.Body.String(), "+ prose") {
		t.Errorf("response missing add buttons: %s", rec.Body.String())
	}

	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(onDisk, []byte("```sh\n```")) {
		t.Errorf("on-disk file missing empty appended cell: %s", onDisk)
	}
	if !bytes.HasPrefix(onDisk, []byte("Just some prose.")) {
		t.Errorf("on-disk file lost original prose: %s", onDisk)
	}
}

func TestAddCellKindProse(t *testing.T) {
	src := "Prose only.\n"
	_, e, path := makeServer(t, src)

	req := httptest.NewRequest(http.MethodPost, "/add-cell?kind=prose", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	onDisk, _ := os.ReadFile(path)
	// kind=prose adds an empty prose block (no sh fence).
	if bytes.Contains(onDisk, []byte("```sh")) {
		t.Errorf("kind=prose should not add an sh cell:\n%s", onDisk)
	}
	// The file must still contain the original prose.
	if !bytes.HasPrefix(onDisk, []byte("Prose only.")) {
		t.Errorf("on-disk file lost original prose: %s", onDisk)
	}
}

func TestAddProseStartsInEditMode(t *testing.T) {
	// Adding a prose block returns a response with the new block already in
	// edit mode (textarea visible) so the user can type immediately.
	_, e, _ := makeServer(t, "Existing.\n")
	req := httptest.NewRequest(http.MethodPost, "/add-cell?kind=prose", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `class="prose prose-edit"`) {
		t.Errorf("expected new prose in edit mode: %s", rec.Body.String())
	}
}

func TestAddCellStartsInEditModeOnlyWhenEditable(t *testing.T) {
	// With editable: true the new sh cell renders in edit mode.
	srcEditable := "---\ntitle: T\neditable: true\n---\n\nfoo\n"
	_, eEditable, _ := makeServer(t, srcEditable)
	req := httptest.NewRequest(http.MethodPost, "/add-cell?kind=sh", nil)
	rec := httptest.NewRecorder()
	eEditable.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `class="cell cell-editing"`) {
		t.Errorf("editable=true: expected new cell in edit mode: %s", rec.Body.String())
	}

	// Without editable, the new sh cell renders in view mode (no textarea).
	_, ePlain, _ := makeServer(t, "foo\n")
	req2 := httptest.NewRequest(http.MethodPost, "/add-cell?kind=sh", nil)
	rec2 := httptest.NewRecorder()
	ePlain.ServeHTTP(rec2, req2)
	if strings.Contains(rec2.Body.String(), "cell-editing") {
		t.Errorf("editable=false: should NOT start in edit mode: %s", rec2.Body.String())
	}
}

func TestDeleteCellGatedByEditable(t *testing.T) {
	// Without editable, delete is 403.
	src := "```sh\necho hi\n```\n"
	_, e, _ := makeServer(t, src)
	req := httptest.NewRequest(http.MethodPost, "/block/0/delete", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 without editable, got %d", rec.Code)
	}

	// With editable, delete succeeds and the file becomes empty.
	srcE := "---\ntitle: T\neditable: true\n---\n\n```sh\necho hi\n```\n"
	_, eE, path := makeServer(t, srcE)
	req2 := httptest.NewRequest(http.MethodPost, "/block/2/delete", nil) // idx 2: cmd (idx 0 was a prose-like leading text? actually let me find it)
	rec2 := httptest.NewRecorder()
	// Find the cmd idx first.
	cmdIdx := -1
	srvE, _, _ := makeServer(t, srcE)
	_ = srvE // not used here; just need to find idx via reading the response
	// Simpler: GET / and parse
	reqIdx := httptest.NewRequest(http.MethodGet, "/", nil)
	recIdx := httptest.NewRecorder()
	eE.ServeHTTP(recIdx, reqIdx)
	// We know from the source structure that the cmd is the only sh fence — find its idx by looking for "Run" / cell-N
	for i := 0; i < 5; i++ {
		if strings.Contains(recIdx.Body.String(), `id="cell-`+strconv.Itoa(i)+`"`) {
			cmdIdx = i
			break
		}
	}
	if cmdIdx < 0 {
		t.Fatal("cmd idx not found")
	}
	req2 = httptest.NewRequest(http.MethodPost, "/block/"+strconv.Itoa(cmdIdx)+"/delete", nil)
	eE.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("delete status %d body=%s", rec2.Code, rec2.Body.String())
	}
	post, _ := os.ReadFile(path)
	if bytes.Contains(post, []byte("```sh")) {
		t.Errorf("cell not deleted from file: %s", post)
	}
}

func TestDeleteProseAlwaysAllowed(t *testing.T) {
	// Prose can be deleted without editable flag.
	src := "Some prose here.\n"
	_, e, path := makeServer(t, src)
	req := httptest.NewRequest(http.MethodPost, "/block/0/delete", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	post, _ := os.ReadFile(path)
	if bytes.Contains(post, []byte("Some prose here")) {
		t.Errorf("prose not deleted from file: %s", post)
	}
}

func TestCellEditDisabledByDefault(t *testing.T) {
	// No editable flag → GET /cell/0/edit must be forbidden.
	src := "```sh\necho hi\n```\n"
	_, e, _ := makeServer(t, src)

	req := httptest.NewRequest(http.MethodGet, "/cell/0/edit", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Render must NOT include an edit button on the cell.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), "cell-edit-btn") {
		t.Errorf("page rendered cell-edit button without editable=true: %s", rec.Body.String())
	}
}

func TestCellEditEnabledByFrontMatter(t *testing.T) {
	src := "---\ntitle: T\neditable: true\n---\n\n```sh\necho hi\n```\n"
	_, e, path := makeServer(t, src)

	// Edit-form returns 200 with a textarea containing the body.
	req := httptest.NewRequest(http.MethodGet, "/cell/0/edit", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<textarea") {
		t.Errorf("expected textarea, got: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "echo hi") {
		t.Errorf("textarea missing body content: %s", rec.Body.String())
	}

	// Save a new body.
	form := url.Values{}
	form.Set("text", "echo replaced\n")
	req = httptest.NewRequest(http.MethodPost, "/cell/0/edit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	// Verify on-disk file.
	onDisk, _ := os.ReadFile(path)
	if !bytes.Contains(onDisk, []byte("echo replaced")) {
		t.Errorf("on-disk file missing edited body: %s", onDisk)
	}
	if bytes.Contains(onDisk, []byte("echo hi\n```")) {
		t.Errorf("old body still present: %s", onDisk)
	}

	// Index should now show the edit button on the cell.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "cell-edit-btn") {
		t.Errorf("expected edit button when editable=true: %s", rec.Body.String())
	}
}

func TestWidthFullAppliesWideClass(t *testing.T) {
	// width: full → main gets .wide class.
	src := "---\ntitle: T\nwidth: full\n---\n\nbody\n"
	_, e, _ := makeServer(t, src)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `id="notebook" class="wide"`) {
		t.Errorf("expected wide class on main, got: %s", rec.Body.String())
	}
}

func TestNoWidthDefaultsNarrow(t *testing.T) {
	// No width field → no .wide class on main.
	src := "body\n"
	_, e, _ := makeServer(t, src)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), `class="wide"`) {
		t.Errorf("did not expect wide class without width field: %s", rec.Body.String())
	}
}

func TestRunWithOutCSVHintRendersTable(t *testing.T) {
	// `sh out=csv` → output block's type= reflects the hint → renderer picks CSV.
	src := "```sh out=csv\nprintf 'col1,col2\\n1,2\\n3,4\\n'\n```\n"
	_, e, path := makeServer(t, src)

	req := httptest.NewRequest(http.MethodPost, "/run/0", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("run status = %d body=%s", rec.Code, rec.Body.String())
	}
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
	// Browser-side payload is a table, not a pre.
	if !strings.Contains(final, "<table") {
		t.Errorf("expected <table> in rendered output, got:\n%s", final)
	}
	if !strings.Contains(final, "col1") || !strings.Contains(final, "col2") {
		t.Errorf("expected CSV header cells in output: %s", final)
	}
	// On disk: the output block's type= is "csv", not "text".
	onDisk, _ := os.ReadFile(path)
	if !bytes.Contains(onDisk, []byte("```output type=csv ")) {
		t.Errorf("expected type=csv in saved output block, got:\n%s", onDisk)
	}
}

func TestFormatPickerUpdatesBothAttrs(t *testing.T) {
	// Start with a cell that has run, producing type=text. Then change to csv.
	src := "---\ntitle: T\neditable: true\n---\n\n```sh\nprintf 'a,b\\n1,2\\n'\n```\n"
	_, e, path := makeServer(t, src)

	// Run first so an output block exists.
	req := httptest.NewRequest(http.MethodPost, "/run/0", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, "/cell/0", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if !strings.Contains(rec.Body.String(), "spinner") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Sanity: pre-change file shows type=text.
	pre, _ := os.ReadFile(path)
	if !bytes.Contains(pre, []byte("```output type=text")) {
		t.Fatalf("pre-state expected type=text, got: %s", pre)
	}

	// Change format to csv.
	form := url.Values{}
	form.Set("type", "csv")
	req = httptest.NewRequest(http.MethodPost, "/cell/0/format", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("format status = %d body=%s", rec.Code, rec.Body.String())
	}
	// Response should render the output as a table.
	if !strings.Contains(rec.Body.String(), "<table") {
		t.Errorf("expected table in response: %s", rec.Body.String())
	}

	post, _ := os.ReadFile(path)
	// 1) Command's `out=csv` was added.
	if !bytes.Contains(post, []byte("```sh out=csv\n")) {
		t.Errorf("expected sh out=csv on command, got: %s", post)
	}
	// 2) Output block's type= updated to csv.
	if !bytes.Contains(post, []byte("```output type=csv ")) {
		t.Errorf("expected output type=csv, got: %s", post)
	}
}

func TestFormatPicker403WhenNotEditable(t *testing.T) {
	src := "```sh\necho hi\n```\n"
	_, e, _ := makeServer(t, src)
	form := url.Values{}
	form.Set("type", "csv")
	req := httptest.NewRequest(http.MethodPost, "/cell/0/format", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestFormatPickerRendersInEditableNotebook(t *testing.T) {
	// With editable, the cell controls include the format picker <select>.
	src := "---\ntitle: T\neditable: true\n---\n\n```sh\necho hi\n```\n"
	_, e, _ := makeServer(t, src)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, `class="format-picker"`) {
		t.Errorf("expected format-picker in editable cell: %s", body)
	}
	// Default selection is text (no out= attr present).
	if !strings.Contains(body, `<option value="text" selected`) {
		t.Errorf("expected text selected by default: %s", body)
	}
}

func TestFormatPickerHiddenWhenNotEditable(t *testing.T) {
	src := "```sh\necho hi\n```\n"
	_, e, _ := makeServer(t, src)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), "format-picker") {
		t.Errorf("format-picker should not render without editable: %s", rec.Body.String())
	}
}

func TestSuccessShowsStdoutOnly(t *testing.T) {
	// stdout has the payload, stderr has noise; exit=0 → save stdout only.
	src := "```sh\nprintf 'PAYLOAD\\n'; printf 'noise\\n' 1>&2\n```\n"
	_, e, path := makeServer(t, src)

	req := httptest.NewRequest(http.MethodPost, "/run/0", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, "/cell/0", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if !strings.Contains(rec.Body.String(), "spinner") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	onDisk, _ := os.ReadFile(path)
	// Look only inside the output block — the command source mentions "noise".
	outBody := extractOutputBody(t, onDisk)
	if !strings.Contains(outBody, "PAYLOAD") {
		t.Errorf("expected stdout payload in output body: %q", outBody)
	}
	if strings.Contains(outBody, "noise") {
		t.Errorf("stderr noise should NOT appear in output body on exit=0: %q", outBody)
	}
}

// extractOutputBody returns the body of the first ```output ... ``` block.
func extractOutputBody(t *testing.T, file []byte) string {
	t.Helper()
	lines := strings.Split(string(file), "\n")
	in := false
	var body []string
	for _, l := range lines {
		if strings.HasPrefix(l, "```output") {
			in = true
			continue
		}
		if in && strings.HasPrefix(l, "```") {
			return strings.Join(body, "\n")
		}
		if in {
			body = append(body, l)
		}
	}
	t.Fatalf("no output block found in: %s", file)
	return ""
}

func TestFailureShowsStderr(t *testing.T) {
	// stderr has the error, stdout has nothing; exit=1 (via `false` to avoid
	// `exit N` which would kill the persistent interactive shell) → save stderr.
	src := "```sh\nprintf 'ERROR-MSG\\n' 1>&2; false\n```\n"
	_, e, path := makeServer(t, src)

	req := httptest.NewRequest(http.MethodPost, "/run/0", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, "/cell/0", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if !strings.Contains(rec.Body.String(), "spinner") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	onDisk, _ := os.ReadFile(path)
	if !bytes.Contains(onDisk, []byte("ERROR-MSG")) {
		t.Errorf("expected stderr error in saved output on failure: %s", onDisk)
	}
	if !bytes.Contains(onDisk, []byte("exit=1")) {
		t.Errorf("expected exit=1 in attrs: %s", onDisk)
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
