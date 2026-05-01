package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/sgnraft/insider-case/internal/domain"
)

const (
	queueKeyHigh   = "queue:high"
	queueKeyNormal = "queue:normal"
	queueKeyLow    = "queue:low"
	dlqKey         = "queue:dlq"
	processingPfx  = "processing:"
)

// Queue manages the Redis-backed priority queue.
type Queue struct {
	rdb    *redis.Client
	logger *slog.Logger
}

// New creates a new Queue.
func New(rdb *redis.Client, logger *slog.Logger) *Queue {
	return &Queue{rdb: rdb, logger: logger}
}

// Enqueue adds a notification to the appropriate priority sorted set.
func (q *Queue) Enqueue(ctx context.Context, item domain.QueueItem) error {
	data, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("marshal queue item: %w", err)
	}
	key := priorityKey(item.Priority)
	score := float64(item.EnqueuedAt.UnixNano())
	return q.rdb.ZAdd(ctx, key, redis.Z{Score: score, Member: string(data)}).Err()
}

// Dequeue pops the highest-priority item (high → normal → low).
func (q *Queue) Dequeue(ctx context.Context) (*domain.QueueItem, error) {
	for _, key := range []string{queueKeyHigh, queueKeyNormal, queueKeyLow} {
		res, err := q.rdb.ZPopMin(ctx, key, 1).Result()
		if err != nil && err != redis.Nil {
			return nil, fmt.Errorf("zpopmin %s: %w", key, err)
		}
		if len(res) == 0 {
			continue
		}
		var item domain.QueueItem
		if err := json.Unmarshal([]byte(res[0].Member.(string)), &item); err != nil {
			q.logger.Error("unmarshal queue item failed", "err", err)
			continue
		}
		q.rdb.SetEx(ctx, processingPfx+item.NotificationID, "1", 5*time.Minute)
		return &item, nil
	}
	return nil, nil
}

// Depths returns the number of items in each priority queue.
func (q *Queue) Depths(ctx context.Context) map[string]int64 {
	out := map[string]int64{}
	for _, pair := range []struct{ k, n string }{
		{queueKeyHigh, "high"}, {queueKeyNormal, "normal"},
		{queueKeyLow, "low"}, {dlqKey, "dlq"},
	} {
		n, _ := q.rdb.ZCard(ctx, pair.k).Result()
		out[pair.n] = n
	}
	return out
}

// SendToDLQ moves a permanently-failed item to the dead-letter queue.
func (q *Queue) SendToDLQ(ctx context.Context, item domain.QueueItem, reason string) error {
	payload, _ := json.Marshal(map[string]interface{}{
		"item": item, "reason": reason, "failed_at": time.Now().UTC(),
	})
	return q.rdb.ZAdd(ctx, dlqKey, redis.Z{
		Score:  float64(time.Now().UnixNano()),
		Member: string(payload),
	}).Err()
}

// AcknowledgeProcessing removes the in-processing marker for a notification.
func (q *Queue) AcknowledgeProcessing(ctx context.Context, id string) {
	q.rdb.Del(ctx, processingPfx+id)
}

func priorityKey(priority string) string {
	switch priority {
	case domain.PriorityHigh:
		return queueKeyHigh
	case domain.PriorityLow:
		return queueKeyLow
	default:
		return queueKeyNormal
	}
}
