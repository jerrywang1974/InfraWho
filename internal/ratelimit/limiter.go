package ratelimit

import (
	"sync"
	"time"
)

// Limiter is a process-local fixed-window counter.
type Limiter struct {
	mu      sync.Mutex
	windows map[string]*window
}

type window struct {
	count  int
	start  time.Time
	denied bool // first-deny already signaled this window
}

func New() *Limiter {
	return &Limiter{windows: make(map[string]*window)}
}

// Allow reports whether key may proceed under max events per window.
func (l *Limiter) Allow(key string, max int, per time.Duration) bool {
	ok, _ := l.AllowReport(key, max, per)
	return ok
}

// AllowReport is like Allow; firstDeny is true only on the first rejection in the current window.
func (l *Limiter) AllowReport(key string, max int, per time.Duration) (allowed, firstDeny bool) {
	if max <= 0 || per <= 0 {
		return true, false
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	w, ok := l.windows[key]
	if !ok || now.Sub(w.start) >= per {
		l.windows[key] = &window{count: 1, start: now}
		return true, false
	}
	if w.count >= max {
		first := !w.denied
		w.denied = true
		return false, first
	}
	w.count++
	return true, false
}

// Lockout tracks consecutive failures and temporary locks per key.
type Lockout struct {
	mu      sync.Mutex
	entries map[string]*lockEntry
}

type lockEntry struct {
	failures    int
	lockedUntil time.Time
}

func NewLockout() *Lockout {
	return &Lockout{entries: make(map[string]*lockEntry)}
}

// Locked reports whether key is currently locked and remaining duration.
func (l *Lockout) Locked(key string) (bool, time.Duration) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok {
		return false, 0
	}
	if now.Before(e.lockedUntil) {
		return true, e.lockedUntil.Sub(now)
	}
	return false, 0
}

// Fail records a failure. When failures reach threshold, locks for lockFor and returns locked=true.
func (l *Lockout) Fail(key string, threshold int, lockFor time.Duration) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok {
		e = &lockEntry{}
		l.entries[key] = e
	}
	if now.Before(e.lockedUntil) {
		return true
	}
	if !e.lockedUntil.IsZero() && !now.Before(e.lockedUntil) {
		e.failures = 0
		e.lockedUntil = time.Time{}
	}
	e.failures++
	if e.failures >= threshold {
		e.lockedUntil = now.Add(lockFor)
		e.failures = 0
		return true
	}
	return false
}

// Success clears failure state for key.
func (l *Lockout) Success(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}
