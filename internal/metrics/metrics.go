// Package metrics provides a lightweight, self-contained Prometheus-compatible
// metrics store. It exposes a /metrics endpoint in the standard text exposition
// format without depending on github.com/prometheus/client_golang (which has
// unavailable transitive deps in this build environment).
package metrics

import (
	"fmt"
	"io"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics holds all application counters, gauges, and histograms.
type Metrics struct {
	// Counters
	deliveredTotal  map[string]*atomic.Int64 // by channel
	failedTotal     map[string]*atomic.Int64 // by channel
	retryTotal      map[string]*atomic.Int64 // by channel

	// Gauges
	queueDepth map[string]*atomic.Int64 // by priority

	// Histograms (simple bucketed latency)
	latencyMu      sync.Mutex
	latencySamples map[string][]float64 // by channel
}

// New creates and returns a ready-to-use Metrics instance.
func New() *Metrics {
	channels := []string{"sms", "email", "push"}
	m := &Metrics{
		deliveredTotal:  make(map[string]*atomic.Int64),
		failedTotal:     make(map[string]*atomic.Int64),
		retryTotal:      make(map[string]*atomic.Int64),
		queueDepth:      make(map[string]*atomic.Int64),
		latencySamples:  make(map[string][]float64),
	}
	for _, ch := range channels {
		m.deliveredTotal[ch] = &atomic.Int64{}
		m.failedTotal[ch] = &atomic.Int64{}
		m.retryTotal[ch] = &atomic.Int64{}
		m.latencySamples[ch] = []float64{}
	}
	for _, p := range []string{"high", "normal", "low", "dlq"} {
		m.queueDepth[p] = &atomic.Int64{}
	}
	return m
}

// RecordSuccess records a successful delivery.
func (m *Metrics) RecordSuccess(channel string, latency time.Duration) {
	if c, ok := m.deliveredTotal[channel]; ok {
		c.Add(1)
	}
	m.recordLatency(channel, latency.Seconds())
}

// RecordFailure records a failed delivery.
func (m *Metrics) RecordFailure(channel string, latency time.Duration) {
	if c, ok := m.failedTotal[channel]; ok {
		c.Add(1)
	}
	m.recordLatency(channel, latency.Seconds())
}

// RecordRetry records a retry attempt.
func (m *Metrics) RecordRetry(channel string) {
	if c, ok := m.retryTotal[channel]; ok {
		c.Add(1)
	}
}

// SetQueueDepth updates a queue depth gauge.
func (m *Metrics) SetQueueDepth(priority string, depth int64) {
	if g, ok := m.queueDepth[priority]; ok {
		g.Store(depth)
	}
}

func (m *Metrics) recordLatency(channel string, secs float64) {
	m.latencyMu.Lock()
	defer m.latencyMu.Unlock()
	if _, ok := m.latencySamples[channel]; !ok {
		m.latencySamples[channel] = []float64{}
	}
	// Keep last 10 000 samples per channel to bound memory
	samples := m.latencySamples[channel]
	if len(samples) >= 10_000 {
		samples = samples[1:]
	}
	m.latencySamples[channel] = append(samples, secs)
}

// Snapshot returns a human-readable JSON-friendly summary.
func (m *Metrics) Snapshot() map[string]interface{} {
	snap := map[string]interface{}{}
	
	delivered := map[string]int64{}
	failed := map[string]int64{}
	for ch := range m.deliveredTotal {
		delivered[ch] = m.deliveredTotal[ch].Load()
		failed[ch] = m.failedTotal[ch].Load()
	}
	depth := map[string]int64{}
	for p := range m.queueDepth {
		depth[p] = m.queueDepth[p].Load()
	}
	snap["delivered_total"] = delivered
	snap["failed_total"] = failed
	snap["queue_depth"] = depth
	return snap
}

// WritePrometheusText writes Prometheus exposition format to w.
func (m *Metrics) WritePrometheusText(w io.Writer) {
	// Delivered
	fmt.Fprintln(w, "# HELP notifications_delivered_total Total successfully delivered notifications")
	fmt.Fprintln(w, "# TYPE notifications_delivered_total counter")
	for ch, c := range m.deliveredTotal {
		fmt.Fprintf(w, "notifications_delivered_total{channel=%q} %d\n", ch, c.Load())
	}
	// Failed
	fmt.Fprintln(w, "# HELP notifications_failed_total Total failed notifications")
	fmt.Fprintln(w, "# TYPE notifications_failed_total counter")
	for ch, c := range m.failedTotal {
		fmt.Fprintf(w, "notifications_failed_total{channel=%q} %d\n", ch, c.Load())
	}
	// Retries
	fmt.Fprintln(w, "# HELP notifications_retry_total Total retry attempts")
	fmt.Fprintln(w, "# TYPE notifications_retry_total counter")
	for ch, c := range m.retryTotal {
		fmt.Fprintf(w, "notifications_retry_total{channel=%q} %d\n", ch, c.Load())
	}
	// Queue depth
	fmt.Fprintln(w, "# HELP notification_queue_depth Current queue depth by priority")
	fmt.Fprintln(w, "# TYPE notification_queue_depth gauge")
	for p, g := range m.queueDepth {
		fmt.Fprintf(w, "notification_queue_depth{priority=%q} %d\n", p, g.Load())
	}
	// Latency histogram
	fmt.Fprintln(w, "# HELP notification_delivery_duration_seconds Delivery latency")
	fmt.Fprintln(w, "# TYPE notification_delivery_duration_seconds histogram")
	m.latencyMu.Lock()
	defer m.latencyMu.Unlock()
	buckets := []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
	for ch, samples := range m.latencySamples {
		if len(samples) == 0 {
			continue
		}
		sorted := make([]float64, len(samples))
		copy(sorted, samples)
		sort.Float64s(sorted)
		var sum float64
		for _, s := range sorted {
			sum += s
		}
		for _, b := range buckets {
			count := 0
			for _, s := range sorted {
				if s <= b {
					count++
				}
			}
			fmt.Fprintf(w, "notification_delivery_duration_seconds_bucket{channel=%q,le=\"%.3f\"} %d\n", ch, b, count)
		}
		fmt.Fprintf(w, "notification_delivery_duration_seconds_bucket{channel=%q,le=\"+Inf\"} %d\n", ch, len(sorted))
		fmt.Fprintf(w, "notification_delivery_duration_seconds_sum{channel=%q} %g\n", ch, sum)
		fmt.Fprintf(w, "notification_delivery_duration_seconds_count{channel=%q} %d\n", ch, len(sorted))
	}
}

// Percentile computes a percentile from a sorted float64 slice.
func Percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := p / 100.0 * float64(len(sorted)-1)
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if lo == hi {
		return sorted[lo]
	}
	return sorted[lo]*(float64(hi)-idx) + sorted[hi]*(idx-float64(lo))
}
