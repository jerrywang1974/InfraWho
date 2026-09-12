package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	_ "modernc.org/sqlite"
)

var memSeq atomic.Uint64

// Open opens SQLite from INFRAWHO_DB_URL, enables foreign keys, and runs migrations.
func Open(dbURL string) (*sql.DB, error) {
	path, err := SQLitePath(dbURL)
	if err != nil {
		return nil, err
	}

	if path != ":memory:" {
		dir := filepath.Dir(path)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("create database directory %q: %w", dir, err)
			}
		}
	}

	dsn := sqliteDSN(path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// One pool connection serializes all SQLite access (avoids SQLITE_BUSY across goroutines).
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := pingAndPragma(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := Migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func sqliteDSN(path string) string {
	const pragmas = "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	if path == ":memory:" {
		// Unique name so concurrent Open(":memory:") calls do not share state.
		return fmt.Sprintf("file:infrawho-mem-%d?mode=memory&cache=shared&%s", memSeq.Add(1), pragmas)
	}
	// Absolute paths need an extra leading slash in file: URIs on Unix.
	if strings.HasPrefix(path, "/") {
		return "file://" + path + "?" + pragmas
	}
	return "file:" + path + "?" + pragmas
}

func pingAndPragma(db *sql.DB) error {
	if err := db.Ping(); err != nil {
		return fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("pragma foreign_keys: %w", err)
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		return fmt.Errorf("pragma busy_timeout: %w", err)
	}
	// Best-effort; ignore failures on :memory: / unsupported FS.
	_, _ = db.Exec(`PRAGMA journal_mode = WAL`)
	return nil
}
