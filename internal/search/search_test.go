package search_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jerrywang1974/InfraWho/internal/assets"
	"github.com/jerrywang1974/InfraWho/internal/auth"
	"github.com/jerrywang1974/InfraWho/internal/db"
	"github.com/jerrywang1974/InfraWho/internal/jobs"
	"github.com/jerrywang1974/InfraWho/internal/notes"
	"github.com/jerrywang1974/InfraWho/internal/ratelimit"
	"github.com/jerrywang1974/InfraWho/internal/search"
)

func testServer(t *testing.T) (http.Handler, *search.Store, *sql.DB) {
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
	searchStore := search.NewStore(sqlDB)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/setup/bootstrap", ah.Bootstrap)
	mux.HandleFunc("/api/v1/auth/login", ah.Login)
	assets.NewHandler(assets.NewStore(sqlDB), assets.Options{}).Register(mux, ah.RequireOrigin)
	jobs.NewHandler(jobs.NewStore(sqlDB)).Register(mux, ah.RequireOrigin)
	notes.NewHandler(notes.NewStore(sqlDB)).Register(mux, ah.RequireOrigin)
	search.NewHandler(searchStore).Register(mux, ah.RequireOrigin)
	return ah.Middleware(mux), searchStore, sqlDB
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

func createAsset(t *testing.T, srv http.Handler, tok, hostname, purpose string, tags []string) string {
	t.Helper()
	tagJSON := "[]"
	if len(tags) > 0 {
		b, _ := json.Marshal(tags)
		tagJSON = string(b)
	}
	body := `{"name":"Host ` + hostname + `","hostname":"` + hostname + `","asset_type":"vm","os_family":"linux","environment":"prod","purpose":"` + purpose + `","os_detail":"Ubuntu 24.04","location":"rack-a","primary_ip":"10.0.0.1","config_notes":"wired for HA","tags":` + tagJSON + `}`
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

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec: %v", err)
	}
}

func TestBuildFTSQuery(t *testing.T) {
	q, ok := search.BuildFTSQuery("  postgres db  ")
	if !ok || q != "postgres* db*" {
		t.Fatalf("got %q ok=%v", q, ok)
	}
	if _, ok := search.BuildFTSQuery("   "); ok {
		t.Fatal("expected empty")
	}
	if _, ok := search.BuildFTSQuery("1"); ok {
		t.Fatal("single-char token should be dropped")
	}
	q, ok = search.BuildFTSQuery("host-1.example")
	// "1" dropped (len < 2)
	if !ok || q != "host* example*" {
		t.Fatalf("hostname token: %q ok=%v", q, ok)
	}
	q, ok = search.BuildFTSQuery("OR NOT")
	if !ok || q != `"OR" "NOT"` {
		t.Fatalf("quoted keywords: %q ok=%v", q, ok)
	}
	var parts []string
	for i := 0; i < 20; i++ {
		parts = append(parts, fmt.Sprintf("tok%d", i))
	}
	q, ok = search.BuildFTSQuery(strings.Join(parts, " "))
	if !ok || len(strings.Fields(q)) != 16 {
		t.Fatalf("token cap: %q (%d fields)", q, len(strings.Fields(q)))
	}
}

func TestSearchIndexesNonSecretsAndNotSecrets(t *testing.T) {
	srv, store, sqlDB := testServer(t)
	tok := bootstrap(t, srv)

	id := createAsset(t, srv, tok, "db-1.example", "primary postgres", []string{"db", "postgres"})

	// Account username indexed; description must not be.
	mustExec(t, sqlDB, `INSERT INTO accounts (id, asset_id, username, auth_type, description)
		VALUES ('acc1', ?, 'deployer', 'password', 'NEVERINDEX_ACCT_DESC')`, id)
	mustExec(t, sqlDB, `INSERT INTO secret_payloads (account_id, nonce, ciphertext, wrapped_dek, key_version)
		VALUES ('acc1', X'dead', X'4e45564552494e444558434950484552', X'beef', 'kek-v1')`)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+id+"/jobs", tok,
		`{"name":"nightly-backup","scheduler_type":"cron","schedule_expr":"0 2 * * *","description":"dumps to NAS","enabled_doc":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create job: %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+id+"/notes", tok,
		`{"title":"failover-runbook","body":"NEVERINDEX_NOTE_BODY with secret sauce"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create note: %d %s", rec.Code, rec.Body.String())
	}

	if bad, err := search.FTSContainsSecretColumn(sqlDB); err != nil || bad {
		t.Fatalf("FTS view must not reference secrets: bad=%v err=%v", bad, err)
	}

	cases := []struct {
		q      string
		want   bool
		reason string
	}{
		{"postgres", true, "asset purpose/tags"},
		{"deployer", true, "account username"},
		{"nightly-backup", true, "job name"},
		{"dumps", true, "job description"},
		{"failover-runbook", true, "note title"},
		{"Ubuntu", true, "os_detail"},
		{"rack-a", true, "location"},
		{"NEVERINDEX_ACCT_DESC", false, "account description must not be indexed"},
		{"NEVERINDEX_NOTE_BODY", false, "note body must not be indexed"},
		{"NEVERINDEXCIPHER", false, "secret ciphertext must not be indexed"},
	}

	mustExec(t, sqlDB, `UPDATE assets SET additional_ips = ?, hypervisor = ? WHERE id = ?`,
		`["10.9.8.7"]`, "esxi-lab-1", id)
	for _, tc := range []struct {
		q    string
		want bool
	}{
		{"10.9.8.7", true},
		{"esxi-lab", true},
	} {
		hits, _, err := store.SearchAssets(search.Filter{Q: tc.q, Limit: 50})
		if err != nil {
			t.Fatalf("q=%q: %v", tc.q, err)
		}
		found := false
		for _, h := range hits {
			if h.AssetID == id {
				found = true
			}
		}
		if found != tc.want {
			t.Fatalf("q=%q additional/hypervisor: found=%v", tc.q, found)
		}
	}
	for _, tc := range cases {
		hits, total, err := store.SearchAssets(search.Filter{Q: tc.q, Limit: 50})
		if err != nil {
			t.Fatalf("q=%q: %v", tc.q, err)
		}
		found := false
		for _, h := range hits {
			if h.AssetID == id {
				found = true
				break
			}
		}
		if found != tc.want {
			t.Fatalf("q=%q (%s): found=%v want=%v total=%d hits=%v", tc.q, tc.reason, found, tc.want, total, hits)
		}
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/v1/search?q=deployer", tok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("search: %d %s", rec.Code, rec.Body.String())
	}
	var searchResp struct {
		Items []search.Hit `json:"items"`
		Total int          `json:"total"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&searchResp)
	if searchResp.Total != 1 || len(searchResp.Items) != 1 || searchResp.Items[0].AssetID != id {
		t.Fatalf("search resp: %+v", searchResp)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/v1/assets?q=failover-runbook", tok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("assets q: %d %s", rec.Code, rec.Body.String())
	}
	var listResp struct {
		Total int `json:"total"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&listResp)
	if listResp.Total != 1 {
		t.Fatalf("assets q total=%d", listResp.Total)
	}
}

func TestSearchAuthAndEmptyQuery(t *testing.T) {
	srv, _, _ := testServer(t)
	tok := bootstrap(t, srv)
	_ = createAsset(t, srv, tok, "web-1.example", "nginx edge", nil)

	rec := doJSON(t, srv, http.MethodGet, "/api/v1/search?q=nginx", "", "")
	if rec.Code != http.StatusUnauthorized || errorCode(rec.Body) != "unauthorized" {
		t.Fatalf("unauth: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/v1/search", tok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty q: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []search.Hit `json:"items"`
		Total int          `json:"total"`
		Limit int          `json:"limit"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Total != 0 || len(resp.Items) != 0 {
		t.Fatalf("empty q must return no hits, got %+v", resp)
	}
	if resp.Limit != 50 {
		t.Fatalf("default limit=%d", resp.Limit)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/v1/search?q=nginx&limit=999", tok, "")
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Limit != 200 {
		t.Fatalf("clamped limit=%d", resp.Limit)
	}
}

func TestFTSTriggersOnRelatedUpdates(t *testing.T) {
	srv, store, _ := testServer(t)
	tok := bootstrap(t, srv)
	id := createAsset(t, srv, tok, "app-1.example", "api tier", nil)

	_, total, err := store.SearchAssets(search.Filter{Q: "cron-cleanup"})
	if err != nil || total != 0 {
		t.Fatalf("pre: total=%d err=%v", total, err)
	}

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+id+"/jobs", tok,
		`{"name":"cron-cleanup","scheduler_type":"cron","description":"purge temp"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("job: %d %s", rec.Code, rec.Body.String())
	}
	hits, total, err := store.SearchAssets(search.Filter{Q: "cron-cleanup"})
	if err != nil || total != 1 || hits[0].AssetID != id {
		t.Fatalf("after job insert: total=%d err=%v hits=%v", total, err, hits)
	}

	var job map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &job)
	jobID, _ := job["id"].(string)
	if jobID == "" {
		t.Fatal("missing job id")
	}

	rec = doJSON(t, srv, http.MethodDelete, "/api/v1/jobs/"+jobID, tok, "")
	if rec.Code != http.StatusNoContent && rec.Code != http.StatusOK {
		t.Fatalf("delete job: %d %s", rec.Code, rec.Body.String())
	}
	_, total, err = store.SearchAssets(search.Filter{Q: "cron-cleanup"})
	if err != nil || total != 0 {
		t.Fatalf("after job delete: total=%d err=%v", total, err)
	}
}
