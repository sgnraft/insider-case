package queue

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/sgnraft/insider-case/internal/delivery"
	"github.com/sgnraft/insider-case/internal/domain"
	"github.com/sgnraft/insider-case/internal/metrics"
	"github.com/sgnraft/insider-case/internal/ratelimit"
	"github.com/sgnraft/insider-case/internal/repository"
)

// Worker processes notifications from the priority queue.
type Worker struct {
	repo        *repository.Repository
	queue       *Queue
	provider    *delivery.Provider
	metrics     *metrics.Metrics
	logger      *slog.Logger
	concurrency int
	rateLimits  *ratelimit.Group
	wg          sync.WaitGroup
}

// NewWorker creates a new Worker.
func NewWorker(
	repo *repository.Repository,
	q *Queue,
	provider *delivery.Provider,
	m *metrics.Metrics,
	logger *slog.Logger,
	concurrency int,
	rateLimitPerSec float64,
) *Worker {
	return &Worker{
		repo:        repo,
		queue:       q,
		provider:    provider,
		metrics:     m,
		logger:      logger,
		concurrency: concurrency,
		rateLimits:  ratelimit.NewGroup(rateLimitPerSec),
	}
}

// Start launches worker goroutines and blocks until context is cancelled.
func (w *Worker) Start(ctx context.Context, pollInterval time.Duration) {
	w.logger.Info("worker pool starting", "concurrency", w.concurrency)
	sem := make(chan struct{}, w.concurrency)

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("worker shutting down, waiting for in-flight jobs")
			w.wg.Wait()
			return
		default:
		}

		item, err := w.queue.Dequeue(ctx)
		if err != nil {
			w.logger.Error("dequeue error", "err", err)
			time.Sleep(pollInterval)
			continue
		}
		if item == nil {
			time.Sleep(pollInterval)
			continue
		}

		sem <- struct{}{}
		w.wg.Add(1)
		go func(qi domain.QueueItem) {
			defer func() {
				<-sem
				w.wg.Done()
			}()
			w.process(ctx, qi)
		}(*item)
	}
}

func (w *Worker) process(ctx context.Context, item domain.QueueItem) {
	start := time.Now()
	log := w.logger.With("notification_id", item.NotificationID, "priority", item.Priority)

	defer w.queue.AcknowledgeProcessing(ctx, item.NotificationID)

	n, err := w.repo.GetByID(ctx, item.NotificationID)
	if err != nil {
		log.Error("load notification failed", "err", err)
		return
	}

	if n.Status != domain.StatusPending &&
		n.Status != domain.StatusFailed &&
		n.Status != domain.StatusScheduled {
		log.Warn("skipping notification in unexpected state", "status", n.Status)
		return
	}

	// Per-channel rate limiting
	if err := w.rateLimits.Get(n.Channel).Wait(ctx); err != nil {
		log.Warn("rate limiter cancelled", "err", err)
		return
	}

	if err := w.repo.UpdateStatus(ctx, n.ID, domain.StatusProcessing); err != nil {
		log.Error("mark processing failed", "err", err)
		return
	}

	resp, deliveryErr := w.provider.Send(ctx, n)
	latency := time.Since(start)

	if deliveryErr != nil {
		w.handleFailure(ctx, n, item, deliveryErr, log)
		w.metrics.RecordFailure(n.Channel, latency)
		return
	}

	now := time.Now().UTC()
	if err := w.repo.UpdateStatus(ctx, n.ID, domain.StatusDelivered,
		repository.WithDelivered(now, resp.MessageID)); err != nil {
		log.Error("mark delivered failed", "err", err)
	}

	w.metrics.RecordSuccess(n.Channel, latency)
	log.Info("notification delivered", "provider_msg_id", resp.MessageID, "latency_ms", latency.Milliseconds())
}

func (w *Worker) handleFailure(ctx context.Context, n *domain.Notification, item domain.QueueItem, deliveryErr error, log *slog.Logger) {
	newCount := n.RetryCount + 1

	if newCount >= n.MaxRetries {
		if err := w.repo.UpdateStatus(ctx, n.ID, domain.StatusFailed,
			repository.WithRetryCount(newCount),
			repository.WithError(deliveryErr.Error())); err != nil {
			log.Error("mark permanently failed", "err", err)
		}
		w.queue.SendToDLQ(ctx, item, deliveryErr.Error())
		w.metrics.RecordRetry(n.Channel)
		log.Warn("notification permanently failed", "retry_count", newCount, "err", deliveryErr)
		return
	}

	delay := domain.RetryDelay(newCount)
	nextRetry := time.Now().Add(delay).UTC()
	if err := w.repo.UpdateStatus(ctx, n.ID, domain.StatusFailed,
		repository.WithRetry(newCount, nextRetry),
		repository.WithError(deliveryErr.Error())); err != nil {
		log.Error("schedule retry failed", "err", err)
		return
	}
	w.metrics.RecordRetry(n.Channel)
	log.Warn("notification failed, scheduled retry", "retry_count", newCount, "backoff", delay, "err", deliveryErr)
}
