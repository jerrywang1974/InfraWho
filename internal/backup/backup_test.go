package backup_test

import (
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jerrywang1974/InfraWho/internal/accounts"
	"github.com/jerrywang1974/InfraWho/internal/assets"
	"github.com/jerrywang1974/InfraWho/internal/audit"
	"github.com/jerrywang1974/InfraWho/internal/auth"
	"github.com/jerrywang1974/InfraWho/internal/backup"
	"github.com/jerrywang1974/InfraWho/internal/config"
	"github.com/jerrywang1974/InfraWho/internal/db"
	"github.com/jerrywang1974/InfraWho/internal/ratelimit"
)

func writeTempKEK(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "master.key")
	key := make([]byte, config.MasterKeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testServer(t *testing.T, exportSecrets bool) (http.Handler, *audit.Store, *auth.Store) {
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
	loadKEK := func() ([]byte, error) { return config.LoadMasterKey(kekPath) }
	accountStore := accounts.NewStore(sqlDB, accounts.StoreOptions{KeyVersion: "kek-v1", LoadKEK: loadKEK})
	accountH := accounts.NewHandler(accountStore, auditStore, accounts.Options{Limiter: limiter})
	backupStore := backup.NewStore(sqlDB, backup.StoreOptions{KeyVersion: "kek-v1", LoadKEK: loadKEK})
	backupH := backup.NewHandler(backupStore, auditStore, backup.Options{FeatureExportSecrets: exportSecrets})

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
	backupH.Register(mux, ah.RequireOrigin)
	audit.NewHandler(auditStore).Register(mux)
	return ah.Middleware(mux), auditStore, authStore
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

func seedAssetWithSecret(t *testing.T, srv http.Handler, tok, hostname string) {
	t.Helper()
	body := `{"name":"Host","hostname":"` + hostname + `","asset_type":"vm","os_family":"linux","environment":"lab","purpose":"test","tags":["lab"]}`
	rec := doJSON(t, srv, http.MethodPost, "/api/v1/assets", tok, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create asset: %d %s", rec.Code, rec.Body.String())
	}
	var a map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&a)
	id, _ := a["id"].(string)
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/assets/"+id+"/accounts", tok,
		`{"username":"deploy","auth_type":"password","description":"ci","secret":"s3cret"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create account: %d %s", rec.Code, rec.Body.String())
	}
}

func TestExportRejectsGET(t *testing.T) {
	srv, _, _ := testServer(t, false)
	tok := bootstrap(t, srv)
	rec := doJSON(t, srv, http.MethodGet, "/api/v1/export", tok, "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET export: %d %s", rec.Code, rec.Body.String())
	}
}

func TestExportMetadataMatrixB(t *testing.T) {
	srv, auditStore, _ := testServer(t, false)
	tok := bootstrap(t, srv)
	seedAssetWithSecret(t, srv, tok, "export-meta.example")

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/export", tok, `{"format":"json","include_secrets":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("export: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control: %q", rec.Header().Get("Cache-Control"))
	}
	var doc backup.Document
	if err := json.NewDecoder(rec.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if doc.IncludeSecrets || len(doc.Assets) != 1 {
		t.Fatalf("doc: include=%v assets=%d", doc.IncludeSecrets, len(doc.Assets))
	}
	acc := doc.Assets[0].Accounts[0]
	if !acc.HasSecret || acc.Secret != nil {
		t.Fatalf("metadata export must not include secret field: %#v", acc)
	}

	events, total, err := auditStore.List(audit.ListFilter{Action: audit.ActionExportMetadata, Limit: 5})
	if err != nil || total < 1 || events[0].Action != audit.ActionExportMetadata {
		t.Fatalf("audit metadata: total=%d err=%v", total, err)
	}
}

func TestExportSecretsFeatureDisabled(t *testing.T) {
	srv, _, _ := testServer(t, false)
	tok := bootstrap(t, srv)
	seedAssetWithSecret(t, srv, tok, "export-off.example")
	stepUp(t, srv, tok)

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/export", tok, `{"include_secrets":true}`)
	if rec.Code != http.StatusForbidden || errorCode(rec.Body) != "feature_disabled" {
		t.Fatalf("expected feature_disabled: %d %s", rec.Code, rec.Body.String())
	}
}

func TestExportSecretsMatrixC(t *testing.T) {
	srv, auditStore, authStore := testServer(t, true)
	tok := bootstrap(t, srv)
	seedAssetWithSecret(t, srv, tok, "export-sec.example")

	// Without step-up
	rec := doJSON(t, srv, http.MethodPost, "/api/v1/export", tok, `{"include_secrets":true}`)
	if rec.Code != http.StatusForbidden || errorCode(rec.Body) != "step_up_required" {
		t.Fatalf("secrets without step-up: %d %s", rec.Code, rec.Body.String())
	}

	stepUp(t, srv, tok)
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/export", tok, `{"include_secrets":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("export secrets: %d %s", rec.Code, rec.Body.String())
	}
	var doc backup.Document
	_ = json.NewDecoder(rec.Body).Decode(&doc)
	if !doc.IncludeSecrets || doc.Assets[0].Accounts[0].Secret == nil || *doc.Assets[0].Accounts[0].Secret != "s3cret" {
		t.Fatalf("expected plaintext secret: %#v", doc.Assets[0].Accounts[0])
	}

	events, total, err := auditStore.List(audit.ListFilter{Action: audit.ActionExportWithSecrets, Limit: 5})
	if err != nil || total < 1 {
		t.Fatalf("audit secrets: total=%d err=%v", total, err)
	}
	_ = events

	// Operator cannot export secrets even with flag + step-up
	hash, err := auth.HashPassword("password1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authStore.CreateUser("op1", "Op", auth.RoleOperator, hash); err != nil {
		t.Fatal(err)
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/auth/login", "", `{"username":"op1","password":"password1"}`)
	opTok := cookieValue(rec, auth.SessionCookieName)
	stepUp(t, srv, opTok)
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/export", opTok, `{"include_secrets":true}`)
	if rec.Code != http.StatusForbidden || errorCode(rec.Body) != "forbidden" {
		t.Fatalf("operator secrets export: %d %s", rec.Code, rec.Body.String())
	}
}

func TestImportRejectsHostnameConflict(t *testing.T) {
	srv, _, _ := testServer(t, false)
	tok := bootstrap(t, srv)
	seedAssetWithSecret(t, srv, tok, "dup.example")

	body := `{
		"version":1,
		"include_secrets":false,
		"assets":[{
			"name":"Other","hostname":"dup.example","asset_type":"vm","os_family":"linux",
			"environment":"lab","purpose":"x","status":"active","additional_ips":[],
			"tags":[],"accounts":[],"jobs":[],"notes":[]
		}]
	}`
	rec := doJSON(t, srv, http.MethodPost, "/api/v1/import", tok, body)
	if rec.Code != http.StatusConflict || errorCode(rec.Body) != "hostname_conflict" {
		t.Fatalf("import conflict: %d %s", rec.Code, rec.Body.String())
	}
}

func TestImportWithSecretsRequiresStepUpAndReseals(t *testing.T) {
	srv, auditStore, _ := testServer(t, false)
	tok := bootstrap(t, srv)

	body := `{
		"version":1,
		"include_secrets":true,
		"assets":[{
			"name":"Imported","hostname":"import-new.example","asset_type":"vm","os_family":"linux",
			"os_detail":"","environment":"lab","purpose":"imported","primary_ip":"",
			"additional_ips":[],"location":"","hypervisor":"","status":"active","config_notes":"",
			"tags":["imported"],
			"accounts":[{"username":"root","auth_type":"password","description":"","has_secret":true,"secret":"hunter2"}],
			"jobs":[{"name":"nightly","scheduler_type":"cron","schedule_expr":"0 2 * * *","command_or_path":"/bin/true","description":"","enabled_doc":true}],
			"notes":[{"title":"n","body":"hello"}]
		}]
	}`

	rec := doJSON(t, srv, http.MethodPost, "/api/v1/import", tok, body)
	if rec.Code != http.StatusForbidden || errorCode(rec.Body) != "step_up_required" {
		t.Fatalf("import secrets without step-up: %d %s", rec.Code, rec.Body.String())
	}

	stepUp(t, srv, tok)
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/import", tok, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("import: %d %s", rec.Code, rec.Body.String())
	}
	var result map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&result)
	if result["imported_assets"] != float64(1) || result["imported_accounts"] != float64(1) {
		t.Fatalf("result: %#v", result)
	}

	events, total, err := auditStore.List(audit.ListFilter{Action: audit.ActionImport, Limit: 5})
	if err != nil || total < 1 {
		t.Fatalf("import audit: total=%d err=%v", total, err)
	}
	_ = events

	// Reveal should return the imported plaintext (resealed under current KEK).
	rec = doJSON(t, srv, http.MethodPost, "/api/v1/export", tok, `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("re-export: %d %s", rec.Code, rec.Body.String())
	}
	var doc backup.Document
	_ = json.NewDecoder(rec.Body).Decode(&doc)
	found := false
	for _, a := range doc.Assets {
		if a.Hostname == "import-new.example" {
			found = true
			if len(a.Accounts) != 1 || !a.Accounts[0].HasSecret {
				t.Fatalf("imported account missing secret flag: %#v", a.Accounts)
			}
			if a.Accounts[0].Secret != nil {
				t.Fatal("metadata export must omit secret")
			}
			if len(a.Jobs) != 1 || len(a.Notes) != 1 {
				t.Fatalf("jobs/notes: jobs=%d notes=%d", len(a.Jobs), len(a.Notes))
			}
		}
	}
	if !found {
		t.Fatal("imported asset missing from export")
	}
}

func TestBackupScriptExcludesMasterKey(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "scripts", "backup.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("backup.sh missing: %v", err)
	}

	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	keyDir := filepath.Join(dir, "etc", "infrawho")
	outDir := filepath.Join(dir, "backups")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(keyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "infrawho.db")
	keyPath := filepath.Join(keyDir, "master.key")
	if err := os.WriteFile(dbPath, []byte("SQLite fake db"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("not-a-real-key-but-named-master"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Tempt the script: also place a master.key next to the DB.
	if err := os.WriteFile(filepath.Join(dataDir, "master.key"), []byte("adjacent-key"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", script, "--db", dbPath, "--out", outDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("backup.sh failed: %v\n%s", err, out)
	}
	archive := strings.TrimSpace(string(out))
	if archive == "" || !strings.HasSuffix(archive, ".tar.gz") {
		t.Fatalf("unexpected archive path: %q", archive)
	}

	listCmd := exec.Command("tar", "-tzf", archive)
	listOut, err := listCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tar tz: %v\n%s", err, listOut)
	}
	listing := string(listOut)
	if strings.Contains(listing, "master.key") || strings.Contains(listing, ".key") {
		t.Fatalf("archive must not contain key paths:\n%s", listing)
	}
	if !strings.Contains(listing, "infrawho.db") {
		t.Fatalf("archive missing db:\n%s", listing)
	}

	// Restore round-trip
	restoreScript := filepath.Join(root, "scripts", "restore.sh")
	restored := filepath.Join(dir, "restored.db")
	cmd = exec.Command("bash", restoreScript, "--archive", archive, "--db", restored)
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restore.sh failed: %v\n%s", err, out)
	}
	got, err := os.ReadFile(restored)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "SQLite fake db" {
		t.Fatalf("restored content mismatch: %q", got)
	}
}
