package assets_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jerrywang1974/InfraWho/internal/assets"
	"github.com/jerrywang1974/InfraWho/internal/auth"
	"github.com/jerrywang1974/InfraWho/internal/db"
	"github.com/jerrywang1974/InfraWho/internal/ratelimit"
)

func testServer(t *testing.T) (*auth.Handler, http.Handler, *assets.Store) {
	t.Helper()
	sqlDB, err := db.Open("sqlite::memory:")
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	authStore := auth.NewStore(sqlDB)
	ah := auth.NewHandler(authStore, ratelimit.New(), auth.Options{
		CookieSecure:   false,
		MasterKeyReady: func() bool { return true },
	})
	assetStore := assets.NewStore(sqlDB)
	assetH := assets.NewHandler(assetStore, assets.Options{})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", ah.Login)
	mux.HandleFunc("/api/v1/auth/logout", ah.Logout)
	mux.HandleFunc("/api/v1/auth/me", ah.Me)
	mux.HandleFunc("/api/v1/auth/step-up", ah.StepUp)
	mux.HandleFunc("/api/v1/setup/status", ah.SetupStatus)
	mux.HandleFunc("/api/v1/setup/bootstrap", ah.Bootstrap)
	mux.HandleFunc("/api/v1/setup/acknowledge", ah.Acknowledge)
	assetH.Register(mux, ah.RequireOrigin)
	return ah, ah.Middleware(mux), assetStore
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

func sampleCreate(hostname string) string {
	return `{"name":"App DB","hostname":"` + hostname + `","asset_type":"vm","os_family":"linux","environment":"prod","purpose":"primary postgres","tags":["db","postgres"]}`
}

func TestAssetCRUDAndPagination(t *testing.T) {
	_, srv, _ := testServer(t)
	tok := bootstrap(t, srv)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets", tok, sampleCreate("db-1.example"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	id, _ := created["id"].(string)
	if id == "" || created["purpose"] != "primary postgres" {
		t.Fatalf("created: %#v", created)
	}
	tags, _ := created["tags"].([]any)
	if len(tags) != 2 {
		t.Fatalf("tags: %#v", tags)
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/v1/assets", tok, sampleCreate("db-2.example"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create2: %d", rec.Code)
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/assets", tok,
		`{"name":"Web","hostname":"web-1.example","asset_type":"vm","os_family":"linux","environment":"prod","purpose":"nginx","tags":["web"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create3: %d", rec.Code)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/v1/assets?limit=2&offset=0", tok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var list map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&list)
	if int(list["total"].(float64)) != 3 || int(list["limit"].(float64)) != 2 {
		t.Fatalf("list meta: %#v", list)
	}
	items, _ := list["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items len %d", len(items))
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/v1/assets?limit=999", tok, "")
	_ = json.NewDecoder(rec.Body).Decode(&list)
	if int(list["limit"].(float64)) != 200 {
		t.Fatalf("limit clamp: %#v", list["limit"])
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/v1/assets?tag=db", tok, "")
	_ = json.NewDecoder(rec.Body).Decode(&list)
	if int(list["total"].(float64)) != 2 {
		t.Fatalf("tag filter: %#v", list)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/v1/assets/"+id, tok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}
	var detail map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&detail)
	if _, ok := detail["accounts"]; !ok {
		t.Fatalf("detail missing accounts: %#v", detail)
	}

	rec = doJSON(t, srv, http.MethodPatch, "/api/v1/assets/"+id, tok, `{"purpose":"updated purpose","tags":["db","primary"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	_ = json.NewDecoder(rec.Body).Decode(&created)
	if created["purpose"] != "updated purpose" {
		t.Fatalf("patch body: %#v", created)
	}
}

func TestHostnameConflictCaseInsensitive(t *testing.T) {
	_, srv, _ := testServer(t)
	tok := bootstrap(t, srv)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets", tok, sampleCreate("Host-A.example"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/assets", tok, sampleCreate("host-a.example"))
	if rec.Code != http.StatusConflict || errorCode(rec.Body) != "conflict" {
		t.Fatalf("expected conflict, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestPatchRejectsDeletedAtAndRetired(t *testing.T) {
	_, srv, _ := testServer(t)
	tok := bootstrap(t, srv)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets", tok, sampleCreate("patch-reject.example"))
	var created map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&created)
	id, _ := created["id"].(string)

	rec = doJSON(t, srv, http.MethodPatch, "/api/v1/assets/"+id, tok, `{"deleted_at":"2026-01-01T00:00:00.000Z"}`)
	if rec.Code != http.StatusUnprocessableEntity || errorCode(rec.Body) != "validation_error" {
		t.Fatalf("deleted_at: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, srv, http.MethodPatch, "/api/v1/assets/"+id, tok, `{"deleted_at":null}`)
	if rec.Code != http.StatusUnprocessableEntity || errorCode(rec.Body) != "validation_error" {
		t.Fatalf("deleted_at null: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, srv, http.MethodPatch, "/api/v1/assets/"+id, tok, `{"status":"retired"}`)
	if rec.Code != http.StatusUnprocessableEntity || errorCode(rec.Body) != "validation_error" {
		t.Fatalf("status retired: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, srv, http.MethodPatch, "/api/v1/assets/"+id, tok, `{"status":"unknown"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status unknown: %d %s", rec.Code, rec.Body.String())
	}
}

func TestCreateRejectsRetiredStatus(t *testing.T) {
	_, srv, _ := testServer(t)
	tok := bootstrap(t, srv)
	body := `{"name":"X","hostname":"retired-create.example","asset_type":"other","os_family":"other","environment":"lab","status":"retired"}`
	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets", tok, body)
	if rec.Code != http.StatusUnprocessableEntity || errorCode(rec.Body) != "validation_error" {
		t.Fatalf("create retired: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSoftDeleteAndPurgeRequireAdminStepUp(t *testing.T) {
	_, srv, store := testServer(t)
	tok := bootstrap(t, srv)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets", tok, sampleCreate("del-1.example"))
	var created map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&created)
	id, _ := created["id"].(string)

	// Without step-up
	rec = doJSON(t, srv, http.MethodDelete, "/api/v1/assets/"+id, tok, "")
	if rec.Code != http.StatusForbidden || errorCode(rec.Body) != "step_up_required" {
		t.Fatalf("delete without step-up: %d %s", rec.Code, rec.Body.String())
	}

	stepUp(t, srv, tok)
	rec = doJSON(t, srv, http.MethodDelete, "/api/v1/assets/"+id, tok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("soft-delete: %d %s", rec.Code, rec.Body.String())
	}
	_ = json.NewDecoder(rec.Body).Decode(&created)
	if created["status"] != "retired" || created["deleted_at"] == nil {
		t.Fatalf("soft-delete body: %#v", created)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/v1/assets", tok, "")
	var list map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&list)
	if int(list["total"].(float64)) != 0 {
		t.Fatalf("default list should hide soft-deleted: %#v", list)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/v1/assets?include_deleted=true", tok, "")
	_ = json.NewDecoder(rec.Body).Decode(&list)
	if int(list["total"].(float64)) != 1 {
		t.Fatalf("include_deleted: %#v", list)
	}

	rec = doJSON(t, srv, http.MethodDelete, "/api/v1/assets/"+id, tok, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("second soft-delete: %d %s", rec.Code, rec.Body.String())
	}

	// Soft-deleted hostname can be reused
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/assets", tok, sampleCreate("del-1.example"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("reuse hostname after soft-delete: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+id+"/purge", tok, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("purge: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := store.Get(id); err != assets.ErrNotFound {
		t.Fatalf("purged get: %v", err)
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+id+"/purge", tok, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("purge missing: %d", rec.Code)
	}
}

func TestOperatorRBACViaDirectInsert(t *testing.T) {
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
	assetH := assets.NewHandler(assets.NewStore(sqlDB), assets.Options{})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", ah.Login)
	mux.HandleFunc("/api/v1/auth/step-up", ah.StepUp)
	mux.HandleFunc("/api/v1/setup/bootstrap", ah.Bootstrap)
	assetH.Register(mux, ah.RequireOrigin)
	srv := ah.Middleware(mux)

	adminTok := bootstrap(t, srv)
	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets", adminTok, sampleCreate("rbac.example"))
	var created map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&created)
	id, _ := created["id"].(string)

	hash, err := auth.HashPassword("password1")
	if err != nil {
		t.Fatal(err)
	}
	op, err := authStore.CreateUser("operator1", "Op", auth.RoleOperator, hash)
	if err != nil {
		t.Fatal(err)
	}
	_ = op
	viewerHash, err := auth.HashPassword("password1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authStore.CreateUser("viewer1", "View", auth.RoleViewer, viewerHash); err != nil {
		t.Fatal(err)
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/v1/auth/login", "", `{"username":"operator1","password":"password1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("op login: %d %s", rec.Code, rec.Body.String())
	}
	opTok := cookieValue(rec, auth.SessionCookieName)

	rec = doJSON(t, srv, http.MethodPost, "/api/v1/assets", opTok, sampleCreate("op-create.example"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("operator create: %d %s", rec.Code, rec.Body.String())
	}

	stepUp(t, srv, opTok)
	rec = doJSON(t, srv, http.MethodDelete, "/api/v1/assets/"+id, opTok, "")
	if rec.Code != http.StatusForbidden || errorCode(rec.Body) != "forbidden" {
		t.Fatalf("operator delete: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+id+"/purge", opTok, "")
	if rec.Code != http.StatusForbidden || errorCode(rec.Body) != "forbidden" {
		t.Fatalf("operator purge: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/v1/auth/login", "", `{"username":"viewer1","password":"password1"}`)
	viewerTok := cookieValue(rec, auth.SessionCookieName)
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/assets", viewerTok, sampleCreate("viewer.example"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer create: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, http.MethodGet, "/api/v1/assets", viewerTok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer list: %d", rec.Code)
	}
}

func TestUnauthenticatedAndOrigin(t *testing.T) {
	_, srv, _ := testServer(t)
	tok := bootstrap(t, srv)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/assets", nil)
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth list: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/assets", strings.NewReader(sampleCreate("no-origin.example")))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "example.test"
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: tok})
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || errorCode(rec.Body) != "forbidden" {
		t.Fatalf("missing origin: %d %s", rec.Code, rec.Body.String())
	}
}

func TestPatchSoftDeletedConflict(t *testing.T) {
	_, srv, _ := testServer(t)
	tok := bootstrap(t, srv)
	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets", tok, sampleCreate("soft-patch.example"))
	var created map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&created)
	id, _ := created["id"].(string)

	stepUp(t, srv, tok)
	rec = doJSON(t, srv, http.MethodDelete, "/api/v1/assets/"+id, tok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("soft-delete: %d", rec.Code)
	}
	rec = doJSON(t, srv, http.MethodPatch, "/api/v1/assets/"+id, tok, `{"purpose":"nope"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("patch deleted: %d %s", rec.Code, rec.Body.String())
	}
}

func TestValidationLimits(t *testing.T) {
	_, srv, _ := testServer(t)
	tok := bootstrap(t, srv)

	longPurpose := strings.Repeat("p", 8*1024+1)
	body := `{"name":"X","hostname":"lim.example","asset_type":"vm","os_family":"linux","environment":"dev","purpose":"` + longPurpose + `"}`
	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets", tok, body)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("purpose limit: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/v1/assets", tok, `{"name":"","hostname":"x","asset_type":"vm","os_family":"linux","environment":"dev"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty name: %d", rec.Code)
	}
}

func TestGetNotFound(t *testing.T) {
	_, srv, _ := testServer(t)
	tok := bootstrap(t, srv)
	rec := doJSON(t, srv, http.MethodGet, "/api/v1/assets/018f0000-0000-7000-8000-000000000000", tok, "")
	if rec.Code != http.StatusNotFound || errorCode(rec.Body) != "not_found" {
		t.Fatalf("get missing: %d %s", rec.Code, rec.Body.String())
	}
}

func TestPatchRejectsEmptyAndUnknown(t *testing.T) {
	_, srv, _ := testServer(t)
	tok := bootstrap(t, srv)
	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets", tok, sampleCreate("patch-empty.example"))
	var created map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&created)
	id, _ := created["id"].(string)
	before := created["updated_at"]

	rec = doJSON(t, srv, http.MethodPatch, "/api/v1/assets/"+id, tok, `{}`)
	if rec.Code != http.StatusUnprocessableEntity || errorCode(rec.Body) != "validation_error" {
		t.Fatalf("empty patch: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, srv, http.MethodPatch, "/api/v1/assets/"+id, tok, `{"not_a_field":1}`)
	if rec.Code != http.StatusUnprocessableEntity || errorCode(rec.Body) != "validation_error" {
		t.Fatalf("unknown field: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/v1/assets/"+id, tok, "")
	_ = json.NewDecoder(rec.Body).Decode(&created)
	if created["updated_at"] != before {
		t.Fatalf("updated_at changed on rejected patch: %v -> %v", before, created["updated_at"])
	}
}

func TestPurgeRequiresSoftDelete(t *testing.T) {
	_, srv, _ := testServer(t)
	tok := bootstrap(t, srv)
	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets", tok, sampleCreate("purge-active.example"))
	var created map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&created)
	id, _ := created["id"].(string)

	stepUp(t, srv, tok)
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+id+"/purge", tok, "")
	if rec.Code != http.StatusConflict || errorCode(rec.Body) != "conflict" {
		t.Fatalf("purge active: %d %s", rec.Code, rec.Body.String())
	}
}

func TestPurgeWithoutStepUp(t *testing.T) {
	_, srv, _ := testServer(t)
	tok := bootstrap(t, srv)
	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets", tok, sampleCreate("purge-stepup.example"))
	var created map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&created)
	id, _ := created["id"].(string)

	stepUp(t, srv, tok)
	rec = doJSON(t, srv, http.MethodDelete, "/api/v1/assets/"+id, tok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("soft-delete: %d %s", rec.Code, rec.Body.String())
	}

	// New login without step-up
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/auth/login", "", `{"username":"admin","password":"password1"}`)
	tok = cookieValue(rec, auth.SessionCookieName)
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+id+"/purge", tok, "")
	if rec.Code != http.StatusForbidden || errorCode(rec.Body) != "step_up_required" {
		t.Fatalf("purge without step-up: %d %s", rec.Code, rec.Body.String())
	}
}

func TestDestructiveOriginRequired(t *testing.T) {
	_, srv, _ := testServer(t)
	tok := bootstrap(t, srv)
	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets", tok, sampleCreate("origin-del.example"))
	var created map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&created)
	id, _ := created["id"].(string)
	stepUp(t, srv, tok)

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/assets/"+id, nil)
	req.Host = "example.test"
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: tok})
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || errorCode(rec.Body) != "forbidden" {
		t.Fatalf("delete without origin: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/assets/"+id+"/purge", nil)
	req.Host = "example.test"
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: tok})
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || errorCode(rec.Body) != "forbidden" {
		t.Fatalf("purge without origin: %d %s", rec.Code, rec.Body.String())
	}
}

func TestListDefaultLimit(t *testing.T) {
	_, srv, _ := testServer(t)
	tok := bootstrap(t, srv)
	rec := doJSON(t, srv, http.MethodGet, "/api/v1/assets", tok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	var list map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&list)
	if int(list["limit"].(float64)) != 50 {
		t.Fatalf("default limit: %#v", list["limit"])
	}
}

func TestPurgeCascadesChildren(t *testing.T) {
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
	store := assets.NewStore(sqlDB)
	assetH := assets.NewHandler(store, assets.Options{})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", ah.Login)
	mux.HandleFunc("/api/v1/auth/step-up", ah.StepUp)
	mux.HandleFunc("/api/v1/setup/bootstrap", ah.Bootstrap)
	assetH.Register(mux, ah.RequireOrigin)
	srv := ah.Middleware(mux)

	tok := bootstrap(t, srv)
	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets", tok,
		`{"name":"Cascade","hostname":"cascade.example","asset_type":"vm","os_family":"linux","environment":"lab","tags":["cascade"]}`)
	var created map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&created)
	id, _ := created["id"].(string)

	accID := "01a0932e-0000-7000-8000-000000000001"
	_, err = sqlDB.Exec(
		`INSERT INTO accounts (id, asset_id, username, auth_type, description) VALUES (?, ?, 'root', 'password', '')`,
		accID, id,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = sqlDB.Exec(
		`INSERT INTO secret_payloads (account_id, nonce, ciphertext, wrapped_dek, key_version)
		 VALUES (?, X'00', X'00', X'00', 'v1')`,
		accID,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = sqlDB.Exec(
		`INSERT INTO scheduled_jobs (id, asset_id, name, scheduler_type) VALUES (?, ?, 'nightly', 'cron')`,
		"01a0932e-0000-7000-8000-000000000002", id,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = sqlDB.Exec(
		`INSERT INTO asset_notes (id, asset_id, title, body) VALUES (?, ?, 'note', 'body')`,
		"01a0932e-0000-7000-8000-000000000003", id,
	)
	if err != nil {
		t.Fatal(err)
	}

	stepUp(t, srv, tok)
	rec = doJSON(t, srv, http.MethodDelete, "/api/v1/assets/"+id, tok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("soft-delete: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+id+"/purge", tok, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("purge: %d %s", rec.Code, rec.Body.String())
	}

	var n int
	_ = sqlDB.QueryRow(`SELECT COUNT(*) FROM accounts WHERE asset_id = ?`, id).Scan(&n)
	if n != 0 {
		t.Fatalf("accounts remain: %d", n)
	}
	_ = sqlDB.QueryRow(`SELECT COUNT(*) FROM secret_payloads WHERE account_id = ?`, accID).Scan(&n)
	if n != 0 {
		t.Fatalf("secrets remain: %d", n)
	}
	_ = sqlDB.QueryRow(`SELECT COUNT(*) FROM scheduled_jobs WHERE asset_id = ?`, id).Scan(&n)
	if n != 0 {
		t.Fatalf("jobs remain: %d", n)
	}
	_ = sqlDB.QueryRow(`SELECT COUNT(*) FROM asset_notes WHERE asset_id = ?`, id).Scan(&n)
	if n != 0 {
		t.Fatalf("notes remain: %d", n)
	}
	_ = sqlDB.QueryRow(`SELECT COUNT(*) FROM asset_tags WHERE asset_id = ?`, id).Scan(&n)
	if n != 0 {
		t.Fatalf("asset_tags remain: %d", n)
	}
}

func TestInvalidOwner(t *testing.T) {
	_, srv, _ := testServer(t)
	tok := bootstrap(t, srv)
	body := `{"name":"X","hostname":"bad-owner.example","asset_type":"vm","os_family":"linux","environment":"dev","owner_id":"01a0932e-0000-7000-8000-ffffffffffff"}`
	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets", tok, body)
	if rec.Code != http.StatusUnprocessableEntity || errorCode(rec.Body) != "validation_error" {
		t.Fatalf("bad owner create: %d %s", rec.Code, rec.Body.String())
	}
}
