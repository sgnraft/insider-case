package ratelimit_test

import (
	"context"
	"testing"
	"time"

	"github.com/sgnraft/insider-case/internal/ratelimit"
)

func TestLimiter_Wait_Success(t *testing.T) {
	l := ratelimit.NewLimiter(1000)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := l.Wait(ctx); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLimiter_WaitCancelled(t *testing.T) {
	l := ratelimit.NewLimiter(0.001) // 1 token per 1000s — effectively blocks
	// Burn the initial token
	ctx0 := context.Background()
	l.Wait(ctx0) //nolint:errcheck
	// Now try with a very short deadline
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := l.Wait(ctx); err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestGroup_PerKey(t *testing.T) {
	g := ratelimit.NewGroup(100)
	l1 := g.Get("sms")
	l2 := g.Get("email")
	l3 := g.Get("sms")

	if l1 == l2 {
		t.Error("different keys should have different limiters")
	}
	if l1 != l3 {
		t.Error("same key should return same limiter instance")
	}
}
