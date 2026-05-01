package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"github.com/sgnraft/insider-case/internal/domain"
)

// Repository handles all database operations via database/sql + lib/pq.
type Repository struct {
	db     *sql.DB
	logger *slog.Logger
}

// New creates a new Repository.
func New(db *sql.DB, logger *slog.Logger) *Repository {
	return &Repository{db: db, logger: logger}
}

// Create inserts a new notification.
func (r *Repository) Create(ctx context.Context, n *domain.Notification) error {
	if n.ID == "" {
		n.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	n.CreatedAt = now
	n.UpdatedAt = now

	tvJSON, _ := json.Marshal(n.TemplateVars)
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO notifications (
			id, batch_id, recipient, channel, content, priority, status,
			idempotency_key, template_id, template_vars,
			retry_count, max_retries, scheduled_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		n.ID, n.BatchID, n.Recipient, n.Channel, n.Content, n.Priority, n.Status,
		nilIfEmpty(n.IdempotencyKey), n.TemplateID, tvJSON,
		n.RetryCount, n.MaxRetries, n.ScheduledAt, n.CreatedAt, n.UpdatedAt,
	)
	return err
}

// CreateBatch inserts multiple notifications in a single transaction.
func (r *Repository) CreateBatch(ctx context.Context, notifications []*domain.Notification) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO notifications (
			id, batch_id, recipient, channel, content, priority, status,
			idempotency_key, template_id, template_vars,
			retry_count, max_retries, scheduled_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().UTC()
	for _, n := range notifications {
		if n.ID == "" {
			n.ID = uuid.NewString()
		}
		n.CreatedAt = now
		n.UpdatedAt = now
		tvJSON, _ := json.Marshal(n.TemplateVars)

		if _, err := stmt.ExecContext(ctx,
			n.ID, n.BatchID, n.Recipient, n.Channel, n.Content, n.Priority, n.Status,
			nilIfEmpty(n.IdempotencyKey), n.TemplateID, tvJSON,
			n.RetryCount, n.MaxRetries, n.ScheduledAt, n.CreatedAt, n.UpdatedAt,
		); err != nil {
			return fmt.Errorf("insert %s: %w", n.ID, err)
		}
	}
	return tx.Commit()
}

// GetByID retrieves a notification by its ID.
func (r *Repository) GetByID(ctx context.Context, id string) (*domain.Notification, error) {
	row := r.db.QueryRowContext(ctx, selectCols+` WHERE id = $1`, id)
	return scanRow(row)
}

// GetByIdempotencyKey looks up a notification by idempotency key.
func (r *Repository) GetByIdempotencyKey(ctx context.Context, key string) (*domain.Notification, error) {
	row := r.db.QueryRowContext(ctx, selectCols+` WHERE idempotency_key = $1`, key)
	n, err := scanRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return n, err
}

// UpdateStatus updates the status and related fields.
func (r *Repository) UpdateStatus(ctx context.Context, id, status string, opts ...UpdateOption) error {
	u := &updateFields{updatedAt: time.Now().UTC()}
	for _, o := range opts {
		o(u)
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE notifications SET
			status = $2, retry_count = $3, next_retry_at = $4,
			delivered_at = $5, error_message = $6,
			provider_message_id = $7, updated_at = $8
		WHERE id = $1`,
		id, status, u.retryCount, u.nextRetryAt, u.deliveredAt,
		u.errorMessage, u.providerMsgID, u.updatedAt,
	)
	return err
}

// Cancel marks a pending/scheduled notification as cancelled.
func (r *Repository) Cancel(ctx context.Context, id string) (bool, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE notifications SET status = 'cancelled', updated_at = $2
		WHERE id = $1 AND status IN ('pending','scheduled')`,
		id, time.Now().UTC(),
	)
	if err != nil {
		return false, err
	}
	rows, _ := res.RowsAffected()
	return rows > 0, nil
}

// List returns paginated notifications with optional filters.
func (r *Repository) List(ctx context.Context, f domain.ListFilter) (*domain.ListResponse, error) {
	if f.PageSize <= 0 {
		f.PageSize = 20
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	offset := (f.Page - 1) * f.PageSize

	where, args := buildWhere(f)

	var total int64
	if err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM notifications "+where, args...).Scan(&total); err != nil {
		return nil, err
	}

	args = append(args, f.PageSize, offset)
	n := len(args)
	rows, err := r.db.QueryContext(ctx,
		selectCols+` `+where+
			fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, n-1, n),
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notifications []domain.Notification
	for rows.Next() {
		notif, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		notifications = append(notifications, *notif)
	}

	totalPages := int(math.Ceil(float64(total) / float64(f.PageSize)))
	return &domain.ListResponse{
		Data:       notifications,
		Total:      total,
		Page:       f.Page,
		PageSize:   f.PageSize,
		TotalPages: totalPages,
	}, nil
}

// GetPendingScheduled returns scheduled notifications that are due.
func (r *Repository) GetPendingScheduled(ctx context.Context, limit int) ([]*domain.Notification, error) {
	rows, err := r.db.QueryContext(ctx, selectCols+`
		WHERE status = 'scheduled' AND scheduled_at <= NOW()
		ORDER BY scheduled_at ASC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRows(rows)
}

// GetForRetry returns failed notifications ready for retry.
func (r *Repository) GetForRetry(ctx context.Context, limit int) ([]*domain.Notification, error) {
	rows, err := r.db.QueryContext(ctx, selectCols+`
		WHERE status = 'failed' AND retry_count < max_retries AND next_retry_at <= NOW()
		ORDER BY next_retry_at ASC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRows(rows)
}

// --- Template operations ---

func (r *Repository) CreateTemplate(ctx context.Context, t *domain.Template) error {
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO templates (id, name, channel, content, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		t.ID, t.Name, t.Channel, t.Content, t.CreatedAt, t.UpdatedAt)
	return err
}

func (r *Repository) GetTemplate(ctx context.Context, id string) (*domain.Template, error) {
	t := &domain.Template{}
	err := r.db.QueryRowContext(ctx, `
		SELECT id, name, channel, content, created_at, updated_at
		FROM templates WHERE id = $1`, id).
		Scan(&t.ID, &t.Name, &t.Channel, &t.Content, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// --- Helpers ---

const selectCols = `
	SELECT id, batch_id, recipient, channel, content, priority, status,
	       COALESCE(idempotency_key,''), template_id, template_vars, provider_message_id,
	       retry_count, max_retries, next_retry_at, scheduled_at,
	       delivered_at, error_message, created_at, updated_at
	FROM notifications`

type scanner interface {
	Scan(dest ...any) error
}

func scanRow(s scanner) (*domain.Notification, error) {
	n := &domain.Notification{}
	var tvJSON []byte
	err := s.Scan(
		&n.ID, &n.BatchID, &n.Recipient, &n.Channel, &n.Content,
		&n.Priority, &n.Status, &n.IdempotencyKey, &n.TemplateID,
		&tvJSON, &n.ProviderMsgID,
		&n.RetryCount, &n.MaxRetries, &n.NextRetryAt, &n.ScheduledAt,
		&n.DeliveredAt, &n.ErrorMessage, &n.CreatedAt, &n.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if len(tvJSON) > 0 {
		json.Unmarshal(tvJSON, &n.TemplateVars)
	}
	return n, nil
}

func scanRows(rows *sql.Rows) ([]*domain.Notification, error) {
	var result []*domain.Notification
	for rows.Next() {
		n, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, rows.Err()
}

func buildWhere(f domain.ListFilter) (string, []interface{}) {
	where := "WHERE 1=1"
	args := []interface{}{}
	idx := 1
	if f.Status != "" {
		where += fmt.Sprintf(" AND status = $%d", idx)
		args = append(args, f.Status)
		idx++
	}
	if f.Channel != "" {
		where += fmt.Sprintf(" AND channel = $%d", idx)
		args = append(args, f.Channel)
		idx++
	}
	if f.BatchID != "" {
		where += fmt.Sprintf(" AND batch_id = $%d", idx)
		args = append(args, f.BatchID)
		idx++
	}
	if f.DateFrom != nil {
		where += fmt.Sprintf(" AND created_at >= $%d", idx)
		args = append(args, f.DateFrom)
		idx++
	}
	if f.DateTo != nil {
		where += fmt.Sprintf(" AND created_at <= $%d", idx)
		args = append(args, f.DateTo)
		idx++
	}
	return where, args
}

func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// --- Functional options for UpdateStatus ---

type updateFields struct {
	retryCount    int
	nextRetryAt   *time.Time
	deliveredAt   *time.Time
	errorMessage  string
	providerMsgID string
	updatedAt     time.Time
}

type UpdateOption func(*updateFields)

func WithRetry(count int, nextAt time.Time) UpdateOption {
	return func(u *updateFields) {
		u.retryCount = count
		u.nextRetryAt = &nextAt
	}
}

func WithDelivered(at time.Time, msgID string) UpdateOption {
	return func(u *updateFields) {
		u.deliveredAt = &at
		u.providerMsgID = msgID
	}
}

func WithError(msg string) UpdateOption {
	return func(u *updateFields) {
		u.errorMessage = msg
	}
}

func WithRetryCount(count int) UpdateOption {
	return func(u *updateFields) {
		u.retryCount = count
	}
}
