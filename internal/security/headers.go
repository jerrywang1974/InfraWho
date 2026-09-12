package security

import "net/http"

// DefaultCSP is a Phase 1 policy for same-origin API + future SPA from this origin.
// style-src stays 'self' (Vite/file CSS); add hashes/nonces or 'unsafe-inline' only
// if an embed/UI PR demonstrates a need for inline styles.
const DefaultCSP = "default-src 'self'; script-src 'self'; style-src 'self'; " +
	"img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; " +
	"base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

// Headers wraps next with baseline browser security headers (CSP, framing, MIME sniffing).
// HSTS is intentionally omitted — set it on the TLS-terminating reverse proxy.
func Headers(next http.Handler) http.Handler {
	return HeadersWithCSP(next, DefaultCSP)
}

// HeadersWithCSP is like Headers but with a custom Content-Security-Policy value.
func HeadersWithCSP(next http.Handler, csp string) http.Handler {
	if csp == "" {
		csp = DefaultCSP
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}
