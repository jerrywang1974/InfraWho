package audit_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jerrywang1974/InfraWho/internal/audit"
	"github.com/jerrywang1974/InfraWho/internal/auth"
	"github.com/jerrywang1974/InfraWho/internal/db"
	"github.com/jerrywang1974/InfraWho/internal/ratelimit"
)

func TestWriteAndList(t *testing.T) {
	sqlDB, err := db.Open("sqlite::memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	store := audit.NewStore(sqlDB)
	resource := "res-1"
	if err := store.Write(audit.WriteInput{
		Action:       audit.ActionCredentialReveal,
		ResourceType: "account",
		ResourceID:   &resource,
		Outcome:      audit.OutcomeSuccess,
		IP:           "127.0.0.1",
		UserAgent:    "test",
	}); err != nil {
		t.Fatal(err)
	}

	items, total, err := store.List(audit.ListFilter{Limit: 10, Action: audit.ActionCredentialReveal})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("list: total=%d items=%d", total, len(items))
	}
	if items[0].Action != audit.ActionCredentialReveal || items[0].Outcome != audit.OutcomeSuccess {
		t.Fatalf("event: %#v", items[0])
	}
}

func TestListRequiresAdmin(t *testing.T) {
	sqlDB, err := db.Open("sqlite::memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	authStore := auth.NewStore(sqlDB)
	ah := auth.NewHandler(authStore, ratelimit.New(), auth.Options{
		CookieSecure:   false,
		MasterKeyReady: func() bool { return true },
	})
	auditStore := audit.NewStore(sqlDB)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/setup/bootstrap", ah.Bootstrap)
	mux.HandleFunc("/api/v1/auth/login", ah.Login)
	audit.NewHandler(auditStore).Register(mux)
	srv := ah.Middleware(mux)

	body := `{"username":"admin","password":"password1","acknowledge_kek_offline":true,"acknowledge_kek_irrecoverable":true,"acknowledge_backup_planned":true}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/bootstrap", strings.NewReader(body))
	req.Host = "example.test"
	req.Header.Set("Origin", "http://example.test")
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap: %d %s", rec.Code, rec.Body.String())
	}
	var tok string
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.SessionCookieName {
			tok = c.Value
		}
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/audit-events", nil)
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/audit-events?limit=10", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: tok})
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin list: %d %s", rec.Code, rec.Body.String())
	}
	var list map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&list)
	if _, ok := list["items"]; !ok {
		t.Fatalf("list body: %#v", list)
	}

	hash, err := auth.HashPassword("password1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authStore.CreateUser("op1", "Op", auth.RoleOperator, hash); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"op1","password":"password1"}`))
	req.Host = "example.test"
	req.Header.Set("Origin", "http://example.test")
	req.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(rec, req)
	opTok := ""
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.SessionCookieName {
			opTok = c.Value
		}
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/audit-events", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: opTok})
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("operator list: %d %s", rec.Code, rec.Body.String())
	}
}
