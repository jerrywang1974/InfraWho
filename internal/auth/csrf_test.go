package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://localhost:8080/api/v1/auth/login", nil)
	req.Host = "localhost:8080"
	req.Header.Set("Origin", "http://localhost:8080")
	if !CheckOrigin(req, nil) {
		t.Fatal("same-origin should pass")
	}

	req.Header.Set("Origin", "http://evil.example")
	if CheckOrigin(req, nil) {
		t.Fatal("cross-origin should fail")
	}

	req.Header.Set("Origin", "http://app.example:443")
	if !CheckOrigin(req, []string{"http://app.example:443"}) {
		t.Fatal("trusted origin should pass")
	}

	get := httptest.NewRequest(http.MethodGet, "http://localhost:8080/api/v1/auth/me", nil)
	if !CheckOrigin(get, nil) {
		t.Fatal("GET should skip origin check")
	}

	ref := httptest.NewRequest(http.MethodPost, "http://localhost:8080/x", nil)
	ref.Host = "localhost:8080"
	ref.Header.Set("Referer", "http://localhost:8080/ui")
	if !CheckOrigin(ref, nil) {
		t.Fatal("referer fallback should pass")
	}
}
