// Package ratelimit is a small in-process token-bucket limiter keyed by string
// (token id, client IP, session). It is per instance: v1 runs one instance.
package ratelimit

import (
	"math"
	"sync"
	"time"
)

type bucket struct {
	tokens float64
	last   time.Time
}

// Limiter allows Burst requests at once and refills at Rate per second.
type Limiter struct {
	mu      sync.Mutex
	rate    float64
	burst   float64
	buckets map[string]*bucket
	now     func() time.Time
}

// maxKeys bounds memory; full (idle) buckets are dropped past it.
const maxKeys = 100000

// New returns a limiter that allows n requests per interval with the given burst.
func New(n int, per time.Duration, burst int) *Limiter {
	return &Limiter{
		rate:    float64(n) / per.Seconds(),
		burst:   float64(max(burst, 1)),
		buckets: map[string]*bucket{},
		now:     time.Now,
	}
}

// WithClock sets the clock (tests).
func (l *Limiter) WithClock(now func() time.Time) *Limiter {
	l.now = now
	return l
}

// Allow takes one token for key. When none is left it returns false and how
// long until one is available.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= maxKeys {
			l.prune(now)
		}
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	if el := now.Sub(b.last).Seconds(); el > 0 {
		b.tokens = math.Min(l.burst, b.tokens+el*l.rate)
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
	return false, wait
}

func (l *Limiter) prune(now time.Time) {
	for k, b := range l.buckets {
		if b.tokens+now.Sub(b.last).Seconds()*l.rate >= l.burst {
			delete(l.buckets, k)
		}
	}
}
