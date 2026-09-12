package db

import (
	"fmt"
	"net/url"
	"strings"
)

// SQLitePath extracts the filesystem path (or ":memory:") from an INFRAWHO_DB_URL.
// Accepts sqlite:///abs/path, sqlite:relative, sqlite:file:... forms.
func SQLitePath(dbURL string) (string, error) {
	if dbURL == "" {
		return "", fmt.Errorf("empty database URL")
	}
	if !strings.HasPrefix(strings.ToLower(dbURL), "sqlite:") {
		return "", fmt.Errorf("database URL must use sqlite: scheme (got %q)", dbURL)
	}
	rest := dbURL[len("sqlite:"):]
	if rest == "" {
		return "", fmt.Errorf("database URL %q has empty path", dbURL)
	}

	lower := strings.ToLower(rest)
	switch {
	case rest == ":memory:" || lower == "file::memory:" || strings.HasPrefix(lower, "file::memory:?"):
		return ":memory:", nil
	case strings.HasPrefix(lower, "file:"):
		return parseFilePath(rest[len("file:"):])
	}

	u, err := url.Parse(dbURL)
	if err != nil {
		return "", fmt.Errorf("parse database URL: %w", err)
	}
	if u.Opaque != "" {
		return stripQuery(u.Opaque), nil
	}
	// Two-slash forms (sqlite://data/x.db) put a path segment in Host and open the wrong file.
	if u.Host != "" {
		return "", fmt.Errorf("database URL %q must not include a host; use sqlite:///abs/path or sqlite:relative.db", dbURL)
	}
	p := u.Path
	if p == "" || p == "/" {
		return "", fmt.Errorf("database URL %q has empty path", dbURL)
	}
	// url.Parse("sqlite:////tmp/x") yields Path "//tmp/x"; normalize to "/tmp/x".
	if strings.HasPrefix(p, "//") {
		p = p[1:]
	}
	return p, nil
}

func parseFilePath(rest string) (string, error) {
	lower := strings.ToLower(rest)
	if rest == ":memory:" || strings.HasPrefix(lower, ":memory:") {
		return ":memory:", nil
	}
	if strings.HasPrefix(rest, "///") {
		return stripQuery(rest[2:]), nil
	}
	if strings.HasPrefix(rest, "//") {
		u, err := url.Parse("file:" + rest)
		if err != nil {
			return "", fmt.Errorf("parse file: database URL: %w", err)
		}
		if u.Host != "" {
			return "", fmt.Errorf("file: database URL must not include a host; use file:///abs/path or file:relative.db")
		}
		p := u.Path
		if p == "" || p == "/" {
			return "", fmt.Errorf("file: database URL has empty path")
		}
		if strings.HasPrefix(p, "//") {
			p = p[1:]
		}
		return p, nil
	}
	return stripQuery(rest), nil
}

func stripQuery(p string) string {
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		return p[:i]
	}
	return p
}
