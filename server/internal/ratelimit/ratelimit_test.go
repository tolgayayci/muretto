package ratelimit

import (
	"testing"
	"time"
)

func TestTokenBucket(t *testing.T) {
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	l := New(60, time.Minute, 3).WithClock(func() time.Time { return now })
	for i := range 3 {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("request %d within burst denied", i)
		}
	}
	ok, wait := l.Allow("a")
	if ok || wait <= 0 || wait > time.Second {
		t.Fatalf("over burst: ok=%v wait=%v", ok, wait)
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("keys must be independent")
	}
	now = now.Add(time.Second)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("not refilled after 1s")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("refilled too much")
	}
}
