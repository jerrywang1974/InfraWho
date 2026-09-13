// Package webui serves a built SPA from a filesystem directory for single-box
// lab/verify images. Production design still prefers TLS at an org reverse
// proxy with the API as a separate origin or path; leave INFRAWHO_WEB_ROOT
// unset there so this handler is not mounted.
package webui

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// WithSPAFallback routes /api/* (and other already-registered API paths on
// next) to next, and serves GET/HEAD of everything else from rootDir.
// Missing files fall back to index.html so React Router client routes work.
//
// rootDir must contain index.html (typically Vite's web/dist). If rootDir is
// empty or unreadable, next is returned unchanged.
func WithSPAFallback(rootDir string, next http.Handler) (http.Handler, error) {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		return next, nil
	}
	abs, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, os.ErrNotExist
	}
	index := filepath.Join(abs, "index.html")
	if _, err := os.Stat(index); err != nil {
		return nil, err
	}

	fileServer := http.FileServer(http.Dir(abs))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// API and ops endpoints always go to the Go handlers.
		if isAPIPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		serveSPA(w, r, abs, index, fileServer)
	}), nil
}

func isAPIPath(p string) bool {
	p = path.Clean("/" + p)
	switch {
	case strings.HasPrefix(p, "/api/"):
		return true
	case p == "/healthz" || p == "/readyz" || p == "/metrics":
		return true
	default:
		return false
	}
}

func serveSPA(w http.ResponseWriter, r *http.Request, root, index string, fileServer http.Handler) {
	rel := path.Clean("/" + r.URL.Path)
	if rel == "/" {
		http.ServeFile(w, r, index)
		return
	}
	full := filepath.Join(root, filepath.FromSlash(rel))
	// Containment: resolved path must stay under root.
	if !strings.HasPrefix(full, root+string(os.PathSeparator)) && full != root {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	fi, err := os.Stat(full)
	if err == nil && !fi.IsDir() {
		fileServer.ServeHTTP(w, r)
		return
	}
	// Client-side route or missing asset → SPA shell.
	http.ServeFile(w, r, index)
}
