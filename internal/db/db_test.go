package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestSQLitePath(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"sqlite:///data/infrawho.db", "/data/infrawho.db"},
		{"sqlite:////tmp/x.db", "/tmp/x.db"},
		{"sqlite:relative.db", "relative.db"},
		{"sqlite:./data/lab.db", "./data/lab.db"},
		{"sqlite::memory:", ":memory:"},
		{"sqlite:file::memory:", ":memory:"},
		{"sqlite:file:/tmp/a.db", "/tmp/a.db"},
		{"sqlite:file:rel.db", "rel.db"},
		{"sqlite:///tmp/a.db?cache=shared", "/tmp/a.db"},
	}
	for _, tc := range tests {
		got, err := SQLitePath(tc.in)
		if err != nil {
			t.Fatalf("SQLitePath(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("SQLitePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if _, err := SQLitePath("postgres://x"); err == nil {
		t.Fatal("expected error for non-sqlite URL")
	}
}

func TestOpenMigratesSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "infrawho.db")
	db, err := Open("sqlite://" + path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	required := []string{
		"users", "sessions", "assets", "tags", "asset_tags",
		"accounts", "secret_payloads", "scheduled_jobs", "asset_notes",
		"audit_events", "app_settings", "schema_migrations",
	}
	for _, table := range required {
		if !tableExists(t, db, table) {
			t.Fatalf("missing table %s", table)
		}
	}
	if tableExists(t, db, "api_tokens") {
		t.Fatal("api_tokens must not exist")
	}

	var version int
	if err := db.QueryRow(`SELECT version FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatalf("schema_migrations: %v", err)
	}
	if version != 1 {
		t.Fatalf("version = %d, want 1", version)
	}

	// Idempotent second open / migrate.
	db2, err := Open("sqlite://" + path)
	if err != nil {
		t.Fatalf("re-Open: %v", err)
	}
	db2.Close()
}

func TestHostnamePartialUniqueAndOptionalSecret(t *testing.T) {
	db, err := Open("sqlite::memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	mustExec(t, db, `INSERT INTO users (id, username, role, password_hash) VALUES ('u1', 'admin', 'admin', 'x')`)
	mustExec(t, db, `INSERT INTO assets (id, name, hostname, asset_type, os_family, environment, owner_id, status)
		VALUES ('a1', 'one', 'host1.example', 'vm', 'linux', 'lab', 'u1', 'active')`)

	_, err = db.Exec(`INSERT INTO assets (id, name, hostname, asset_type, os_family, environment, status)
		VALUES ('a2', 'two', 'host1.example', 'vm', 'linux', 'lab', 'active')`)
	if err == nil {
		t.Fatal("expected unique hostname conflict among active assets")
	}

	mustExec(t, db, `UPDATE assets SET status='retired', deleted_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id='a1'`)
	mustExec(t, db, `INSERT INTO assets (id, name, hostname, asset_type, os_family, environment, status)
		VALUES ('a2', 'two', 'host1.example', 'vm', 'linux', 'lab', 'active')`)

	mustExec(t, db, `INSERT INTO accounts (id, asset_id, username, auth_type) VALUES ('c1', 'a2', 'root', 'password')`)
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM secret_payloads WHERE account_id='c1'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("secret_payloads count = %d, want 0 (optional)", n)
	}

	mustExec(t, db, `INSERT INTO secret_payloads (account_id, nonce, ciphertext, wrapped_dek, key_version)
		VALUES ('c1', X'01', X'02', X'03', 'kek-v1')`)
}

func TestSessionStepUpNullable(t *testing.T) {
	db, err := Open("sqlite::memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	mustExec(t, db, `INSERT INTO users (id, username, role, password_hash) VALUES ('u1', 'admin', 'admin', 'x')`)
	mustExec(t, db, `INSERT INTO sessions (id, user_id, token_hash, idle_expires_at, absolute_expires_at)
		VALUES ('s1', 'u1', 'th', '2099-01-01T00:00:00Z', '2099-01-01T12:00:00Z')`)

	var stepUp sql.NullString
	if err := db.QueryRow(`SELECT step_up_expires_at FROM sessions WHERE id='s1'`).Scan(&stepUp); err != nil {
		t.Fatal(err)
	}
	if stepUp.Valid {
		t.Fatalf("step_up_expires_at = %q, want NULL", stepUp.String)
	}
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name,
	).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n == 1
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %s: %v", query, err)
	}
}
