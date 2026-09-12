package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jerrywang1974/InfraWho/internal/config"
	"github.com/jerrywang1974/InfraWho/internal/db"
)

func TestDispatchRejectsUnknownCommand(t *testing.T) {
	err := dispatch([]string{"rewrap"})
	if err == nil {
		t.Fatal("expected error for unknown root command")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("err = %v", err)
	}

	err = dispatch([]string{"key", "rewrap"})
	if err == nil {
		t.Fatal("expected error for typo root command")
	}
}

func TestReadyzRequiresDBThenKEK(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "master.key")
	key := make([]byte, config.MasterKeySize)
	for i := range key {
		key[i] = byte(i + 1)
	}
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		t.Fatal(err)
	}

	sqlDB, err := db.Open("sqlite::memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer sqlDB.Close()

	cfg := &config.Config{MasterKeyFile: keyPath}

	t.Run("ok", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		handleReadyz(cfg, sqlDB)(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
		}
		if body := rec.Body.String(); body != "ok\n" {
			t.Fatalf("body = %q", body)
		}
	})

	t.Run("db unavailable before kek", func(t *testing.T) {
		_ = sqlDB.Close()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		handleReadyz(cfg, sqlDB)(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
		body, _ := io.ReadAll(rec.Body)
		if !strings.Contains(string(body), "database unavailable") {
			t.Fatalf("body = %q, want database unavailable (checked before KEK)", body)
		}
		if strings.Contains(string(body), "master key") {
			t.Fatalf("body = %q, should not reach KEK check when DB ping fails", body)
		}
	})
}

func TestReadyzKEKUnavailable(t *testing.T) {
	sqlDB, err := db.Open("sqlite::memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer sqlDB.Close()

	cfg := &config.Config{MasterKeyFile: filepath.Join(t.TempDir(), "missing.key")}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	handleReadyz(cfg, sqlDB)(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "master key unavailable") {
		t.Fatalf("body = %q", body)
	}
}
