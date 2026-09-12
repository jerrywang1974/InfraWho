package jobs_test

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jerrywang1974/InfraWho/internal/assets"
	"github.com/jerrywang1974/InfraWho/internal/auth"
	"github.com/jerrywang1974/InfraWho/internal/db"
	"github.com/jerrywang1974/InfraWho/internal/jobs"
	"github.com/jerrywang1974/InfraWho/internal/ratelimit"
)

func testServer(t *testing.T) (http.Handler, *auth.Store, *sql.DB) {
	t.Helper()
	sqlDB, err := db.Open("sqlite::memory:")
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	store := auth.NewStore(sqlDB)
	ah := auth.NewHandler(store, ratelimit.New(), auth.Options{
		CookieSecure:   false,
		MasterKeyReady: func() bool { return true },
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/setup/bootstrap", ah.Bootstrap)
	mux.HandleFunc("/api/v1/auth/login", ah.Login)
	mux.HandleFunc("/api/v1/auth/step-up", ah.StepUp)
	assets.NewHandler(assets.NewStore(sqlDB), assets.Options{}).Register(mux, ah.RequireOrigin)
	jobs.NewHandler(jobs.NewStore(sqlDB)).Register(mux, ah.RequireOrigin)
	return ah.Middleware(mux), store, sqlDB
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
	body := `{"name":"Host","hostname":"` + hostname + `","asset_type":"vm","os_family":"linux","environment":"lab"}`
	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets", tok, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create asset: %d %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&created)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("missing asset id")
	}
	return id
}

func TestJobCRUDAndPagination(t *testing.T) {
	srv, _, _ := testServer(t)
	tok := bootstrap(t, srv)
	assetID := createAsset(t, srv, tok, "jobs-1.example")

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+assetID+"/jobs", tok,
		`{"name":"nightly-backup","scheduler_type":"cron","schedule_expr":"0 2 * * *","command_or_path":"/usr/local/bin/backup.sh","description":"DB dump","enabled_doc":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	jobID, _ := created["id"].(string)
	if jobID == "" || created["name"] != "nightly-backup" || created["scheduler_type"] != "cron" {
		t.Fatalf("created: %#v", created)
	}
	if created["enabled_doc"] != true {
		t.Fatalf("enabled_doc: %#v", created["enabled_doc"])
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+assetID+"/jobs", tok,
		`{"name":"weekly","scheduler_type":"systemd_timer","schedule_expr":"OnCalendar=Sun","command_or_path":"cleanup.service"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create2: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/v1/assets/"+assetID+"/jobs?limit=1&offset=0", tok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var list map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&list)
	if int(list["total"].(float64)) != 2 || int(list["limit"].(float64)) != 1 {
		t.Fatalf("list: %#v", list)
	}
	items, _ := list["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items: %#v", items)
	}

	rec = doJSON(t, srv, http.MethodPatch, "/api/v1/jobs/"+jobID, tok,
		`{"enabled_doc":false,"description":"updated"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	var patched map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&patched)
	if patched["enabled_doc"] != false || patched["description"] != "updated" {
		t.Fatalf("patched: %#v", patched)
	}

	rec = doJSON(t, srv, http.MethodDelete, "/api/v1/jobs/"+jobID, tok, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, http.MethodGet, "/api/v1/assets/"+assetID+"/jobs", tok, "")
	_ = json.NewDecoder(rec.Body).Decode(&list)
	if int(list["total"].(float64)) != 1 {
		t.Fatalf("after delete total: %#v", list["total"])
	}
}

func TestJobEmptyPatchAndValidation(t *testing.T) {
	srv, _, _ := testServer(t)
	tok := bootstrap(t, srv)
	assetID := createAsset(t, srv, tok, "jobs-val.example")

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+assetID+"/jobs", tok,
		`{"name":"","scheduler_type":"cron"}`)
	if rec.Code != http.StatusUnprocessableEntity || errorCode(rec.Body) != "validation_error" {
		t.Fatalf("empty name: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+assetID+"/jobs", tok,
		`{"name":"x","scheduler_type":"bogus"}`)
	if rec.Code != http.StatusUnprocessableEntity || errorCode(rec.Body) != "validation_error" {
		t.Fatalf("bad type: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+assetID+"/jobs", tok,
		`{"name":"ok","scheduler_type":"cron"}`)
	var created map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&created)
	jobID, _ := created["id"].(string)

	rec = doJSON(t, srv, http.MethodPatch, "/api/v1/jobs/"+jobID, tok, `{}`)
	if rec.Code != http.StatusUnprocessableEntity || errorCode(rec.Body) != "validation_error" {
		t.Fatalf("empty patch: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, srv, http.MethodPatch, "/api/v1/jobs/"+jobID, tok, `{"unknown":1}`)
	if rec.Code != http.StatusUnprocessableEntity || errorCode(rec.Body) != "validation_error" {
		t.Fatalf("unknown field: %d %s", rec.Code, rec.Body.String())
	}
}

func TestJobSoftDeletedAssetRejectsWrites(t *testing.T) {
	srv, _, _ := testServer(t)
	tok := bootstrap(t, srv)
	assetID := createAsset(t, srv, tok, "jobs-retired.example")

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+assetID+"/jobs", tok,
		`{"name":"keep","scheduler_type":"cron"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create before soft-delete: %d %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&created)
	jobID, _ := created["id"].(string)

	stepUp(t, srv, tok)
	rec = doJSON(t, srv, http.MethodDelete, "/api/v1/assets/"+assetID, tok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("soft-delete: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+assetID+"/jobs", tok,
		`{"name":"should-fail","scheduler_type":"cron"}`)
	if rec.Code != http.StatusConflict || errorCode(rec.Body) != "conflict" {
		t.Fatalf("create on deleted: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, http.MethodPatch, "/api/v1/jobs/"+jobID, tok, `{"description":"nope"}`)
	if rec.Code != http.StatusConflict || errorCode(rec.Body) != "conflict" {
		t.Fatalf("patch on deleted: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, http.MethodDelete, "/api/v1/jobs/"+jobID, tok, "")
	if rec.Code != http.StatusConflict || errorCode(rec.Body) != "conflict" {
		t.Fatalf("delete on deleted: %d %s", rec.Code, rec.Body.String())
	}
}

func TestJobMissingAsset(t *testing.T) {
	srv, _, _ := testServer(t)
	tok := bootstrap(t, srv)
	missing := "01a0932e-0000-7000-8000-ffffffffffff"
	rec := doJSON(t, srv, http.MethodGet, "/api/v1/assets/"+missing+"/jobs", tok, "")
	if rec.Code != http.StatusNotFound || errorCode(rec.Body) != "not_found" {
		t.Fatalf("list missing: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+missing+"/jobs", tok,
		`{"name":"x","scheduler_type":"other"}`)
	if rec.Code != http.StatusNotFound || errorCode(rec.Body) != "not_found" {
		t.Fatalf("create missing: %d %s", rec.Code, rec.Body.String())
	}
}

func TestJobAuthzAndOrigin(t *testing.T) {
	srv, store, _ := testServer(t)
	adminTok := bootstrap(t, srv)
	assetID := createAsset(t, srv, adminTok, "jobs-authz.example")
	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+assetID+"/jobs", adminTok,
		`{"name":"owned","scheduler_type":"cron"}`)
	var created map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&created)
	jobID, _ := created["id"].(string)

	hash, err := auth.HashPassword("password1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateUser("viewer1", "Viewer", auth.RoleViewer, hash); err != nil {
		t.Fatal(err)
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/auth/login", "",
		`{"username":"viewer1","password":"password1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer login: %d %s", rec.Code, rec.Body.String())
	}
	viewerTok := cookieValue(rec, auth.SessionCookieName)

	rec = doJSON(t, srv, http.MethodGet, "/api/v1/assets/"+assetID+"/jobs", viewerTok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer list: %d %s", rec.Code, rec.Body.String())
	}
	for _, tc := range []struct {
		method, path, body string
	}{
		{http.MethodPost, "/api/v1/assets/" + assetID + "/jobs", `{"name":"x","scheduler_type":"other"}`},
		{http.MethodPatch, "/api/v1/jobs/" + jobID, `{"description":"x"}`},
		{http.MethodDelete, "/api/v1/jobs/" + jobID, ""},
	} {
		rec = doJSON(t, srv, tc.method, tc.path, viewerTok, tc.body)
		if rec.Code != http.StatusForbidden || errorCode(rec.Body) != "forbidden" {
			t.Fatalf("viewer %s: %d %s", tc.method, rec.Code, rec.Body.String())
		}
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/assets/"+assetID+"/jobs",
		strings.NewReader(`{"name":"no-origin","scheduler_type":"cron"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "example.test"
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: adminTok})
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || errorCode(rec.Body) != "forbidden" {
		t.Fatalf("create without origin: %d %s", rec.Code, rec.Body.String())
	}
}

func TestJobRequiresAuth(t *testing.T) {
	srv, _, _ := testServer(t)
	rec := doJSON(t, srv, http.MethodGet, "/api/v1/assets/x/jobs", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth list: %d", rec.Code)
	}
}

func TestJobListPaginationDefaults(t *testing.T) {
	srv, _, _ := testServer(t)
	tok := bootstrap(t, srv)
	assetID := createAsset(t, srv, tok, "jobs-page.example")

	rec := doJSON(t, srv, http.MethodGet, "/api/v1/assets/"+assetID+"/jobs", tok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var list map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&list)
	if int(list["limit"].(float64)) != 50 {
		t.Fatalf("default limit: %#v", list["limit"])
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/v1/assets/"+assetID+"/jobs?limit=999", tok, "")
	_ = json.NewDecoder(rec.Body).Decode(&list)
	if int(list["limit"].(float64)) != 200 {
		t.Fatalf("max clamp: %#v", list["limit"])
	}
}
