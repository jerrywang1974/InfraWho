package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

const (
	EnvDBURL          = "INFRAWHO_DB_URL"
	EnvMasterKeyFile  = "INFRAWHO_MASTER_KEY_FILE"
	EnvKeyVersion     = "INFRAWHO_KEY_VERSION"
	EnvListenAddr     = "INFRAWHO_LISTEN_ADDR"
	EnvCookieSecure   = "INFRAWHO_COOKIE_SECURE"
	EnvTrustedOrigins = "INFRAWHO_TRUSTED_ORIGINS"
	EnvTrustProxy     = "INFRAWHO_TRUST_PROXY"
	// EnvFeatureExportSecrets enables plaintext secret export (K18). Default false.
	EnvFeatureExportSecrets = "FEATURE_EXPORT_SECRETS"
	EnvFeatureMetrics       = "INFRAWHO_FEATURE_METRICS"

	DefaultDBURL      = "sqlite:///data/infrawho.db"
	DefaultListenAddr = ":8080"
	DefaultKeyVersion = "kek-v1"
	MasterKeySize     = 32
)

type Config struct {
	DBURL         string
	MasterKeyFile string
	// KeyVersion is stamped on new secret_payloads (must match the loaded KEK identity).
	KeyVersion     string
	ListenAddr     string
	CookieSecure   bool
	TrustedOrigins []string
	// TrustProxy enables X-Forwarded-Host / X-Forwarded-For from an upstream reverse proxy.
	TrustProxy bool
	// FeatureExportSecrets gates POST /export with include_secrets:true (default false).
	FeatureExportSecrets bool
	FeatureMetrics       bool
}

// Load reads env. Phase 1: require sqlite: scheme.
func Load() (*Config, error) {
	cfg := &Config{
		DBURL:                getenv(EnvDBURL, DefaultDBURL),
		MasterKeyFile:        strings.TrimSpace(os.Getenv(EnvMasterKeyFile)),
		KeyVersion:           getenv(EnvKeyVersion, DefaultKeyVersion),
		ListenAddr:           getenv(EnvListenAddr, DefaultListenAddr),
		CookieSecure:         getenvBool(EnvCookieSecure, true),
		TrustedOrigins:       splitCSV(os.Getenv(EnvTrustedOrigins)),
		TrustProxy:           getenvBool(EnvTrustProxy, false),
		FeatureExportSecrets: getenvBool(EnvFeatureExportSecrets, false),
		FeatureMetrics:       getenvBool(EnvFeatureMetrics, false),
	}
	if err := validateDBURL(cfg.DBURL); err != nil {
		return nil, err
	}
	return cfg, nil
}

func getenvBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
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
