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

func TestAllowReportFirstDeny(t *testing.T) {
	l := New()
	for i := 0; i < 2; i++ {
		ok, need := l.AllowReport("k", 2, time.Minute)
		if !ok || need {
			t.Fatalf("allow #%d: ok=%v need=%v", i+1, ok, need)
		}
	}
	ok, need := l.AllowReport("k", 2, time.Minute)
	if ok || !need {
		t.Fatalf("first deny: ok=%v need=%v", ok, need)
	}
	// Without ConfirmDeny, subsequent denials still need audit.
	ok, need = l.AllowReport("k", 2, time.Minute)
	if ok || !need {
		t.Fatalf("unconfirmed deny: ok=%v need=%v", ok, need)
	}
	l.ConfirmDeny("k")
	ok, need = l.AllowReport("k", 2, time.Minute)
	if ok || need {
		t.Fatalf("after confirm: ok=%v need=%v", ok, need)
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
