package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/jerrywang1974/InfraWho/internal/accounts"
	"github.com/jerrywang1974/InfraWho/internal/assets"
	"github.com/jerrywang1974/InfraWho/internal/audit"
	"github.com/jerrywang1974/InfraWho/internal/auth"
	"github.com/jerrywang1974/InfraWho/internal/backup"
	"github.com/jerrywang1974/InfraWho/internal/config"
	"github.com/jerrywang1974/InfraWho/internal/db"
	"github.com/jerrywang1974/InfraWho/internal/jobs"
	"github.com/jerrywang1974/InfraWho/internal/metrics"
	"github.com/jerrywang1974/InfraWho/internal/notes"
	"github.com/jerrywang1974/InfraWho/internal/ratelimit"
	"github.com/jerrywang1974/InfraWho/internal/search"
	"github.com/jerrywang1974/InfraWho/internal/security"
	"github.com/jerrywang1974/InfraWho/internal/webui"
)

func main() {
	if err := dispatch(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func dispatch(args []string) error {
	if len(args) == 0 {
		runServe()
		return nil
	}
	switch args[0] {
	case "keys":
		if err := runKeys(args[1:]); err != nil {
			return fmt.Errorf("keys: %w", err)
		}
		return nil
	case "serve":
		runServe()
		return nil
	case "help", "-h", "--help":
		printRootUsage()
		return nil
	default:
		printRootUsage()
		return fmt.Errorf("unknown command %q (use bare infrawho or 'serve' to start HTTP)", args[0])
	}
}

func printRootUsage() {
	fmt.Print(`infrawho — InfraWho server and maintenance CLI

Usage:
  infrawho              Start the HTTP server (default)
  infrawho serve        Start the HTTP server
  infrawho keys rewrap  Rewrap secret DEKs under a new KEK (stop HTTP first)

Environment:
  INFRAWHO_DB_URL            SQLite URL (default sqlite:///data/infrawho.db)
  INFRAWHO_MASTER_KEY_FILE   Path to current KEK file (server readiness)
  INFRAWHO_LISTEN_ADDR       HTTP listen address (default :8080)
  INFRAWHO_WEB_ROOT          Optional SPA dist dir (lab/verify single-box only)

See: infrawho keys --help for the KEK rotation runbook.
`)
}

func runServe() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	sqlDB, err := db.Open(cfg.DBURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer sqlDB.Close()

	mux := http.NewServeMux()
	registerRoutes(mux, cfg, sqlDB)

	handler := security.Headers(mux)
	log.Printf("infrawho listening on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, handler); err != nil {
		log.Fatalf("listen: %v", err)
	}
}

func registerRoutes(mux *http.ServeMux, cfg *config.Config, sqlDB *sql.DB) {
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/readyz", handleReadyz(cfg, sqlDB))
	if cfg.FeatureMetrics {
		mux.Handle("/metrics", metrics.Handler())
	}

	store := auth.NewStore(sqlDB)
	authHandler := auth.NewHandler(store, ratelimit.New(), auth.Options{
		CookieSecure:   cfg.CookieSecure,
		TrustProxy:     cfg.TrustProxy,
		TrustedOrigins: cfg.TrustedOrigins,
		MasterKeyFile:  cfg.MasterKeyFile,
	})

	api := http.NewServeMux()
	api.HandleFunc("/api/v1/auth/login", authHandler.Login)
	api.HandleFunc("/api/v1/auth/logout", authHandler.Logout)
	api.HandleFunc("/api/v1/auth/me", authHandler.Me)
	api.HandleFunc("/api/v1/auth/step-up", authHandler.StepUp)
	api.HandleFunc("/api/v1/setup/status", authHandler.SetupStatus)
	api.HandleFunc("/api/v1/setup/bootstrap", authHandler.Bootstrap)
	api.HandleFunc("/api/v1/setup/acknowledge", authHandler.Acknowledge)

	assetHandler := assets.NewHandler(assets.NewStore(sqlDB), assets.Options{TrustProxy: cfg.TrustProxy})
	assetHandler.Register(api, authHandler.RequireOrigin)

	auditStore := audit.NewStore(sqlDB)
	audit.NewHandler(auditStore).Register(api)

	masterKeyFile := cfg.MasterKeyFile
	accountStore := accounts.NewStore(sqlDB, accounts.StoreOptions{
		KeyVersion: cfg.KeyVersion,
		LoadKEK: func() ([]byte, error) {
			return config.LoadMasterKey(masterKeyFile)
		},
	})
	accountHandler := accounts.NewHandler(accountStore, auditStore, accounts.Options{
		TrustProxy: cfg.TrustProxy,
		Limiter:    authHandler.Limiter(),
	})
	accountHandler.Register(api, authHandler.RequireOrigin)

	jobs.NewHandler(jobs.NewStore(sqlDB)).Register(api, authHandler.RequireOrigin)
	notes.NewHandler(notes.NewStore(sqlDB)).Register(api, authHandler.RequireOrigin)

	backupStore := backup.NewStore(sqlDB, backup.StoreOptions{
		KeyVersion: cfg.KeyVersion,
		LoadKEK: func() ([]byte, error) {
			return config.LoadMasterKey(masterKeyFile)
		},
	})
	backup.NewHandler(backupStore, auditStore, backup.Options{
		TrustProxy:           cfg.TrustProxy,
		FeatureExportSecrets: cfg.FeatureExportSecrets,
		Limiter:              authHandler.Limiter(),
	}).Register(api, authHandler.RequireOrigin)
	search.NewHandler(search.NewStore(sqlDB)).Register(api, authHandler.RequireOrigin)

	// Default: API-only (production behind org reverse proxy). When
	// INFRAWHO_WEB_ROOT is set (Docker verify/lab image), serve the built SPA
	// for non-/api paths so one container can be smoke-tested in a browser.
	var inner http.Handler = api
	if cfg.WebRoot != "" {
		h, err := webui.WithSPAFallback(cfg.WebRoot, api)
		if err != nil {
			log.Fatalf("webui: INFRAWHO_WEB_ROOT=%q: %v", cfg.WebRoot, err)
		}
		inner = h
		log.Printf("serving SPA from %s (lab/verify; unset WEB_ROOT for API-only prod)", cfg.WebRoot)
	}
	mux.Handle("/", authHandler.Middleware(inner))
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func handleReadyz(cfg *config.Config, sqlDB *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := sqlDB.Ping(); err != nil {
			http.Error(w, "not ready: database unavailable\n", http.StatusServiceUnavailable)
			return
		}
		if _, err := config.LoadMasterKey(cfg.MasterKeyFile); err != nil {
			http.Error(w, "not ready: master key unavailable\n", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	}
}
