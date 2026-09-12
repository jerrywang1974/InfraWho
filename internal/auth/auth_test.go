package auth_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jerrywang1974/InfraWho/internal/auth"
	"github.com/jerrywang1974/InfraWho/internal/db"
	"github.com/jerrywang1974/InfraWho/internal/ratelimit"
)

func testHandler(t *testing.T, keyReady bool) (*auth.Handler, http.Handler) {
	t.Helper()
	sqlDB, err := db.Open("sqlite::memory:")
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	store := auth.NewStore(sqlDB)
	h := auth.NewHandler(store, ratelimit.New(), auth.Options{
		CookieSecure: false,
		MasterKeyReady: func() bool {
			return keyReady
		},
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", h.Login)
	mux.HandleFunc("/api/v1/auth/logout", h.Logout)
	mux.HandleFunc("/api/v1/auth/me", h.Me)
	mux.HandleFunc("/api/v1/auth/step-up", h.StepUp)
	mux.HandleFunc("/api/v1/setup/status", h.SetupStatus)
	mux.HandleFunc("/api/v1/setup/bootstrap", h.Bootstrap)
	mux.HandleFunc("/api/v1/setup/acknowledge", h.Acknowledge)
	return h, h.Middleware(mux)
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

func TestBootstrapLoginMeStepUpLogout(t *testing.T) {
	_, srv := testHandler(t, true)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil)
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status code %d", rec.Code)
	}
	var st map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if st["needs_bootstrap"] != true || st["show_banner"] != true || st["master_key_ready"] != true {
		t.Fatalf("unexpected status: %#v", st)
	}

	body := `{"username":"Admin","password":"password1","acknowledge_kek_offline":true,"acknowledge_kek_irrecoverable":true,"acknowledge_backup_planned":false}`
	rec = httptest.NewRecorder()
	req = withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/setup/bootstrap", strings.NewReader(body)))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bootstrap without ack: %d %s", rec.Code, rec.Body.String())
	}

	body = `{"username":"Admin","password":"password1","display_name":"Root","acknowledge_kek_offline":true,"acknowledge_kek_irrecoverable":true,"acknowledge_backup_planned":true}`
	rec = httptest.NewRecorder()
	req = withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/setup/bootstrap", strings.NewReader(body)))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap: %d %s", rec.Code, rec.Body.String())
	}
	tok := cookieValue(rec, auth.SessionCookieName)
	if tok == "" {
		t.Fatal("missing session cookie after bootstrap")
	}

	rec = httptest.NewRecorder()
	req = withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/setup/bootstrap", strings.NewReader(body)))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second bootstrap: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil)
	srv.ServeHTTP(rec, req)
	_ = json.NewDecoder(rec.Body).Decode(&st)
	if st["needs_bootstrap"] != false || st["checklist_complete"] != true || st["show_banner"] != false {
		t.Fatalf("post-bootstrap status: %#v", st)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: tok})
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("me: %d %s", rec.Code, rec.Body.String())
	}
	var me map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&me)
	if me["username"] != "Admin" || me["role"] != "admin" || me["step_up_active"] != false {
		t.Fatalf("me body: %#v", me)
	}

	rec = httptest.NewRecorder()
	req = withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/auth/step-up", strings.NewReader(`{"password":"nope"}`)))
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: tok})
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("step-up bad pw: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/auth/step-up", strings.NewReader(`{"password":"password1"}`)))
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: tok})
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("step-up: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: tok})
	srv.ServeHTTP(rec, req)
	_ = json.NewDecoder(rec.Body).Decode(&me)
	if me["step_up_active"] != true {
		t.Fatalf("expected step_up_active: %#v", me)
	}

	rec = httptest.NewRecorder()
	req = withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil))
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: tok})
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: tok})
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"password1"}`)))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	if cookieValue(rec, auth.SessionCookieName) == "" {
		t.Fatal("login missing cookie")
	}
}

func TestBootstrapRequiresMasterKey(t *testing.T) {
	_, srv := testHandler(t, false)
	body := `{"username":"admin","password":"password1","acknowledge_kek_offline":true,"acknowledge_kek_irrecoverable":true,"acknowledge_backup_planned":true}`
	rec := httptest.NewRecorder()
	req := withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/setup/bootstrap", strings.NewReader(body)))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLoginOriginRejected(t *testing.T) {
	_, srv := testHandler(t, true)
	body := `{"username":"admin","password":"password1","acknowledge_kek_offline":true,"acknowledge_kek_irrecoverable":true,"acknowledge_backup_planned":true}`
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/setup/bootstrap", strings.NewReader(body))))
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"password1"}`))
	req.Host = "example.test"
	req.Header.Set("Origin", "http://evil.test")
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden, got %d", rec.Code)
	}
}

func TestRequireStepUpMiddleware(t *testing.T) {
	h, srv := testHandler(t, true)
	body := `{"username":"admin","password":"password1","acknowledge_kek_offline":true,"acknowledge_kek_irrecoverable":true,"acknowledge_backup_planned":true}`
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/setup/bootstrap", strings.NewReader(body))))
	tok := cookieValue(rec, auth.SessionCookieName)

	mux := http.NewServeMux()
	mux.Handle("/api/v1/protected", auth.RequireStepUp(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	})))
	wrapped := h.Middleware(mux)

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/protected", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: tok})
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("without step-up: %d", rec.Code)
	}
	var errBody map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&errBody)
	if errObj, _ := errBody["error"].(map[string]any); errObj["code"] != "step_up_required" {
		t.Fatalf("error body: %#v", errBody)
	}

	rec = httptest.NewRecorder()
	req = withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/auth/step-up", strings.NewReader(`{"password":"password1"}`)))
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: tok})
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("step-up: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/protected", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: tok})
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("with step-up: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSessionIdleExpiry(t *testing.T) {
	sqlDB, err := db.Open("sqlite::memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := auth.NewStore(sqlDB)
	hash, err := auth.HashPassword("password1")
	if err != nil {
		t.Fatal(err)
	}
	u, err := store.CreateUser("u1", "U", auth.RoleViewer, hash)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	raw, sess, err := store.CreateSession(u.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if !sess.Valid(now.Add(time.Minute)) {
		t.Fatal("should be valid")
	}
	if err := store.TouchSession(sess.ID, now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	got, err := store.FindSessionByToken(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Valid(now) {
		t.Fatal("idle-expired session should be invalid")
	}
}

func TestRBACHelpers(t *testing.T) {
	if !auth.RoleAdmin.HasAtLeast(auth.RoleOperator) {
		t.Fatal("admin >= operator")
	}
	if auth.RoleViewer.CanReveal() {
		t.Fatal("viewer cannot reveal")
	}
	if !auth.RoleOperator.CanReveal() || !auth.RoleAdmin.CanReveal() {
		t.Fatal("operator/admin can reveal")
	}
	if auth.RoleOperator.CanSoftDeleteOrPurge() {
		t.Fatal("operator cannot purge")
	}
}
