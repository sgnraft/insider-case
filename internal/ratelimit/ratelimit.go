package ratelimit

import (
	"context"
	"sync"
	"time"
)

// Limiter is a token-bucket rate limiter for a single resource.
type Limiter struct {
	mu       sync.Mutex
	tokens   float64
	maxTok   float64
	refillPS float64
	lastTick time.Time
}

// NewLimiter creates a limiter with ratePerSec tokens/second (burst = rate).
func NewLimiter(ratePerSec float64) *Limiter {
	return &Limiter{
		tokens:   ratePerSec,
		maxTok:   ratePerSec,
		refillPS: ratePerSec,
		lastTick: time.Now(),
	}
}

// Wait blocks until a token is available or ctx is done.
func (l *Limiter) Wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		l.refill()
		if l.tokens >= 1 {
			l.tokens--
			l.mu.Unlock()
			return nil
		}
		need := (1 - l.tokens) / l.refillPS
		l.mu.Unlock()

		waitDur := time.Duration(need * float64(time.Second))
		if waitDur < time.Millisecond {
			waitDur = time.Millisecond
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitDur):
		}
	}
}

func (l *Limiter) refill() {
	now := time.Now()
	elapsed := now.Sub(l.lastTick).Seconds()
	l.lastTick = now
	l.tokens += elapsed * l.refillPS
	if l.tokens > l.maxTok {
		l.tokens = l.maxTok
	}
}

// Group manages per-key Limiters.
type Group struct {
	mu       sync.RWMutex
	limiters map[string]*Limiter
	rate     float64
}

// NewGroup creates a Group where each key gets its own Limiter at ratePerSec.
func NewGroup(ratePerSec float64) *Group {
	return &Group{limiters: make(map[string]*Limiter), rate: ratePerSec}
}

// Get returns the Limiter for key, creating one if necessary.
func (g *Group) Get(key string) *Limiter {
	g.mu.RLock()
	l, ok := g.limiters[key]
	g.mu.RUnlock()
	if ok {
		return l
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if l, ok = g.limiters[key]; ok {
		return l
	}
	l = NewLimiter(g.rate)
	g.limiters[key] = l
	return l
}
