package handlers_test

import (
	"testing"
	"github.com/sgnraft/insider-case/internal/domain"
)

func TestRetryDelay(t *testing.T) {
	tests := []struct {
		attempt int
		want    float64
	}{
		{0, 5}, {1, 15}, {2, 60}, {3, 300}, {4, 1800}, {5, 1800},
	}
	for _, tt := range tests {
		got := domain.RetryDelay(tt.attempt)
		if got.Seconds() < tt.want {
			t.Errorf("RetryDelay(%d) = %v, want >= %vs", tt.attempt, got, tt.want)
		}
	}
}

func TestPriorityScore(t *testing.T) {
	h := domain.PriorityScore(domain.PriorityHigh)
	n := domain.PriorityScore(domain.PriorityNormal)
	l := domain.PriorityScore(domain.PriorityLow)
	if h >= n {
		t.Errorf("high (%v) must be lower than normal (%v)", h, n)
	}
	if n >= l {
		t.Errorf("normal (%v) must be lower than low (%v)", n, l)
	}
}
