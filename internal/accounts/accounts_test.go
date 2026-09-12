package accounts_test

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jerrywang1974/InfraWho/internal/accounts"
	"github.com/jerrywang1974/InfraWho/internal/assets"
	"github.com/jerrywang1974/InfraWho/internal/audit"
	"github.com/jerrywang1974/InfraWho/internal/auth"
	"github.com/jerrywang1974/InfraWho/internal/config"
	"github.com/jerrywang1974/InfraWho/internal/db"
	"github.com/jerrywang1974/InfraWho/internal/ratelimit"
)

func writeTempKEK(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "master.key")
	key := make([]byte, config.MasterKeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testServer(t *testing.T) (http.Handler, *audit.Store, string) {
	t.Helper()
	sqlDB, err := db.Open("sqlite::memory:")
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	kekPath := writeTempKEK(t)
	limiter := ratelimit.New()
	authStore := auth.NewStore(sqlDB)
	ah := auth.NewHandler(authStore, limiter, auth.Options{
		CookieSecure:   false,
		MasterKeyReady: func() bool { return true },
	})
	auditStore := audit.NewStore(sqlDB)
	assetH := assets.NewHandler(assets.NewStore(sqlDB), assets.Options{})
	accountStore := accounts.NewStore(sqlDB, accounts.StoreOptions{
		KeyVersion: "kek-v1",
		LoadKEK: func() ([]byte, error) {
			return config.LoadMasterKey(kekPath)
		},
	})
	accountH := accounts.NewHandler(accountStore, auditStore, accounts.Options{Limiter: limiter})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", ah.Login)
	mux.HandleFunc("/api/v1/auth/logout", ah.Logout)
	mux.HandleFunc("/api/v1/auth/me", ah.Me)
	mux.HandleFunc("/api/v1/auth/step-up", ah.StepUp)
	mux.HandleFunc("/api/v1/setup/status", ah.SetupStatus)
	mux.HandleFunc("/api/v1/setup/bootstrap", ah.Bootstrap)
	mux.HandleFunc("/api/v1/setup/acknowledge", ah.Acknowledge)
	assetH.Register(mux, ah.RequireOrigin)
	accountH.Register(mux, ah.RequireOrigin)
	audit.NewHandler(auditStore).Register(mux)
	return ah.Middleware(mux), auditStore, kekPath
}

func withOrigin(r *http.Request) *http.Request {
	r.Host = "example.test"
	r.Header.Set("Origin", "http://example.test")
	return r
}

func cookieValue(rec *httptest.ResponseRecorder, name string) string {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c.Value
		}
	}
	for _, line := range rec.Header().Values("Set-Cookie") {
		if strings.HasPrefix(line, name+"=") {
			rest := strings.TrimPrefix(line, name+"=")
			if i := strings.IndexByte(rest, ';'); i >= 0 {
				rest = rest[:i]
			}
			return rest
		}
	}
	return ""
}

func errorCode(body io.Reader) string {
	var errBody map[string]any
	_ = json.NewDecoder(body).Decode(&errBody)
	if errObj, _ := errBody["error"].(map[string]any); errObj != nil {
		if c, _ := errObj["code"].(string); c != "" {
			return c
		}
	}
	return ""
}

func bootstrap(t *testing.T, srv http.Handler) string {
	t.Helper()
	body := `{"username":"admin","password":"password1","acknowledge_kek_offline":true,"acknowledge_kek_irrecoverable":true,"acknowledge_backup_planned":true}`
	rec := httptest.NewRecorder()
	req := withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/setup/bootstrap", strings.NewReader(body)))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap: %d %s", rec.Code, rec.Body.String())
	}
	tok := cookieValue(rec, auth.SessionCookieName)
	if tok == "" {
		t.Fatal("missing session cookie")
	}
	return tok
}

func stepUp(t *testing.T, srv http.Handler, tok string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/auth/step-up", strings.NewReader(`{"password":"password1"}`)))
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: tok})
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("step-up: %d %s", rec.Code, rec.Body.String())
	}
}

func doJSON(t *testing.T, srv http.Handler, method, path, tok, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if method != http.MethodGet && method != http.MethodHead {
		r = withOrigin(r)
	}
	if tok != "" {
		r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: tok})
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, r)
	return rec
}

func createAsset(t *testing.T, srv http.Handler, tok, hostname string) string {
	t.Helper()
	body := `{"name":"Host","hostname":"` + hostname + `","asset_type":"vm","os_family":"linux","environment":"lab","purpose":"test"}`
	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets", tok, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create asset: %d %s", rec.Code, rec.Body.String())
	}
	var a map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&a)
	id, _ := a["id"].(string)
	if id == "" {
		t.Fatal("missing asset id")
	}
	return id
}

func TestCreateRevealRotateAndAuthTypeImmutable(t *testing.T) {
	srv, auditStore, _ := testServer(t)
	tok := bootstrap(t, srv)
	assetID := createAsset(t, srv, tok, "acc-1.example")

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+assetID+"/accounts", tok,
		`{"username":"deploy","auth_type":"password","description":"ci","secret":"s3cret"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var acc map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&acc)
	id, _ := acc["id"].(string)
	if id == "" || acc["has_secret"] != true || acc["username"] != "deploy" {
		t.Fatalf("created: %#v", acc)
	}
	if _, ok := acc["secret"]; ok {
		t.Fatal("create response must not include secret")
	}

	// Without step-up → 403 step_up_required
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/accounts/"+id+"/reveal", tok, "")
	if rec.Code != http.StatusForbidden || errorCode(rec.Body) != "step_up_required" {
		t.Fatalf("reveal without step-up: %d %s", rec.Code, rec.Body.String())
	}

	stepUp(t, srv, tok)
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/accounts/"+id+"/reveal", tok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reveal: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control: %q", rec.Header().Get("Cache-Control"))
	}
	var revealed map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&revealed)
	if revealed["secret"] != "s3cret" || revealed["account_id"] != id {
		t.Fatalf("reveal body: %#v", revealed)
	}

	events, total, err := auditStore.List(audit.ListFilter{Action: audit.ActionCredentialReveal, Limit: 10})
	if err != nil || total < 1 || len(events) < 1 {
		t.Fatalf("audit reveal: total=%d err=%v", total, err)
	}
	if events[0].Outcome != audit.OutcomeSuccess {
		t.Fatalf("audit outcome: %#v", events[0])
	}

	// K22: cannot PATCH auth_type when secret exists
	rec = doJSON(t, srv, http.MethodPatch, "/api/v1/accounts/"+id, tok, `{"auth_type":"ssh_private_key"}`)
	if rec.Code != http.StatusUnprocessableEntity || errorCode(rec.Body) != "validation_error" {
		t.Fatalf("patch auth_type: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/v1/accounts/"+id+"/rotate-secret", tok,
		`{"secret":"new-key","auth_type":"ssh_private_key"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate: %d %s", rec.Code, rec.Body.String())
	}
	_ = json.NewDecoder(rec.Body).Decode(&acc)
	if acc["auth_type"] != "ssh_private_key" || acc["has_secret"] != true {
		t.Fatalf("after rotate: %#v", acc)
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/v1/accounts/"+id+"/reveal", tok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reveal after rotate: %d %s", rec.Code, rec.Body.String())
	}
	_ = json.NewDecoder(rec.Body).Decode(&revealed)
	if revealed["secret"] != "new-key" || revealed["auth_type"] != "ssh_private_key" {
		t.Fatalf("reveal after rotate: %#v", revealed)
	}

	rotEvents, _, err := auditStore.List(audit.ListFilter{Action: audit.ActionCredentialRotate, Limit: 5})
	if err != nil || len(rotEvents) < 1 {
		t.Fatalf("rotate audit missing: %v", err)
	}
}

func TestCreateWithoutSecretAndPatchAuthType(t *testing.T) {
	srv, _, _ := testServer(t)
	tok := bootstrap(t, srv)
	assetID := createAsset(t, srv, tok, "acc-2.example")

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+assetID+"/accounts", tok,
		`{"username":"root","auth_type":"password","description":"unknown pwd"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var acc map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&acc)
	id, _ := acc["id"].(string)
	if acc["has_secret"] != false {
		t.Fatalf("expected no secret: %#v", acc)
	}

	rec = doJSON(t, srv, http.MethodPatch, "/api/v1/accounts/"+id, tok, `{"auth_type":"other","description":"note"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	_ = json.NewDecoder(rec.Body).Decode(&acc)
	if acc["auth_type"] != "other" {
		t.Fatalf("patched: %#v", acc)
	}

	stepUp(t, srv, tok)
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/accounts/"+id+"/reveal", tok, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("reveal no secret: %d %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteAccountAndEmptyPatch(t *testing.T) {
	srv, auditStore, _ := testServer(t)
	tok := bootstrap(t, srv)
	assetID := createAsset(t, srv, tok, "acc-3.example")

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+assetID+"/accounts", tok,
		`{"username":"svc","auth_type":"password","secret":"x"}`)
	var acc map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&acc)
	id, _ := acc["id"].(string)

	rec = doJSON(t, srv, http.MethodPatch, "/api/v1/accounts/"+id, tok, `{}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty patch: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, http.MethodPatch, "/api/v1/accounts/"+id, tok, `{"nope":1}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown field: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, srv, http.MethodDelete, "/api/v1/accounts/"+id, tok, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, http.MethodDelete, "/api/v1/accounts/"+id, tok, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete again: %d", rec.Code)
	}

	events, _, err := auditStore.List(audit.ListFilter{Action: audit.ActionAccountDelete, Limit: 5})
	if err != nil || len(events) < 1 {
		t.Fatalf("delete audit: %v", err)
	}
}

func TestRevealUnauthAndOrigin(t *testing.T) {
	srv, _, _ := testServer(t)
	tok := bootstrap(t, srv)
	assetID := createAsset(t, srv, tok, "acc-rbac.example")
	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+assetID+"/accounts", tok,
		`{"username":"u","auth_type":"password","secret":"p"}`)
	var acc map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&acc)
	id, _ := acc["id"].(string)

	rec = httptest.NewRecorder()
	req := withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/accounts/"+id+"/reveal", nil))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth reveal: %d %s", rec.Code, rec.Body.String())
	}

	stepUp(t, srv, tok)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/accounts/"+id+"/reveal", nil)
	req.Host = "example.test"
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: tok})
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || errorCode(rec.Body) != "forbidden" {
		t.Fatalf("reveal without origin: %d %s", rec.Code, rec.Body.String())
	}
}

func TestViewerCannotReveal(t *testing.T) {
	sqlDB, err := db.Open("sqlite::memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	kekPath := writeTempKEK(t)
	limiter := ratelimit.New()
	authStore := auth.NewStore(sqlDB)
	ah := auth.NewHandler(authStore, limiter, auth.Options{
		CookieSecure:   false,
		MasterKeyReady: func() bool { return true },
	})
	auditStore := audit.NewStore(sqlDB)
	assetH := assets.NewHandler(assets.NewStore(sqlDB), assets.Options{})
	accountStore := accounts.NewStore(sqlDB, accounts.StoreOptions{
		KeyVersion: "kek-v1",
		LoadKEK:    func() ([]byte, error) { return config.LoadMasterKey(kekPath) },
	})
	accountH := accounts.NewHandler(accountStore, auditStore, accounts.Options{Limiter: limiter})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", ah.Login)
	mux.HandleFunc("/api/v1/auth/step-up", ah.StepUp)
	mux.HandleFunc("/api/v1/setup/bootstrap", ah.Bootstrap)
	assetH.Register(mux, ah.RequireOrigin)
	accountH.Register(mux, ah.RequireOrigin)
	srv := ah.Middleware(mux)

	tok := bootstrap(t, srv)
	assetID := createAsset(t, srv, tok, "viewer-acc.example")
	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+assetID+"/accounts", tok,
		`{"username":"u","auth_type":"password","secret":"p"}`)
	var acc map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&acc)
	id, _ := acc["id"].(string)

	hash, err := auth.HashPassword("password1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authStore.CreateUser("viewer1", "V", auth.RoleViewer, hash); err != nil {
		t.Fatal(err)
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/auth/login", "", `{"username":"viewer1","password":"password1"}`)
	vtok := cookieValue(rec, auth.SessionCookieName)
	if vtok == "" {
		t.Fatal("viewer login failed")
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/accounts/"+id+"/reveal", vtok, "")
	if rec.Code != http.StatusForbidden || errorCode(rec.Body) != "forbidden" {
		t.Fatalf("viewer reveal: %d %s", rec.Code, rec.Body.String())
	}
}

func TestRevealRateLimit(t *testing.T) {
	sqlDB, err := db.Open("sqlite::memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	kekPath := writeTempKEK(t)
	limiter := ratelimit.New()
	authStore := auth.NewStore(sqlDB)
	ah := auth.NewHandler(authStore, limiter, auth.Options{
		CookieSecure:   false,
		MasterKeyReady: func() bool { return true },
	})
	auditStore := audit.NewStore(sqlDB)
	assetH := assets.NewHandler(assets.NewStore(sqlDB), assets.Options{})
	accountStore := accounts.NewStore(sqlDB, accounts.StoreOptions{
		KeyVersion: "kek-v1",
		LoadKEK:    func() ([]byte, error) { return config.LoadMasterKey(kekPath) },
	})
	accountH := accounts.NewHandler(accountStore, auditStore, accounts.Options{Limiter: limiter})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", ah.Login)
	mux.HandleFunc("/api/v1/auth/step-up", ah.StepUp)
	mux.HandleFunc("/api/v1/setup/bootstrap", ah.Bootstrap)
	assetH.Register(mux, ah.RequireOrigin)
	accountH.Register(mux, ah.RequireOrigin)
	srv := ah.Middleware(mux)

	tok := bootstrap(t, srv)
	assetID := createAsset(t, srv, tok, "rate.example")
	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+assetID+"/accounts", tok,
		`{"username":"u","auth_type":"password","secret":"p"}`)
	var acc map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&acc)
	id, _ := acc["id"].(string)
	stepUp(t, srv, tok)

	limited := 0
	for i := 0; i < auth.RevealSessionLimit+5; i++ {
		rec = doJSON(t, srv, http.MethodPost, "/api/v1/accounts/"+id+"/reveal", tok, "")
		if rec.Code == http.StatusTooManyRequests {
			if errorCode(rec.Body) != "rate_limited" {
				t.Fatalf("rate code: %s", rec.Body.String())
			}
			limited++
			continue
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("reveal %d: %d %s", i, rec.Code, rec.Body.String())
		}
	}
	if limited < 2 {
		t.Fatalf("expected multiple rate-limited responses, got %d", limited)
	}
	events, _, err := auditStore.List(audit.ListFilter{Action: audit.ActionRevealRateLimited, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("want one REVEAL_RATE_LIMITED audit, got %d", len(events))
	}
}

func TestCreateOnMissingAsset(t *testing.T) {
	srv, _, _ := testServer(t)
	tok := bootstrap(t, srv)
	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets/018f0000-0000-7000-8000-000000000099/accounts", tok,
		`{"username":"u","auth_type":"password"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing asset: %d %s", rec.Code, rec.Body.String())
	}
}

func TestCreateRejectsEmptySecret(t *testing.T) {
	srv, _, _ := testServer(t)
	tok := bootstrap(t, srv)
	assetID := createAsset(t, srv, tok, "empty-secret.example")
	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+assetID+"/accounts", tok,
		`{"username":"u","auth_type":"password","secret":""}`)
	if rec.Code != http.StatusUnprocessableEntity || errorCode(rec.Body) != "validation_error" {
		t.Fatalf("empty secret create: %d %s", rec.Code, rec.Body.String())
	}
}

func TestCreateOnSoftDeletedAsset(t *testing.T) {
	srv, _, _ := testServer(t)
	tok := bootstrap(t, srv)
	assetID := createAsset(t, srv, tok, "soft-acc.example")
	stepUp(t, srv, tok)
	rec := doJSON(t, srv, http.MethodDelete, "/api/v1/assets/"+assetID, tok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("soft-delete: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+assetID+"/accounts", tok,
		`{"username":"u","auth_type":"password"}`)
	if rec.Code != http.StatusConflict || errorCode(rec.Body) != "conflict" {
		t.Fatalf("create on soft-deleted: %d %s", rec.Code, rec.Body.String())
	}
}

type failAuditor struct {
	failOn string
	inner  accounts.Auditor
}

func (f *failAuditor) Write(in audit.WriteInput) error {
	if in.Action == f.failOn {
		return errors.New("forced audit failure")
	}
	if f.inner != nil {
		return f.inner.Write(in)
	}
	return nil
}

func TestRevealFailsClosedWhenAuditWriteFails(t *testing.T) {
	sqlDB, err := db.Open("sqlite::memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	kekPath := writeTempKEK(t)
	limiter := ratelimit.New()
	authStore := auth.NewStore(sqlDB)
	ah := auth.NewHandler(authStore, limiter, auth.Options{
		CookieSecure:   false,
		MasterKeyReady: func() bool { return true },
	})
	auditStore := audit.NewStore(sqlDB)
	assetH := assets.NewHandler(assets.NewStore(sqlDB), assets.Options{})
	accountStore := accounts.NewStore(sqlDB, accounts.StoreOptions{
		KeyVersion: "kek-v1",
		LoadKEK:    func() ([]byte, error) { return config.LoadMasterKey(kekPath) },
	})
	failing := &failAuditor{failOn: audit.ActionCredentialReveal, inner: auditStore}
	accountH := accounts.NewHandler(accountStore, failing, accounts.Options{Limiter: limiter})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", ah.Login)
	mux.HandleFunc("/api/v1/auth/step-up", ah.StepUp)
	mux.HandleFunc("/api/v1/setup/bootstrap", ah.Bootstrap)
	assetH.Register(mux, ah.RequireOrigin)
	accountH.Register(mux, ah.RequireOrigin)
	srv := ah.Middleware(mux)

	tok := bootstrap(t, srv)
	assetID := createAsset(t, srv, tok, "fail-audit.example")
	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+assetID+"/accounts", tok,
		`{"username":"u","auth_type":"password","secret":"topsecret"}`)
	var acc map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&acc)
	id, _ := acc["id"].(string)
	stepUp(t, srv, tok)

	rec = doJSON(t, srv, http.MethodPost, "/api/v1/accounts/"+id+"/reveal", tok, "")
	if rec.Code != http.StatusInternalServerError || errorCode(strings.NewReader(rec.Body.String())) != "internal_error" {
		t.Fatalf("reveal with audit fail: %d %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.NewDecoder(strings.NewReader(rec.Body.String())).Decode(&body)
	if _, ok := body["secret"]; ok {
		t.Fatalf("secret must not be present on audit failure: %#v", body)
	}
}
