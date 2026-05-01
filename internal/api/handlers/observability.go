package handlers

import (
	"net/http"

	"github.com/sgnraft/insider-case/internal/metrics"
	"github.com/sgnraft/insider-case/internal/queue"
)

// ObservabilityHandler handles health and metrics endpoints.
type ObservabilityHandler struct {
	queue   *queue.Queue
	metrics *metrics.Metrics
}

// NewObservabilityHandler creates a new ObservabilityHandler.
func NewObservabilityHandler(q *queue.Queue, m *metrics.Metrics) *ObservabilityHandler {
	return &ObservabilityHandler{queue: q, metrics: m}
}

// Health handles GET /health.
func (h *ObservabilityHandler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "notification-system",
	})
}

// MetricsSummary handles GET /api/v1/metrics/summary — human-readable JSON.
func (h *ObservabilityHandler) MetricsSummary(w http.ResponseWriter, r *http.Request) {
	snap := h.metrics.Snapshot()
	snap["queue_depth"] = h.queue.Depths(r.Context())
	writeJSON(w, http.StatusOK, snap)
}

// PrometheusMetrics handles GET /metrics — standard Prometheus text format.
func (h *ObservabilityHandler) PrometheusMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	h.metrics.WritePrometheusText(w)
}
