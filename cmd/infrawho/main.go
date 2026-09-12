package main

import (
	"database/sql"
	"log"
	"net/http"

	"github.com/jerrywang1974/InfraWho/internal/config"
	"github.com/jerrywang1974/InfraWho/internal/db"
)

func main() {
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
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/readyz", handleReadyz(cfg, sqlDB))

	log.Printf("infrawho listening on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, mux); err != nil {
		log.Fatalf("listen: %v", err)
	}
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
