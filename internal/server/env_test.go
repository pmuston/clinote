package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMissingEnvHelper(t *testing.T) {
	t.Setenv("CLINOTE_TEST_SET", "value")
	t.Setenv("CLINOTE_TEST_EMPTY", "")

	got := missingEnv([]string{
		"CLINOTE_TEST_SET",
		"CLINOTE_TEST_EMPTY",
		"CLINOTE_TEST_UNSET",
		"  ", // blank entries are ignored rather than reported
	})

	want := map[string]bool{"CLINOTE_TEST_EMPTY": true, "CLINOTE_TEST_UNSET": true}
	if len(got) != len(want) {
		t.Fatalf("missingEnv = %v, want keys %v", got, want)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected %q in missing list", g)
		}
	}
}

func TestBannerShownWhenRequiredEnvMissing(t *testing.T) {
	t.Setenv("CLINOTE_TEST_ABSENT", "")
	src := "---\ntitle: T\nrequires:\n  - CLINOTE_TEST_ABSENT\n---\n\nbody\n"
	_, e, _ := makeServer(t, src)

	body := get(e, "/").Body.String()
	if !strings.Contains(body, "env-warning") {
		t.Fatalf("expected the banner, got:\n%s", body)
	}
	if !strings.Contains(body, "CLINOTE_TEST_ABSENT") {
		t.Errorf("banner should name the variable: %s", body)
	}
}

func TestNoBannerWhenRequiredEnvPresent(t *testing.T) {
	t.Setenv("CLINOTE_TEST_PRESENT", "secret-value")
	src := "---\ntitle: T\nrequires:\n  - CLINOTE_TEST_PRESENT\n---\n\nbody\n"
	_, e, _ := makeServer(t, src)

	body := get(e, "/").Body.String()
	if strings.Contains(body, "env-warning") {
		t.Errorf("banner shown despite the variable being set:\n%s", body)
	}
	// The value itself must never reach the page.
	if strings.Contains(body, "secret-value") {
		t.Error("the environment variable's VALUE leaked into the page")
	}
}

func TestNoBannerWithoutRequires(t *testing.T) {
	_, e, _ := makeServer(t, "plain notebook\n")
	if strings.Contains(get(e, "/").Body.String(), "env-warning") {
		t.Error("banner shown for a notebook with no requires:")
	}
}

// The banner reports; it must not prevent running.
func TestMissingEnvDoesNotBlockRunning(t *testing.T) {
	t.Setenv("CLINOTE_TEST_ABSENT2", "")
	src := "---\ntitle: T\nrequires:\n  - CLINOTE_TEST_ABSENT2\n---\n\n```sh\necho ran anyway\n```\n"
	_, e, _ := makeServer(t, src)

	req := httptest.NewRequest(http.MethodPost, "/run/0", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("run blocked by missing env: status %d body=%s", rec.Code, rec.Body.String())
	}
}
