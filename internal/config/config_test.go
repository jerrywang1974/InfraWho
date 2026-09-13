package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv(EnvDBURL, "")
	t.Setenv(EnvMasterKeyFile, "")
	t.Setenv(EnvListenAddr, "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DBURL != DefaultDBURL {
		t.Fatalf("DBURL = %q, want %q", cfg.DBURL, DefaultDBURL)
	}
	if cfg.ListenAddr != DefaultListenAddr {
		t.Fatalf("ListenAddr = %q, want %q", cfg.ListenAddr, DefaultListenAddr)
	}
}

func TestLoadRejectsNonSQLite(t *testing.T) {
	t.Setenv(EnvDBURL, "postgres://localhost/infrawho")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for non-sqlite DB URL")
	}
}

func TestLoadMasterKeyRawAndBase64(t *testing.T) {
	dir := t.TempDir()
	raw := make([]byte, MasterKeySize)
	for i := range raw {
		raw[i] = byte(i)
	}

	rawPath := filepath.Join(dir, "raw.key")
	if err := os.WriteFile(rawPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadMasterKey(rawPath)
	if err != nil {
		t.Fatalf("raw: %v", err)
	}
	if string(got) != string(raw) {
		t.Fatalf("raw key mismatch")
	}

	b64Path := filepath.Join(dir, "b64.key")
	encoded := base64.StdEncoding.EncodeToString(raw) + "\n"
	if err := os.WriteFile(b64Path, []byte(encoded), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = LoadMasterKey(b64Path)
	if err != nil {
		t.Fatalf("base64: %v", err)
	}
	if string(got) != string(raw) {
		t.Fatalf("base64 key mismatch")
	}
}

func TestLoadMasterKeyMissing(t *testing.T) {
	_, err := LoadMasterKey("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	_, err = LoadMasterKey(filepath.Join(t.TempDir(), "missing.key"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadMasterKeyRejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadMasterKey(dir)
	if err == nil {
		t.Fatal("expected error for directory path")
	}
}
