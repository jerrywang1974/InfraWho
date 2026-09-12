package auth

import (
	"database/sql"
	"net/http"
	"time"
)

// Middleware loads the session cookie, rejects expired sessions, and refreshes idle TTL.
func (h *Handler) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := readSessionCookie(r)
		if err != nil || raw == "" {
			next.ServeHTTP(w, r)
			return
		}
		sess, err := h.store.FindSessionByToken(raw)
		if err != nil {
			if err != sql.ErrNoRows {
				writeError(w, http.StatusInternalServerError, "internal_error", "Session lookup failed")
				return
			}
			clearSessionCookie(w, h.cookieSecure)
			next.ServeHTTP(w, r)
			return
		}
		now := time.Now().UTC()
		if !sess.Valid(now) {
			_ = h.store.DeleteSession(sess.ID)
			clearSessionCookie(w, h.cookieSecure)
			next.ServeHTTP(w, r)
			return
		}
		user, err := h.store.FindUserByID(sess.UserID)
		if err != nil {
			_ = h.store.DeleteSession(sess.ID)
			clearSessionCookie(w, h.cookieSecure)
			next.ServeHTTP(w, r)
			return
		}
		newIdle := now.Add(IdleTTL)
		if newIdle.After(sess.AbsoluteExpiresAt) {
			newIdle = sess.AbsoluteExpiresAt
		}
		if err := h.store.TouchSession(sess.ID, newIdle); err == nil {
			sess.IdleExpiresAt = newIdle
		}
		ctx := withUser(r.Context(), user)
		ctx = withSession(ctx, sess)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAuth rejects unauthenticated requests.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := UserFromContext(r.Context()); !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole rejects authenticated users below min role.
func RequireRole(min Role, next http.Handler) http.Handler {
	return RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := UserFromContext(r.Context())
		if !u.Role.HasAtLeast(min) {
			writeError(w, http.StatusForbidden, "forbidden", "Insufficient role")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// RequireStepUp rejects when step-up is missing or expired.
func RequireStepUp(next http.Handler) http.Handler {
	return RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := SessionFromContext(r.Context())
		if !ok || !sess.StepUpActive(time.Now().UTC()) {
			writeError(w, http.StatusForbidden, "step_up_required", "Step-up authentication required")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// RequireOrigin rejects state-changing requests that fail Origin/Referer checks.
func (h *Handler) RequireOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !CheckOrigin(r, h.trustedOrigins) {
			writeError(w, http.StatusForbidden, "forbidden", "Origin check failed")
			return
		}
		next.ServeHTTP(w, r)
	})
}
