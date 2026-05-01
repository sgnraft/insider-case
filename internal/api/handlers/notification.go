package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/sgnraft/insider-case/internal/api/middleware"
	"github.com/sgnraft/insider-case/internal/domain"
	"github.com/sgnraft/insider-case/internal/queue"
	"github.com/sgnraft/insider-case/internal/repository"
	tmpl "github.com/sgnraft/insider-case/internal/template"
)

// NotificationHandler handles all notification-related HTTP endpoints.
type NotificationHandler struct {
	repo       *repository.Repository
	queue      *queue.Queue
	logger     *slog.Logger
	maxRetries int
}

// NewNotificationHandler creates a new handler.
func NewNotificationHandler(repo *repository.Repository, q *queue.Queue, logger *slog.Logger, maxRetries int) *NotificationHandler {
	return &NotificationHandler{repo: repo, queue: q, logger: logger, maxRetries: maxRetries}
}

// Create handles POST /api/v1/notifications.
func (h *NotificationHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateNotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	n, err := h.buildNotification(r, req, "")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Idempotency check
	if n.IdempotencyKey != "" {
		existing, err := h.repo.GetByIdempotencyKey(r.Context(), n.IdempotencyKey)
		if err != nil {
			h.logger.Error("idempotency check failed", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if existing != nil {
			writeJSON(w, http.StatusOK, existing)
			return
		}
	}

	if err := h.repo.Create(r.Context(), n); err != nil {
		h.logger.Error("create notification failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to create notification")
		return
	}

	if n.Status == domain.StatusPending {
		h.enqueue(r, n)
	}

	h.logger.Info("notification created",
		"id", n.ID,
		"correlation_id", middleware.GetCorrelationID(r.Context()),
	)
	writeJSON(w, http.StatusCreated, n)
}

// CreateBatch handles POST /api/v1/notifications/batch.
func (h *NotificationHandler) CreateBatch(w http.ResponseWriter, r *http.Request) {
	var req domain.BatchCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Notifications) == 0 {
		writeError(w, http.StatusBadRequest, "notifications array is empty")
		return
	}
	if len(req.Notifications) > 1000 {
		writeError(w, http.StatusBadRequest, "maximum 1000 notifications per batch")
		return
	}

	batchID := uuid.NewString()
	var accepted []domain.Notification
	var dbNotifications []*domain.Notification
	var batchErrors []domain.BatchError

	for i, reqItem := range req.Notifications {
		n, err := h.buildNotification(r, reqItem, batchID)
		if err != nil {
			batchErrors = append(batchErrors, domain.BatchError{Index: i, Message: err.Error()})
			continue
		}
		dbNotifications = append(dbNotifications, n)
		accepted = append(accepted, *n)
	}

	if len(dbNotifications) > 0 {
		if err := h.repo.CreateBatch(r.Context(), dbNotifications); err != nil {
			h.logger.Error("batch create failed", "err", err)
			writeError(w, http.StatusInternalServerError, "failed to create batch")
			return
		}
		for _, n := range dbNotifications {
			if n.Status == domain.StatusPending {
				h.enqueue(r, n)
			}
		}
	}

	writeJSON(w, http.StatusCreated, domain.BatchCreateResponse{
		BatchID:       batchID,
		Total:         len(req.Notifications),
		Accepted:      len(accepted),
		Rejected:      len(batchErrors),
		Notifications: accepted,
		Errors:        batchErrors,
	})
}

// GetByID handles GET /api/v1/notifications/{id}.
func (h *NotificationHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	n, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "notification not found")
		return
	}
	writeJSON(w, http.StatusOK, n)
}

// Cancel handles DELETE /api/v1/notifications/{id}.
func (h *NotificationHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cancelled, err := h.repo.Cancel(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to cancel")
		return
	}
	if !cancelled {
		writeError(w, http.StatusConflict, "notification cannot be cancelled (not pending or scheduled)")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled", "id": id})
}

// List handles GET /api/v1/notifications.
func (h *NotificationHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := domain.ListFilter{
		Status:   q.Get("status"),
		Channel:  q.Get("channel"),
		BatchID:  q.Get("batch_id"),
		Page:     parseIntQuery(q.Get("page"), 1),
		PageSize: parseIntQuery(q.Get("page_size"), 20),
	}
	if df := q.Get("date_from"); df != "" {
		if t, err := time.Parse(time.RFC3339, df); err == nil {
			filter.DateFrom = &t
		}
	}
	if dt := q.Get("date_to"); dt != "" {
		if t, err := time.Parse(time.RFC3339, dt); err == nil {
			filter.DateTo = &t
		}
	}
	result, err := h.repo.List(r.Context(), filter)
	if err != nil {
		h.logger.Error("list failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to list notifications")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// --- Internal helpers ---

func (h *NotificationHandler) buildNotification(r *http.Request, req domain.CreateNotificationRequest, batchID string) (*domain.Notification, error) {
	if err := validateChannel(req.Channel); err != nil {
		return nil, err
	}
	if req.Recipient == "" {
		return nil, fmt.Errorf("recipient is required")
	}
	priority := req.Priority
	if priority == "" {
		priority = domain.PriorityNormal
	}
	if err := validatePriority(priority); err != nil {
		return nil, err
	}

	content := req.Content
	var templateID string

	if req.TemplateID != "" {
		t, err := h.repo.GetTemplate(r.Context(), req.TemplateID)
		if err != nil {
			return nil, fmt.Errorf("template not found: %s", req.TemplateID)
		}
		if t.Channel != req.Channel {
			return nil, fmt.Errorf("template channel mismatch: want %s, got %s", req.Channel, t.Channel)
		}
		rendered, err := tmpl.Render(t.Content, req.TemplateVars)
		if err != nil {
			return nil, err
		}
		content = rendered
		templateID = t.ID
	}

	if content == "" {
		return nil, fmt.Errorf("content is required (or provide template_id)")
	}
	if err := validateContent(req.Channel, content); err != nil {
		return nil, err
	}

	status := domain.StatusPending
	if req.ScheduledAt != nil && req.ScheduledAt.After(time.Now()) {
		status = domain.StatusScheduled
	}

	return &domain.Notification{
		BatchID:        batchID,
		Recipient:      req.Recipient,
		Channel:        req.Channel,
		Content:        content,
		Priority:       priority,
		Status:         status,
		IdempotencyKey: req.IdempotencyKey,
		TemplateID:     templateID,
		TemplateVars:   req.TemplateVars,
		MaxRetries:     h.maxRetries,
		ScheduledAt:    req.ScheduledAt,
	}, nil
}

func (h *NotificationHandler) enqueue(r *http.Request, n *domain.Notification) {
	item := domain.QueueItem{
		NotificationID: n.ID,
		Priority:       n.Priority,
		EnqueuedAt:     time.Now().UTC(),
	}
	if err := h.queue.Enqueue(r.Context(), item); err != nil {
		h.logger.Error("enqueue failed", "id", n.ID, "err", err)
	}
}

// --- Validation ---

func validateChannel(ch string) error {
	switch ch {
	case domain.ChannelSMS, domain.ChannelEmail, domain.ChannelPush:
		return nil
	}
	return fmt.Errorf("invalid channel %q: must be sms, email, or push", ch)
}

func validatePriority(p string) error {
	switch p {
	case domain.PriorityHigh, domain.PriorityNormal, domain.PriorityLow:
		return nil
	}
	return fmt.Errorf("invalid priority %q: must be high, normal, or low", p)
}

func validateContent(channel, content string) error {
	limits := map[string]int{
		domain.ChannelSMS:   160,
		domain.ChannelEmail: 10000,
		domain.ChannelPush:  256,
	}
	if limit, ok := limits[channel]; ok && len(content) > limit {
		return fmt.Errorf("content exceeds %d character limit for channel %s", limit, channel)
	}
	return nil
}

// --- HTTP helpers ---

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func parseIntQuery(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return def
	}
	return v
}
