package notes_test

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
	"github.com/jerrywang1974/InfraWho/internal/notes"
	"github.com/jerrywang1974/InfraWho/internal/ratelimit"
)

func testServer(t *testing.T) http.Handler {
	t.Helper()
	sqlDB, err := db.Open("sqlite::memory:")
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	ah := auth.NewHandler(auth.NewStore(sqlDB), ratelimit.New(), auth.Options{
		CookieSecure:   false,
		MasterKeyReady: func() bool { return true },
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/setup/bootstrap", ah.Bootstrap)
	mux.HandleFunc("/api/v1/auth/step-up", ah.StepUp)
	assets.NewHandler(assets.NewStore(sqlDB), assets.Options{}).Register(mux, ah.RequireOrigin)
	notes.NewHandler(notes.NewStore(sqlDB)).Register(mux, ah.RequireOrigin)
	return ah.Middleware(mux)
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

func TestNoteCRUDAndPagination(t *testing.T) {
	srv := testServer(t)
	tok := bootstrap(t, srv)
	assetID := createAsset(t, srv, tok, "notes-1.example")

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+assetID+"/notes", tok,
		`{"title":"Runbook","body":"# Restart\n\nsystemctl restart app"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	noteID, _ := created["id"].(string)
	if noteID == "" || created["title"] != "Runbook" {
		t.Fatalf("created: %#v", created)
	}
	if created["author_id"] == nil || created["author_id"] == "" {
		t.Fatalf("author_id missing: %#v", created)
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+assetID+"/notes", tok,
		`{"title":"Second","body":"more"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create2: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/v1/assets/"+assetID+"/notes?limit=1&offset=0", tok, "")
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

	rec = doJSON(t, srv, http.MethodPatch, "/api/v1/notes/"+noteID, tok,
		`{"body":"updated body"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	var patched map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&patched)
	if patched["body"] != "updated body" || patched["title"] != "Runbook" {
		t.Fatalf("patched: %#v", patched)
	}

	rec = doJSON(t, srv, http.MethodDelete, "/api/v1/notes/"+noteID, tok, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, http.MethodGet, "/api/v1/assets/"+assetID+"/notes", tok, "")
	_ = json.NewDecoder(rec.Body).Decode(&list)
	if int(list["total"].(float64)) != 1 {
		t.Fatalf("after delete total: %#v", list["total"])
	}
}

func TestNoteEmptyPatchAndValidation(t *testing.T) {
	srv := testServer(t)
	tok := bootstrap(t, srv)
	assetID := createAsset(t, srv, tok, "notes-val.example")

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+assetID+"/notes", tok,
		`{"title":"ok","body":"x"}`)
	var created map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&created)
	noteID, _ := created["id"].(string)

	rec = doJSON(t, srv, http.MethodPatch, "/api/v1/notes/"+noteID, tok, `{}`)
	if rec.Code != http.StatusUnprocessableEntity || errorCode(rec.Body) != "validation_error" {
		t.Fatalf("empty patch: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, srv, http.MethodPatch, "/api/v1/notes/"+noteID, tok, `{"extra":true}`)
	if rec.Code != http.StatusUnprocessableEntity || errorCode(rec.Body) != "validation_error" {
		t.Fatalf("unknown field: %d %s", rec.Code, rec.Body.String())
	}

	tooLong := strings.Repeat("a", 64*1024+1)
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+assetID+"/notes", tok,
		`{"title":"big","body":"`+tooLong+`"}`)
	if rec.Code != http.StatusUnprocessableEntity || errorCode(rec.Body) != "validation_error" {
		t.Fatalf("body too long: %d %s", rec.Code, rec.Body.String())
	}
}

func TestNoteSoftDeletedAssetRejectsCreate(t *testing.T) {
	srv := testServer(t)
	tok := bootstrap(t, srv)
	assetID := createAsset(t, srv, tok, "notes-retired.example")
	stepUp(t, srv, tok)
	rec := doJSON(t, srv, http.MethodDelete, "/api/v1/assets/"+assetID, tok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("soft-delete: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+assetID+"/notes", tok,
		`{"title":"nope","body":"x"}`)
	if rec.Code != http.StatusConflict || errorCode(rec.Body) != "conflict" {
		t.Fatalf("create on deleted: %d %s", rec.Code, rec.Body.String())
	}
}

func TestNoteMissingAsset(t *testing.T) {
	srv := testServer(t)
	tok := bootstrap(t, srv)
	missing := "01a0932e-0000-7000-8000-ffffffffffff"
	rec := doJSON(t, srv, http.MethodGet, "/api/v1/assets/"+missing+"/notes", tok, "")
	if rec.Code != http.StatusNotFound || errorCode(rec.Body) != "not_found" {
		t.Fatalf("list missing: %d %s", rec.Code, rec.Body.String())
	}
}

func TestNoteRequiresAuth(t *testing.T) {
	srv := testServer(t)
	rec := doJSON(t, srv, http.MethodGet, "/api/v1/assets/x/notes", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth list: %d", rec.Code)
	}
}
