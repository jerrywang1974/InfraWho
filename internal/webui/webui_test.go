package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestWithSPAFallbackEmptyRoot(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	h, err := WithSPAFallback("", next)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestWithSPAFallbackServesIndexAndAPI(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>spa</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}

	apiHit := false
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiHit = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	h, err := WithSPAFallback(dir, api)
	if err != nil {
		t.Fatal(err)
	}

	// API path must reach next.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil))
	if !apiHit || rec.Code != http.StatusOK {
		t.Fatalf("api: hit=%v code=%d body=%s", apiHit, rec.Code, rec.Body.String())
	}

	// Static asset.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app.js", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "console.log(1)" {
		t.Fatalf("asset: code=%d body=%q", rec.Code, rec.Body.String())
	}

	// Client route → index.html
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/abc", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "<html>spa</html>" {
		t.Fatalf("spa: code=%d body=%q", rec.Code, rec.Body.String())
	}

	// healthz prefix style should not be treated as SPA when asked via isAPIPath
	if !isAPIPath("/healthz") || !isAPIPath("/readyz") || !isAPIPath("/metrics") {
		t.Fatal("ops paths should be API-classified")
	}
}

func TestWithSPAFallbackRequiresIndex(t *testing.T) {
	dir := t.TempDir()
	_, err := WithSPAFallback(dir, http.NotFoundHandler())
	if err == nil {
		t.Fatal("expected error without index.html")
	}
}
