package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/sgnraft/insider-case/internal/domain"
	"github.com/sgnraft/insider-case/internal/queue"
	"github.com/sgnraft/insider-case/internal/repository"
)

// Scheduler polls the database for scheduled and retry-eligible notifications.
type Scheduler struct {
	repo   *repository.Repository
	queue  *queue.Queue
	logger *slog.Logger
}

// New creates a new Scheduler.
func New(repo *repository.Repository, q *queue.Queue, logger *slog.Logger) *Scheduler {
	return &Scheduler{repo: repo, queue: q, logger: logger}
}

// Start begins polling loops; blocks until ctx is cancelled.
func (s *Scheduler) Start(ctx context.Context) {
	s.logger.Info("scheduler started")
	scheduledTick := time.NewTicker(5 * time.Second)
	retryTick := time.NewTicker(10 * time.Second)
	defer scheduledTick.Stop()
	defer retryTick.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("scheduler stopped")
			return
		case <-scheduledTick.C:
			s.processScheduled(ctx)
		case <-retryTick.C:
			s.processRetries(ctx)
		}
	}
}

func (s *Scheduler) processScheduled(ctx context.Context) {
	notifications, err := s.repo.GetPendingScheduled(ctx, 100)
	if err != nil {
		s.logger.Error("fetch scheduled notifications failed", "err", err)
		return
	}
	for _, n := range notifications {
		if err := s.repo.UpdateStatus(ctx, n.ID, domain.StatusPending); err != nil {
			s.logger.Error("set scheduled to pending failed", "id", n.ID, "err", err)
			continue
		}
		item := domain.QueueItem{
			NotificationID: n.ID,
			Priority:       n.Priority,
			EnqueuedAt:     time.Now().UTC(),
		}
		if err := s.queue.Enqueue(ctx, item); err != nil {
			s.logger.Error("enqueue scheduled notification failed", "id", n.ID, "err", err)
		} else {
			s.logger.Info("scheduled notification enqueued", "id", n.ID)
		}
	}
}

func (s *Scheduler) processRetries(ctx context.Context) {
	notifications, err := s.repo.GetForRetry(ctx, 100)
	if err != nil {
		s.logger.Error("fetch retry notifications failed", "err", err)
		return
	}
	for _, n := range notifications {
		item := domain.QueueItem{
			NotificationID: n.ID,
			Priority:       n.Priority,
			EnqueuedAt:     time.Now().UTC(),
			RetryCount:     n.RetryCount,
		}
		if err := s.queue.Enqueue(ctx, item); err != nil {
			s.logger.Error("re-enqueue retry failed", "id", n.ID, "err", err)
		} else {
			s.logger.Info("notification re-enqueued for retry", "id", n.ID, "retry_count", n.RetryCount)
		}
	}
}
