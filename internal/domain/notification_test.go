package domain_test

import (
	"testing"
	"time"
	"github.com/sgnraft/insider-case/internal/domain"
)

func TestRetryDelay(t *testing.T) {
	tests := []struct{ attempt int; expected time.Duration }{
		{0, 5 * time.Second},
		{1, 15 * time.Second},
		{2, 60 * time.Second},
		{3, 5 * time.Minute},
		{4, 30 * time.Minute},
	}
	for _, tt := range tests {
		if got := domain.RetryDelay(tt.attempt); got != tt.expected {
			t.Errorf("RetryDelay(%d) = %v, want %v", tt.attempt, got, tt.expected)
		}
	}
}

func TestRetryDelayCAP(t *testing.T) {
	if got := domain.RetryDelay(100); got != 30*time.Minute {
		t.Errorf("expected capped at 30m, got %v", got)
	}
}

func TestPriorityScoreOrdering(t *testing.T) {
	h := domain.PriorityScore(domain.PriorityHigh)
	n := domain.PriorityScore(domain.PriorityNormal)
	l := domain.PriorityScore(domain.PriorityLow)
	if h >= n || n >= l {
		t.Errorf("expected high < normal < low, got %v %v %v", h, n, l)
	}
}

func TestPriorityScoreDefault(t *testing.T) {
	if domain.PriorityScore("unknown") != domain.PriorityScore(domain.PriorityNormal) {
		t.Error("unknown priority should equal normal score")
	}
}
