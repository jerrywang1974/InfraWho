package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

const (
	EnvDBURL         = "INFRAWHO_DB_URL"
	EnvMasterKeyFile = "INFRAWHO_MASTER_KEY_FILE"
	EnvListenAddr    = "INFRAWHO_LISTEN_ADDR"

	DefaultDBURL      = "sqlite:///data/infrawho.db"
	DefaultListenAddr = ":8080"
	MasterKeySize     = 32
)

type Config struct {
	DBURL         string
	MasterKeyFile string
	ListenAddr    string
}

// Load reads env. Phase 1: require sqlite: scheme.
func Load() (*Config, error) {
	cfg := &Config{
		DBURL:         getenv(EnvDBURL, DefaultDBURL),
		MasterKeyFile: strings.TrimSpace(os.Getenv(EnvMasterKeyFile)),
		ListenAddr:    getenv(EnvListenAddr, DefaultListenAddr),
	}
	if err := validateDBURL(cfg.DBURL); err != nil {
		return nil, err
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func validateDBURL(dbURL string) error {
	if dbURL == "" {
		return fmt.Errorf("%s is required", EnvDBURL)
	}
	lower := strings.ToLower(dbURL)
	if !strings.HasPrefix(lower, "sqlite:") {
		return fmt.Errorf("%s must use sqlite: scheme in Phase 1 (got %q)", EnvDBURL, dbURL)
	}
	return nil
}

// LoadMasterKey reads a raw 32-byte file or base64 of 32 bytes.
func LoadMasterKey(path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("%s is not set", EnvMasterKeyFile)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read master key file %q: %w", path, err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("master key path %q is not a regular file (Docker may have created a directory for a missing bind mount — remove it and create the key file first)", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read master key file %q: %w", path, err)
	}
	key, err := parseMasterKey(data)
	if err != nil {
		return nil, fmt.Errorf("master key file %q: %w", path, err)
	}
	return key, nil
}

func parseMasterKey(data []byte) ([]byte, error) {
	// Prefer exact raw length before any text trimming (binary keys may contain 0x09/0x0a/0x20).
	if len(data) == MasterKeySize {
		out := make([]byte, MasterKeySize)
		copy(out, data)
		return out, nil
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, fmt.Errorf("empty key material")
	}
	decoded, err := decodeBase64(trimmed)
	if err != nil {
		return nil, fmt.Errorf("expected %d raw bytes or base64 of %d bytes", MasterKeySize, MasterKeySize)
	}
	if len(decoded) != MasterKeySize {
		return nil, fmt.Errorf("decoded key length %d, want %d", len(decoded), MasterKeySize)
	}
	return decoded, nil
}

func decodeBase64(s string) ([]byte, error) {
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.URLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawURLEncoding.DecodeString(s)
}
