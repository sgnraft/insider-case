package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

const correlationIDHeader = "X-Correlation-ID"

type contextKey string

const correlationKey contextKey = "correlationID"

// CorrelationID injects or propagates a correlation ID into every request.
func CorrelationID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(correlationIDHeader)
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set(correlationIDHeader, id)
		ctx := context.WithValue(r.Context(), correlationKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetCorrelationID retrieves the correlation ID from the context.
func GetCorrelationID(ctx context.Context) string {
	if id, ok := ctx.Value(correlationKey).(string); ok {
		return id
	}
	return ""
}

// RequestLogger logs every incoming HTTP request with method, path, status, and latency.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(ww, r)
			logger.Info("http_request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.statusCode,
				"latency_ms", time.Since(start).Milliseconds(),
				"correlation_id", GetCorrelationID(r.Context()),
				"remote_addr", r.RemoteAddr,
			)
		})
	}
}

// Recoverer recovers from panics and returns a 500 response.
func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rc := recover(); rc != nil {
					logger.Error("panic_recovered",
						"panic", rc,
						"correlation_id", GetCorrelationID(r.Context()),
					)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					if _, err := w.Write([]byte(`{"error":"internal server error"}`)); err != nil {
						logger.Error("write panic response failed", "err", err)
					}
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
