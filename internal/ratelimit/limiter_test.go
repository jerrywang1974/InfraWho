package ratelimit

import (
	"testing"
	"time"
)

func TestLimiterAllow(t *testing.T) {
	l := New()
	for i := 0; i < 3; i++ {
		if !l.Allow("k", 3, time.Minute) {
			t.Fatalf("allow #%d failed", i+1)
		}
	}
	if l.Allow("k", 3, time.Minute) {
		t.Fatal("expected rate limited")
	}
	if !l.Allow("other", 3, time.Minute) {
		t.Fatal("other key should be independent")
	}
}

func TestLockout(t *testing.T) {
	lo := NewLockout()
	for i := 0; i < 9; i++ {
		if lo.Fail("u", 10, time.Minute) {
			t.Fatalf("unexpected lock at failure %d", i+1)
		}
	}
	if !lo.Fail("u", 10, time.Minute) {
		t.Fatal("expected lock on 10th failure")
	}
	locked, _ := lo.Locked("u")
	if !locked {
		t.Fatal("should be locked")
	}
	lo.Success("u")
	locked, _ = lo.Locked("u")
	if locked {
		t.Fatal("success should clear lock")
	}
}
