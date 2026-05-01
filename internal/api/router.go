package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/sgnraft/insider-case/internal/api/handlers"
	"github.com/sgnraft/insider-case/internal/api/middleware"
	"github.com/sgnraft/insider-case/internal/metrics"
	"github.com/sgnraft/insider-case/internal/queue"
	"github.com/sgnraft/insider-case/internal/repository"
)

// NewRouter builds and returns the application HTTP router.
func NewRouter(
	repo *repository.Repository,
	q *queue.Queue,
	m *metrics.Metrics,
	logger *slog.Logger,
	maxRetries int,
) http.Handler {
	r := chi.NewRouter()

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type", "X-Correlation-ID"},
	}))
	r.Use(chiMiddleware.RealIP)
	r.Use(middleware.CorrelationID)
	r.Use(middleware.RequestLogger(logger))
	r.Use(middleware.Recoverer(logger))
	r.Use(chiMiddleware.Compress(5))

	notifH := handlers.NewNotificationHandler(repo, q, logger, maxRetries)
	obsH := handlers.NewObservabilityHandler(q, m)
	tmplH := handlers.NewTemplateHandler(repo, logger)

	r.Get("/health", obsH.Health)
	r.Get("/metrics", obsH.PrometheusMetrics)

	r.Route("/api/v1", func(r chi.Router) {
		r.Route("/notifications", func(r chi.Router) {
			r.Post("/", notifH.Create)
			r.Post("/batch", notifH.CreateBatch)
			r.Get("/", notifH.List)
			r.Get("/{id}", notifH.GetByID)
			r.Delete("/{id}", notifH.Cancel)
		})

		r.Route("/templates", func(r chi.Router) {
			r.Post("/", tmplH.Create)
			r.Get("/{id}", tmplH.GetByID)
		})

		r.Get("/metrics/summary", obsH.MetricsSummary)
	})

	return r
}
