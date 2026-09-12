package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCountersAndHandler(t *testing.T) {
	ResetForTest()
	IncLoginFailures()
	IncLoginFailures()
	IncReveal()
	IncRateLimited()
	IncRateLimited()
	IncRateLimited()

	lf, rv, rl := Snapshot()
	if lf != 2 || rv != 1 || rl != 3 {
		t.Fatalf("snapshot = %d %d %d, want 2 1 3", lf, rv, rl)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	s := string(body)
	for _, line := range []string{
		"login_failures_total 2",
		"reveal_total 1",
		"rate_limited_total 3",
	} {
		if !strings.Contains(s, line) {
			t.Fatalf("body missing %q:\n%s", line, s)
		}
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Fatalf("Content-Type = %q", ct)
	}
}

func TestHandlerMethodNotAllowed(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/metrics", nil)
	Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}
