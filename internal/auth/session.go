package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"time"
)

const (
	SessionCookieName = "infrawho_session"

	IdleTTL     = 30 * time.Minute
	AbsoluteTTL = 12 * time.Hour
	StepUpTTL   = 5 * time.Minute

	tokenBytes = 32
)

// Session is a server-side browser session.
type Session struct {
	ID                string
	UserID            string
	TokenHash         string
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
	StepUpExpiresAt   *time.Time
	CreatedAt         time.Time
}

// User is an authenticated account row.
type User struct {
	ID           string
	Username     string
	DisplayName  string
	Role         Role
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// newSessionToken returns the raw cookie value and its hex SHA-256 hash for storage.
func newSessionToken() (raw string, hash string, err error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(raw))
	return raw, hex.EncodeToString(sum[:]), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func setSessionCookie(w http.ResponseWriter, raw string, secure bool, absoluteExpiry time.Time) {
	maxAge := int(time.Until(absoluteExpiry).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    raw,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   maxAge,
		Expires:  absoluteExpiry,
	})
}

func clearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func readSessionCookie(r *http.Request) (string, error) {
	c, err := r.Cookie(SessionCookieName)
	if err != nil {
		return "", err
	}
	return c.Value, nil
}

func (s *Session) StepUpActive(now time.Time) bool {
	return s.StepUpExpiresAt != nil && now.Before(*s.StepUpExpiresAt)
}

func (s *Session) Valid(now time.Time) bool {
	return now.Before(s.IdleExpiresAt) && now.Before(s.AbsoluteExpiresAt)
}
