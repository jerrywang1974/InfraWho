package auth

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jerrywang1974/InfraWho/internal/config"
	"github.com/jerrywang1974/InfraWho/internal/ratelimit"
)

const (
	loginIPLimit       = 10
	loginUserLimit     = 5
	loginWindow        = time.Minute
	lockoutThreshold   = 10
	lockoutDuration    = 15 * time.Minute
	stepUpSessionLimit = 10
	stepUpWindow       = time.Minute
	RevealSessionLimit = 30 // exported for accounts PR
	RevealWindow       = time.Minute
	minPasswordLen     = 8
)

// Handler serves auth and setup HTTP endpoints.
type Handler struct {
	store          *Store
	limiter        *ratelimit.Limiter
	lockout        *ratelimit.Lockout
	cookieSecure   bool
	trustedOrigins []string
	masterKeyFile  string
	masterKeyReady func() bool
}

// Options configures the auth handler.
type Options struct {
	CookieSecure   bool
	TrustedOrigins []string
	MasterKeyFile  string
	// MasterKeyReady overrides default LoadMasterKey check when non-nil.
	MasterKeyReady func() bool
}

func NewHandler(store *Store, limiter *ratelimit.Limiter, opts Options) *Handler {
	h := &Handler{
		store:          store,
		limiter:        limiter,
		lockout:        ratelimit.NewLockout(),
		cookieSecure:   opts.CookieSecure,
		trustedOrigins: opts.TrustedOrigins,
		masterKeyFile:  opts.MasterKeyFile,
		masterKeyReady: opts.MasterKeyReady,
	}
	if h.limiter == nil {
		h.limiter = ratelimit.New()
	}
	if h.masterKeyReady == nil {
		h.masterKeyReady = func() bool {
			_, err := config.LoadMasterKey(h.masterKeyFile)
			return err == nil
		}
	}
	return h
}

// Limiter exposes the shared rate limiter (e.g. reveal limits).
func (h *Handler) Limiter() *ratelimit.Limiter {
	return h.limiter
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type meResponse struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	DisplayName  string `json:"display_name"`
	Role         string `json:"role"`
	StepUpActive bool   `json:"step_up_active"`
}

type stepUpRequest struct {
	Password string `json:"password"`
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}
	if !CheckOrigin(r, h.trustedOrigins) {
		writeError(w, http.StatusForbidden, "forbidden", "Origin check failed")
		return
	}

	ip := clientIP(r)
	if !h.limiter.Allow("login:ip:"+ip, loginIPLimit, loginWindow) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Too many login attempts")
		return
	}

	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", "Invalid JSON body")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Username and password are required")
		return
	}

	userKey := strings.ToLower(req.Username)
	if !h.limiter.Allow("login:user:"+userKey, loginUserLimit, loginWindow) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Too many login attempts")
		return
	}
	if locked, _ := h.lockout.Locked(userKey); locked {
		_ = h.store.WriteAudit(nil, "LOGIN_LOCKOUT", "user", nil, "denied", ip, r.UserAgent(), `{"username":`+jsonString(req.Username)+`}`)
		writeError(w, http.StatusTooManyRequests, "lockout", "Account temporarily locked")
		return
	}

	user, err := h.store.FindUserByUsername(req.Username)
	if err != nil {
		if err == sql.ErrNoRows {
			h.failLogin(w, r, nil, req.Username, ip)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Login failed")
		return
	}
	ok, err := VerifyPassword(user.PasswordHash, req.Password)
	if err != nil || !ok {
		uid := user.ID
		h.failLogin(w, r, &uid, req.Username, ip)
		return
	}

	h.lockout.Success(userKey)
	now := time.Now().UTC()
	raw, sess, err := h.store.CreateSession(user.ID, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not create session")
		return
	}
	setSessionCookie(w, raw, h.cookieSecure, sess.AbsoluteExpiresAt)
	uid := user.ID
	_ = h.store.WriteAudit(&uid, "LOGIN", "session", &sess.ID, "success", ip, r.UserAgent(), "{}")
	writeJSON(w, http.StatusOK, meResponse{
		ID:           user.ID,
		Username:     user.Username,
		DisplayName:  user.DisplayName,
		Role:         string(user.Role),
		StepUpActive: false,
	})
}

func (h *Handler) failLogin(w http.ResponseWriter, r *http.Request, actorID *string, username, ip string) {
	userKey := strings.ToLower(username)
	locked := h.lockout.Fail(userKey, lockoutThreshold, lockoutDuration)
	action := "LOGIN_FAILURE"
	outcome := "failure"
	if locked {
		action = "LOGIN_LOCKOUT"
		outcome = "denied"
	}
	_ = h.store.WriteAudit(actorID, action, "user", actorID, outcome, ip, r.UserAgent(), `{"username":`+jsonString(username)+`}`)
	if locked {
		writeError(w, http.StatusTooManyRequests, "lockout", "Account temporarily locked")
		return
	}
	writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid username or password")
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}
	if !CheckOrigin(r, h.trustedOrigins) {
		writeError(w, http.StatusForbidden, "forbidden", "Origin check failed")
		return
	}
	ip := clientIP(r)
	if sess, ok := SessionFromContext(r.Context()); ok {
		_ = h.store.DeleteSession(sess.ID)
		if u, ok := UserFromContext(r.Context()); ok {
			uid := u.ID
			sid := sess.ID
			_ = h.store.WriteAudit(&uid, "LOGOUT", "session", &sid, "success", ip, r.UserAgent(), "{}")
		}
	} else if raw, err := readSessionCookie(r); err == nil && raw != "" {
		if sess, err := h.store.FindSessionByToken(raw); err == nil {
			_ = h.store.DeleteSession(sess.ID)
		}
	}
	clearSessionCookie(w, h.cookieSecure)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}
	u, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}
	sess, _ := SessionFromContext(r.Context())
	active := sess != nil && sess.StepUpActive(time.Now().UTC())
	writeJSON(w, http.StatusOK, meResponse{
		ID:           u.ID,
		Username:     u.Username,
		DisplayName:  u.DisplayName,
		Role:         string(u.Role),
		StepUpActive: active,
	})
}

func (h *Handler) StepUp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}
	if !CheckOrigin(r, h.trustedOrigins) {
		writeError(w, http.StatusForbidden, "forbidden", "Origin check failed")
		return
	}
	u, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}
	sess, ok := SessionFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}
	if !h.limiter.Allow("stepup:session:"+sess.ID, stepUpSessionLimit, stepUpWindow) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Too many step-up attempts")
		return
	}
	var req stepUpRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", "Invalid JSON body")
		return
	}
	if req.Password == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Password is required")
		return
	}
	okHash, err := VerifyPassword(u.PasswordHash, req.Password)
	if err != nil || !okHash {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid password")
		return
	}
	expires := time.Now().UTC().Add(StepUpTTL)
	if err := h.store.SetStepUp(sess.ID, expires); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not set step-up")
		return
	}
	uid := u.ID
	sid := sess.ID
	_ = h.store.WriteAudit(&uid, "STEP_UP", "session", &sid, "success", clientIP(r), r.UserAgent(), "{}")
	w.WriteHeader(http.StatusNoContent)
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
