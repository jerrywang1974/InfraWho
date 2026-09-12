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
	if !CheckOrigin(req, nil, false) {
		t.Fatal("same-origin should pass")
	}

	req.Header.Set("Origin", "http://evil.example")
	if CheckOrigin(req, nil, false) {
		t.Fatal("cross-origin should fail")
	}

	req.Header.Set("Origin", "https://app.example")
	if !CheckOrigin(req, []string{"https://app.example"}, false) {
		t.Fatal("trusted origin with matching scheme should pass")
	}
	req.Header.Set("Origin", "http://app.example")
	if CheckOrigin(req, []string{"https://app.example"}, false) {
		t.Fatal("trusted origin scheme mismatch should fail")
	}
	req.Header.Set("Origin", "http://bare.example")
	if !CheckOrigin(req, []string{"bare.example"}, false) {
		t.Fatal("bare host trusted entry should match any scheme")
	}

	get := httptest.NewRequest(http.MethodGet, "http://localhost:8080/api/v1/auth/me", nil)
	if !CheckOrigin(get, nil, false) {
		t.Fatal("GET should skip origin check")
	}

	ref := httptest.NewRequest(http.MethodPost, "http://localhost:8080/x", nil)
	ref.Host = "localhost:8080"
	ref.Header.Set("Referer", "http://localhost:8080/ui")
	if !CheckOrigin(ref, nil, false) {
		t.Fatal("referer fallback should pass")
	}

	missing := httptest.NewRequest(http.MethodPost, "http://localhost:8080/x", nil)
	missing.Host = "localhost:8080"
	if CheckOrigin(missing, nil, false) {
		t.Fatal("missing Origin/Referer should fail")
	}
}

func TestCheckOriginRejectsXFHSpoofWithoutTrustProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://victim.test/api/v1/setup/bootstrap", nil)
	req.Host = "victim.test"
	req.Header.Set("Origin", "http://evil.test")
	req.Header.Set("X-Forwarded-Host", "evil.test")
	if CheckOrigin(req, nil, false) {
		t.Fatal("X-Forwarded-Host must not be trusted when TrustProxy is false")
	}
	if !CheckOrigin(req, nil, true) {
		t.Fatal("X-Forwarded-Host should be honored when TrustProxy is true")
	}
}
