package metrics

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

// Process-local counters (Prometheus text names). Cheap to increment always;
// expose via Handler only when FEATURE_METRICS / INFRAWHO_FEATURE_METRICS is on.
var (
	loginFailures atomic.Uint64
	revealTotal   atomic.Uint64
	rateLimited   atomic.Uint64
)

func IncLoginFailures() { loginFailures.Add(1) }
func IncReveal()        { revealTotal.Add(1) }
func IncRateLimited()   { rateLimited.Add(1) }

// Snapshot returns current counter values (for tests).
func Snapshot() (loginFailuresN, revealN, rateLimitedN uint64) {
	return loginFailures.Load(), revealTotal.Load(), rateLimited.Load()
}

// ResetForTest zeroes counters; not for production use.
func ResetForTest() {
	loginFailures.Store(0)
	revealTotal.Store(0)
	rateLimited.Store(0)
}

// Handler serves Prometheus text exposition for the three Phase 1 counters.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		lf, rv, rl := Snapshot()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodHead {
			return
		}
		_, _ = fmt.Fprintf(w, "# HELP login_failures_total Login attempts that failed credential checks.\n")
		_, _ = fmt.Fprintf(w, "# TYPE login_failures_total counter\n")
		_, _ = fmt.Fprintf(w, "login_failures_total %d\n", lf)
		_, _ = fmt.Fprintf(w, "# HELP reveal_total Successful secret reveal operations.\n")
		_, _ = fmt.Fprintf(w, "# TYPE reveal_total counter\n")
		_, _ = fmt.Fprintf(w, "reveal_total %d\n", rv)
		_, _ = fmt.Fprintf(w, "# HELP rate_limited_total Requests rejected by rate limits.\n")
		_, _ = fmt.Fprintf(w, "# TYPE rate_limited_total counter\n")
		_, _ = fmt.Fprintf(w, "rate_limited_total %d\n", rl)
	})
}
