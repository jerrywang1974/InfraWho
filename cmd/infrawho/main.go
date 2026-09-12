package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/jerrywang1974/InfraWho/internal/auth"
	"github.com/jerrywang1974/InfraWho/internal/config"
	"github.com/jerrywang1974/InfraWho/internal/db"
	"github.com/jerrywang1974/InfraWho/internal/ratelimit"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "keys":
			if err := runKeys(os.Args[2:]); err != nil {
				log.Fatalf("keys: %v", err)
			}
			return
		case "serve":
			runServe()
			return
		case "help", "-h", "--help":
			printRootUsage()
			return
		}
	}
	runServe()
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

	log.Printf("infrawho listening on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, mux); err != nil {
		log.Fatalf("listen: %v", err)
	}
}

func registerRoutes(mux *http.ServeMux, cfg *config.Config, sqlDB *sql.DB) {
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/readyz", handleReadyz(cfg, sqlDB))

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

	mux.Handle("/", authHandler.Middleware(api))
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
