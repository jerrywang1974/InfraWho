package auth_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jerrywang1974/InfraWho/internal/auth"
	"github.com/jerrywang1974/InfraWho/internal/db"
	"github.com/jerrywang1974/InfraWho/internal/ratelimit"
)

func testHandler(t *testing.T, keyReady bool, opts auth.Options) (*auth.Handler, http.Handler) {
	t.Helper()
	sqlDB, err := db.Open("sqlite::memory:")
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	opts.MasterKeyReady = func() bool { return keyReady }
	store := auth.NewStore(sqlDB)
	h := auth.NewHandler(store, ratelimit.New(), opts)
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

func bootstrapBody(user string) string {
	return `{"username":"` + user + `","password":"password1","acknowledge_kek_offline":true,"acknowledge_kek_irrecoverable":true,"acknowledge_backup_planned":true}`
}

func TestBootstrapLoginMeStepUpLogout(t *testing.T) {
	_, srv := testHandler(t, true, auth.Options{CookieSecure: false})

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
	assertSessionCookie(t, rec, false)

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

func assertSessionCookie(t *testing.T, rec *httptest.ResponseRecorder, wantSecure bool) {
	t.Helper()
	var c *http.Cookie
	for _, got := range rec.Result().Cookies() {
		if got.Name == auth.SessionCookieName {
			c = got
			break
		}
	}
	if c == nil {
		t.Fatal("missing session cookie")
	}
	if !c.HttpOnly {
		t.Fatal("cookie HttpOnly want true")
	}
	if c.Path != "/" {
		t.Fatalf("cookie Path=%q", c.Path)
	}
	if c.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie SameSite=%v", c.SameSite)
	}
	if c.Secure != wantSecure {
		t.Fatalf("cookie Secure=%v want %v", c.Secure, wantSecure)
	}
}

func TestSecureCookieFlag(t *testing.T) {
	_, srv := testHandler(t, true, auth.Options{CookieSecure: true})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/setup/bootstrap", strings.NewReader(bootstrapBody("admin")))))
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap: %d %s", rec.Code, rec.Body.String())
	}
	assertSessionCookie(t, rec, true)
}

func TestBootstrapRequiresMasterKey(t *testing.T) {
	_, srv := testHandler(t, false, auth.Options{CookieSecure: false})
	rec := httptest.NewRecorder()
	req := withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/setup/bootstrap", strings.NewReader(bootstrapBody("admin"))))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLoginOriginRejected(t *testing.T) {
	_, srv := testHandler(t, true, auth.Options{CookieSecure: false})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/setup/bootstrap", strings.NewReader(bootstrapBody("admin")))))
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

func TestLoginRejectsXFHSpoof(t *testing.T) {
	_, srv := testHandler(t, true, auth.Options{CookieSecure: false, TrustProxy: false})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/bootstrap", strings.NewReader(bootstrapBody("admin")))
	req.Host = "victim.test"
	req.Header.Set("Origin", "http://evil.test")
	req.Header.Set("X-Forwarded-Host", "evil.test")
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("XFH spoof should fail origin check, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestRequireStepUpMiddleware(t *testing.T) {
	h, srv := testHandler(t, true, auth.Options{CookieSecure: false})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/setup/bootstrap", strings.NewReader(bootstrapBody("admin")))))
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
	if errorCode(rec.Body) != "step_up_required" {
		t.Fatalf("want step_up_required")
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

func TestSessionTTLsAndStepUpExpiry(t *testing.T) {
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
	idleDelta := sess.IdleExpiresAt.Sub(now)
	absDelta := sess.AbsoluteExpiresAt.Sub(now)
	if idleDelta < auth.IdleTTL-time.Second || idleDelta > auth.IdleTTL+time.Second {
		t.Fatalf("idle TTL = %v, want ~%v", idleDelta, auth.IdleTTL)
	}
	if absDelta < auth.AbsoluteTTL-time.Second || absDelta > auth.AbsoluteTTL+time.Second {
		t.Fatalf("absolute TTL = %v, want ~%v", absDelta, auth.AbsoluteTTL)
	}

	stepExpires := now.Add(auth.StepUpTTL)
	if err := store.SetStepUp(sess.ID, stepExpires); err != nil {
		t.Fatal(err)
	}
	got, err := store.FindSessionByToken(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !got.StepUpActive(now.Add(auth.StepUpTTL - time.Second)) {
		t.Fatal("step-up should still be active before 5m")
	}
	if got.StepUpActive(now.Add(auth.StepUpTTL + time.Second)) {
		t.Fatal("step-up should expire after 5m")
	}

	if err := store.TouchSession(sess.ID, now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	got, err = store.FindSessionByToken(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Valid(now) {
		t.Fatal("idle-expired session should be invalid")
	}
}

func TestLoginRateLimitAndLockoutHTTP(t *testing.T) {
	_, srv := testHandler(t, true, auth.Options{
		CookieSecure:     false,
		LoginIPLimit:     100, // stay above per-user attempts in this test
		LoginUserLimit:   3,
		LockoutThreshold: 3,
	})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/setup/bootstrap", strings.NewReader(bootstrapBody("admin")))))
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap: %d", rec.Code)
	}

	// Non-existent user avoids Argon2; still counts toward username limit + lockout.
	for i := 0; i < 3; i++ {
		rec = httptest.NewRecorder()
		req := withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"nobody","password":"x"}`)))
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("attempt %d: %d %s", i+1, rec.Code, rec.Body.String())
		}
	}
	// 4th attempt in window: either user rate_limited or lockout (threshold 3).
	rec = httptest.NewRecorder()
	req := withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"nobody","password":"x"}`)))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d %s", rec.Code, rec.Body.String())
	}
	code := errorCode(rec.Body)
	if code != "rate_limited" && code != "lockout" {
		t.Fatalf("code=%q", code)
	}
}

func TestLoginIPRateLimitHTTP(t *testing.T) {
	_, srv := testHandler(t, true, auth.Options{
		CookieSecure:   false,
		LoginIPLimit:   3,
		LoginUserLimit: 100,
	})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/setup/bootstrap", strings.NewReader(bootstrapBody("admin")))))
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap: %d", rec.Code)
	}
	for i := 0; i < 3; i++ {
		rec = httptest.NewRecorder()
		body := `{"username":"missing` + string(rune('a'+i)) + `","password":"x"}`
		req := withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body)))
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i+1, rec.Code)
		}
	}
	rec = httptest.NewRecorder()
	req := withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"other","password":"x"}`)))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests || errorCode(rec.Body) != "rate_limited" {
		t.Fatalf("want rate_limited, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestAcknowledgeClearsBanner(t *testing.T) {
	sqlDB, err := db.Open("sqlite::memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	store := auth.NewStore(sqlDB)
	hash, err := auth.HashPassword("password1")
	if err != nil {
		t.Fatal(err)
	}
	u, err := store.CreateUser("admin", "Admin", auth.RoleAdmin, hash)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	raw, _, err := store.CreateSession(u.ID, now)
	if err != nil {
		t.Fatal(err)
	}

	h := auth.NewHandler(store, ratelimit.New(), auth.Options{
		CookieSecure:   false,
		MasterKeyReady: func() bool { return true },
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/setup/status", h.SetupStatus)
	mux.HandleFunc("/api/v1/setup/acknowledge", h.Acknowledge)
	srv := h.Middleware(mux)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil))
	var st map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&st)
	if st["show_banner"] != true || st["checklist_complete"] != false {
		t.Fatalf("pre-ack status: %#v", st)
	}

	ack := `{"acknowledge_kek_offline":true,"acknowledge_kek_irrecoverable":true,"acknowledge_backup_planned":true}`
	rec = httptest.NewRecorder()
	req := withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/setup/acknowledge", strings.NewReader(ack)))
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: raw})
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("acknowledge: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil))
	_ = json.NewDecoder(rec.Body).Decode(&st)
	if st["show_banner"] != false || st["checklist_complete"] != true {
		t.Fatalf("post-ack status: %#v", st)
	}
}

func TestBootstrapConcurrentSingleAdmin(t *testing.T) {
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

	var wg sync.WaitGroup
	var okCount atomic.Int32
	var conflictCount atomic.Int32
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := store.BootstrapAdmin("admin"+string(rune('a'+i)), "A", hash, time.Now().UTC())
			if err == nil {
				okCount.Add(1)
				return
			}
			if errors.Is(err, auth.ErrAlreadyBootstrapped) {
				conflictCount.Add(1)
				return
			}
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("bootstrap race: %v", err)
	}
	if okCount.Load() != 1 {
		t.Fatalf("ok=%d want 1; conflicts=%d", okCount.Load(), conflictCount.Load())
	}
	n, err := store.UserCount()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("user count=%d want 1", n)
	}
}

func TestContextUserOmitsPasswordHash(t *testing.T) {
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
	u, err := store.CreateUser("admin", "A", auth.RoleAdmin, hash)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.FindUserByID(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PasswordHash != "" {
		t.Fatal("FindUserByID must not load password_hash")
	}
	loaded, err := store.UserPasswordHash(u.ID)
	if err != nil || loaded == "" {
		t.Fatalf("UserPasswordHash: %v %q", err, loaded)
	}
}

func TestClientIPIgnoresXFFWithoutTrustProxy(t *testing.T) {
	_, srv := testHandler(t, true, auth.Options{
		CookieSecure:   false,
		TrustProxy:     false,
		LoginIPLimit:   2,
		LoginUserLimit: 100,
	})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/setup/bootstrap", strings.NewReader(bootstrapBody("admin")))))
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap: %d", rec.Code)
	}
	for i := 0; i < 2; i++ {
		rec = httptest.NewRecorder()
		req := withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"u`+string(rune('a'+i))+`","password":"x"}`)))
		req.Header.Set("X-Forwarded-For", "1.2.3."+string(rune('1'+i)))
		req.RemoteAddr = "10.0.0.1:1234"
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i+1, rec.Code)
		}
	}
	rec = httptest.NewRecorder()
	req := withOrigin(httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"uz","password":"x"}`)))
	req.Header.Set("X-Forwarded-For", "9.9.9.9")
	req.RemoteAddr = "10.0.0.1:1234"
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests || errorCode(rec.Body) != "rate_limited" {
		t.Fatalf("XFF rotation must not bypass IP limit when TrustProxy=false; got %d %s", rec.Code, rec.Body.String())
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
