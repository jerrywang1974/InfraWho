package main

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jerrywang1974/InfraWho/internal/db"
	"github.com/jerrywang1974/InfraWho/internal/vault"
)

func TestRewrapSecretPayloadsIntegration(t *testing.T) {
	sqlDB, err := db.Open("sqlite::memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer sqlDB.Close()

	fromKEK := bytes.Repeat([]byte{0x11}, vault.DEKSize)
	toKEK := bytes.Repeat([]byte{0x22}, vault.DEKSize)

	mustExec(t, sqlDB, `INSERT INTO users (id, username, display_name, role, password_hash)
		VALUES ('u1', 'admin', 'Admin', 'admin', 'x')`)
	mustExec(t, sqlDB, `INSERT INTO assets (id, name, hostname, asset_type, os_family, environment)
		VALUES ('as1', 'box', 'box.example', 'vm', 'linux', 'lab')`)
	mustExec(t, sqlDB, `INSERT INTO accounts (id, asset_id, username, auth_type)
		VALUES ('acc1', 'as1', 'root', 'password')`)
	mustExec(t, sqlDB, `INSERT INTO accounts (id, asset_id, username, auth_type)
		VALUES ('acc2', 'as1', 'deploy', 'password')`)

	plain1 := []byte("password-one")
	plain2 := []byte("password-two")
	env1, err := vault.Seal(plain1, "acc1", "password", fromKEK)
	if err != nil {
		t.Fatal(err)
	}
	env2, err := vault.Seal(plain2, "acc2", "password", fromKEK)
	if err != nil {
		t.Fatal(err)
	}

	mustExec(t, sqlDB,
		`INSERT INTO secret_payloads (account_id, nonce, ciphertext, wrapped_dek, key_version)
		 VALUES (?, ?, ?, ?, ?)`,
		"acc1", env1.Nonce, env1.Ciphertext, env1.WrappedDEK, "kek-v1",
	)
	mustExec(t, sqlDB,
		`INSERT INTO secret_payloads (account_id, nonce, ciphertext, wrapped_dek, key_version)
		 VALUES (?, ?, ?, ?, ?)`,
		"acc2", env2.Nonce, env2.Ciphertext, env2.WrappedDEK, "kek-v1",
	)

	origNonce1 := append([]byte(nil), env1.Nonce...)
	origCT1 := append([]byte(nil), env1.Ciphertext...)

	updated, err := rewrapSecretPayloads(sqlDB, "kek-v1", "kek-v2", fromKEK, toKEK)
	if err != nil {
		t.Fatalf("rewrap: %v", err)
	}
	if updated != 2 {
		t.Fatalf("updated = %d, want 2", updated)
	}

	updated, err = rewrapSecretPayloads(sqlDB, "kek-v1", "kek-v2", fromKEK, toKEK)
	if err != nil {
		t.Fatalf("rewrap again: %v", err)
	}
	if updated != 0 {
		t.Fatalf("second rewrap updated = %d, want 0", updated)
	}

	var nonce, ct, wrapped []byte
	var ver string
	if err := sqlDB.QueryRow(
		`SELECT nonce, ciphertext, wrapped_dek, key_version FROM secret_payloads WHERE account_id = ?`,
		"acc1",
	).Scan(&nonce, &ct, &wrapped, &ver); err != nil {
		t.Fatal(err)
	}
	if ver != "kek-v2" {
		t.Fatalf("key_version = %q, want kek-v2", ver)
	}
	if !bytes.Equal(nonce, origNonce1) || !bytes.Equal(ct, origCT1) {
		t.Fatal("nonce/ciphertext changed during rewrap")
	}

	got, err := vault.Open(&vault.Envelope{
		Nonce:      nonce,
		Ciphertext: ct,
		WrappedDEK: wrapped,
	}, "acc1", "password", toKEK)
	if err != nil {
		t.Fatalf("Open after DB rewrap: %v", err)
	}
	if !bytes.Equal(got, plain1) {
		t.Fatalf("plaintext = %q, want %q", got, plain1)
	}
	if _, err := vault.Open(&vault.Envelope{
		Nonce: nonce, Ciphertext: ct, WrappedDEK: wrapped,
	}, "acc1", "ssh_private_key", toKEK); err == nil {
		t.Fatal("expected wrong AAD to fail after DB rewrap")
	}
}

func TestRunKeysRewrapCLI(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "lab.db")
	oldKey := filepath.Join(dir, "old.key")
	newKey := filepath.Join(dir, "new.key")
	fromKEK := bytes.Repeat([]byte{0x33}, vault.DEKSize)
	toKEK := bytes.Repeat([]byte{0x44}, vault.DEKSize)
	if err := os.WriteFile(oldKey, fromKEK, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newKey, toKEK, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("INFRAWHO_DB_URL", "sqlite:///"+dbPath)

	sqlDB, err := db.Open("sqlite:///" + dbPath)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, sqlDB, `INSERT INTO users (id, username, display_name, role, password_hash)
		VALUES ('u1', 'admin', 'Admin', 'admin', 'x')`)
	mustExec(t, sqlDB, `INSERT INTO assets (id, name, hostname, asset_type, os_family, environment)
		VALUES ('as1', 'box', 'box.example', 'vm', 'linux', 'lab')`)
	mustExec(t, sqlDB, `INSERT INTO accounts (id, asset_id, username, auth_type)
		VALUES ('acc1', 'as1', 'root', 'password')`)
	env, err := vault.Seal([]byte("cli-secret"), "acc1", "password", fromKEK)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, sqlDB,
		`INSERT INTO secret_payloads (account_id, nonce, ciphertext, wrapped_dek, key_version)
		 VALUES (?, ?, ?, ?, ?)`,
		"acc1", env.Nonce, env.Ciphertext, env.WrappedDEK, "kek-v1",
	)
	_ = sqlDB.Close()

	if err := runKeysRewrap([]string{
		"--from", "kek-v1",
		"--to", "kek-v2",
		"--old-key-file", oldKey,
		"--new-key-file", newKey,
	}); err != nil {
		t.Fatalf("runKeysRewrap: %v", err)
	}

	sqlDB, err = db.Open("sqlite:///" + dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	var ver string
	var wrapped []byte
	if err := sqlDB.QueryRow(
		`SELECT key_version, wrapped_dek FROM secret_payloads WHERE account_id = ?`, "acc1",
	).Scan(&ver, &wrapped); err != nil {
		t.Fatal(err)
	}
	if ver != "kek-v2" {
		t.Fatalf("key_version = %q", ver)
	}
	got, err := vault.Open(&vault.Envelope{
		Nonce:      env.Nonce,
		Ciphertext: env.Ciphertext,
		WrappedDEK: wrapped,
	}, "acc1", "password", toKEK)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if string(got) != "cli-secret" {
		t.Fatalf("got %q", got)
	}
}

func TestRunKeysRewrapRejectsSameKeyFile(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "same.key")
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte{0x55}, vault.DEKSize), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INFRAWHO_DB_URL", "sqlite:///"+filepath.Join(dir, "unused.db"))

	err := runKeysRewrap([]string{
		"--from", "kek-v1",
		"--to", "kek-v2",
		"--old-key-file", keyPath,
		"--new-key-file", keyPath,
	})
	if err == nil {
		t.Fatal("expected error for identical key file paths")
	}
	if !strings.Contains(err.Error(), "different paths") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunKeysRewrapRejectsIdenticalKeyMaterial(t *testing.T) {
	dir := t.TempDir()
	oldKey := filepath.Join(dir, "old.key")
	newKey := filepath.Join(dir, "new.key")
	same := bytes.Repeat([]byte{0x66}, vault.DEKSize)
	if err := os.WriteFile(oldKey, same, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newKey, same, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INFRAWHO_DB_URL", "sqlite:///"+filepath.Join(dir, "unused.db"))

	err := runKeysRewrap([]string{
		"--from", "kek-v1",
		"--to", "kek-v2",
		"--old-key-file", oldKey,
		"--new-key-file", newKey,
	})
	if err == nil {
		t.Fatal("expected error for identical KEK material")
	}
	if !strings.Contains(err.Error(), "identical") {
		t.Fatalf("err = %v", err)
	}
}

func mustExec(t *testing.T, sqlDB *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := sqlDB.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}
