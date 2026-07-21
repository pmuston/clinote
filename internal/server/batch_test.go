package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/pmuston/clinote/internal/notebook"
)

// waitForBatch polls until no batch is running, or fails.
func waitForBatch(t *testing.T, srv *Server) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		srv.mu.Lock()
		active := srv.batch.active
		srv.mu.Unlock()
		if !active {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("batch did not finish within 10s")
}

func post(e *echo.Echo, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// outputBodies returns the body of every output block, in order.
func outputBodies(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	nb, err := notebook.Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, b := range nb.Blocks {
		if ob, ok := b.(notebook.OutputBlock); ok {
			out = append(out, strings.TrimSpace(ob.Body(nb.Source)))
		}
	}
	return out
}

func TestRunAllRunsEveryCellInOrder(t *testing.T) {
	src := "```sh\necho one\n```\n\n```sh\necho two\n```\n\n```sh\necho three\n```\n"
	srv, e, path := makeServer(t, src)

	if rec := post(e, "/run-all"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	waitForBatch(t, srv)

	got := outputBodies(t, path)
	want := []string{"one", "two", "three"}
	if len(got) != len(want) {
		t.Fatalf("got %d outputs %v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("output %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// The regression this whole design turns on: completing a cell splices an
// output block in after it, shifting every later block's index. A batch that
// captured indices up front would drift onto the wrong cells.
func TestRunAllSurvivesIndexShift(t *testing.T) {
	// Interleaved prose maximises the shifting: each output insertion moves
	// every subsequent block by one.
	src := "Intro.\n\n```sh\necho alpha\n```\n\nMiddle prose.\n\n```sh\necho beta\n```\n\nMore prose.\n\n```sh\necho gamma\n```\n"
	srv, e, path := makeServer(t, src)

	post(e, "/run-all")
	waitForBatch(t, srv)

	got := outputBodies(t, path)
	want := []string{"alpha", "beta", "gamma"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v — indices drifted", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("output %d = %q, want %q — wrong cell ran", i, got[i], want[i])
		}
	}

	// Each output must sit directly after its own command, not somewhere else.
	raw, _ := os.ReadFile(path)
	for _, pair := range []string{
		"```sh\necho alpha\n```\n\n```output type=text exit=0",
		"```sh\necho beta\n```\n\n```output type=text exit=0",
		"```sh\necho gamma\n```\n\n```output type=text exit=0",
	} {
		if !strings.Contains(string(raw), pair) {
			t.Errorf("output not paired with its command:\n%s", raw)
		}
	}
}

func TestRunAllStopsAtFirstFailure(t *testing.T) {
	src := "```sh\necho first\n```\n\n```sh\necho boom >&2; false\n```\n\n```sh\necho never\n```\n"
	srv, e, path := makeServer(t, src)

	post(e, "/run-all")
	waitForBatch(t, srv)

	got := outputBodies(t, path)
	if len(got) != 2 {
		t.Fatalf("expected 2 outputs (ran until the failure), got %d: %v", len(got), got)
	}
	if got[0] != "first" {
		t.Errorf("first output = %q", got[0])
	}
	// The failing cell records stderr, per the exit-code stream pick.
	if got[1] != "boom" {
		t.Errorf("failing cell output = %q, want stderr %q", got[1], "boom")
	}
	// The third command must still be there but carry no output block — the
	// command text alone proves nothing, since an unrun cell keeps its source.
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "```sh\necho never\n```") {
		t.Fatalf("the unrun command should still be present:\n%s", raw)
	}
	if strings.Contains(string(raw), "echo never\n```\n\n```output") {
		t.Error("the cell after the failure ran; the batch must stop")
	}
}

func TestRunFromSkipsEarlierCells(t *testing.T) {
	src := "```sh\necho skipped\n```\n\n```sh\necho fromhere\n```\n\n```sh\necho andthis\n```\n"
	srv, e, path := makeServer(t, src)

	// Block index 1 is the second command (0 and 1 are the two sh blocks with
	// only whitespace between, so no prose blocks intervene).
	srv.mu.Lock()
	idxs := commandIndices(srv.nb)
	srv.mu.Unlock()
	if len(idxs) != 3 {
		t.Fatalf("expected 3 commands, got %d", len(idxs))
	}

	if rec := post(e, "/run-all?from="+strconv.Itoa(idxs[1])); rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	waitForBatch(t, srv)

	got := outputBodies(t, path)
	if len(got) != 2 {
		t.Fatalf("expected 2 outputs, got %d: %v", len(got), got)
	}
	if got[0] != "fromhere" || got[1] != "andthis" {
		t.Errorf("got %v, want [fromhere andthis]", got)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "skipped\n```\n\n```output") {
		t.Error("the cell before the start point ran")
	}
}

func TestRunAllRejectsConcurrentRuns(t *testing.T) {
	src := "```sh\nsleep 1\n```\n\n```sh\necho done\n```\n"
	srv, e, _ := makeServer(t, src)

	post(e, "/run-all")
	// A single run while the batch holds the runner must be refused.
	if rec := post(e, "/run/0"); rec.Code != http.StatusConflict {
		t.Errorf("single run during batch: status = %d, want 409", rec.Code)
	}
	// And a second batch.
	if rec := post(e, "/run-all"); rec.Code != http.StatusConflict {
		t.Errorf("second batch: status = %d, want 409", rec.Code)
	}
	waitForBatch(t, srv)
}

func TestInterruptAbortsBatch(t *testing.T) {
	src := "```sh\nsleep 5\n```\n\n```sh\necho should-not-run\n```\n"
	srv, e, path := makeServer(t, src)

	post(e, "/run-all")
	time.Sleep(300 * time.Millisecond) // let the sleep get going
	if rec := post(e, "/interrupt"); rec.Code != http.StatusNoContent {
		t.Fatalf("interrupt status = %d", rec.Code)
	}
	waitForBatch(t, srv)

	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "should-not-run\n```\n\n```output") {
		t.Error("batch continued past the interrupt")
	}
	if n := len(outputBodies(t, path)); n != 1 {
		t.Errorf("expected only the interrupted cell to record output, got %d", n)
	}
}

// While a batch runs the body carries a poller; once it ends the poller must
// be gone, or the page would keep polling forever.
func TestBatchPollerSelfTerminates(t *testing.T) {
	src := "```sh\nsleep 1\n```\n"
	srv, e, _ := makeServer(t, src)

	rec := post(e, "/run-all")
	if !strings.Contains(rec.Body.String(), "batch-bar") {
		t.Errorf("expected the batch poller while running: %s", rec.Body.String())
	}
	waitForBatch(t, srv)

	after := get(e, "/notebook").Body.String()
	if strings.Contains(after, "batch-bar") {
		t.Error("poller still present after the batch finished; polling would never stop")
	}
}

func TestRunAllOnNotebookWithNoCells(t *testing.T) {
	_, e, _ := makeServer(t, "just prose\n")
	if rec := post(e, "/run-all"); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestRunAllFromNonCommandBlockRejected(t *testing.T) {
	_, e, _ := makeServer(t, "prose here\n\n```sh\necho x\n```\n")
	// Block 0 is the prose block.
	if rec := post(e, "/run-all?from=0"); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
