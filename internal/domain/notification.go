package domain

import "time"

// Channel types
const (
	ChannelSMS   = "sms"
	ChannelEmail = "email"
	ChannelPush  = "push"
)

// Priority levels
const (
	PriorityHigh   = "high"
	PriorityNormal = "normal"
	PriorityLow    = "low"
)

// Status values
const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusDelivered  = "delivered"
	StatusFailed     = "failed"
	StatusCancelled  = "cancelled"
	StatusScheduled  = "scheduled"
)

// Notification is the core domain entity.
type Notification struct {
	ID             string                 `json:"id"`
	BatchID        string                 `json:"batch_id,omitempty"`
	Recipient      string                 `json:"recipient"`
	Channel        string                 `json:"channel"`
	Content        string                 `json:"content"`
	Priority       string                 `json:"priority"`
	Status         string                 `json:"status"`
	IdempotencyKey string                 `json:"idempotency_key,omitempty"`
	TemplateID     string                 `json:"template_id,omitempty"`
	TemplateVars   map[string]interface{} `json:"template_vars,omitempty"`
	ProviderMsgID  string                 `json:"provider_message_id,omitempty"`
	RetryCount     int                    `json:"retry_count"`
	MaxRetries     int                    `json:"max_retries"`
	NextRetryAt    *time.Time             `json:"next_retry_at,omitempty"`
	ScheduledAt    *time.Time             `json:"scheduled_at,omitempty"`
	DeliveredAt    *time.Time             `json:"delivered_at,omitempty"`
	ErrorMessage   string                 `json:"error_message,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

// Template holds a reusable message template.
type Template struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Channel   string    `json:"channel"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreateNotificationRequest is the API request payload.
type CreateNotificationRequest struct {
	Recipient      string                 `json:"recipient"`
	Channel        string                 `json:"channel"`
	Content        string                 `json:"content,omitempty"`
	Priority       string                 `json:"priority,omitempty"`
	IdempotencyKey string                 `json:"idempotency_key,omitempty"`
	TemplateID     string                 `json:"template_id,omitempty"`
	TemplateVars   map[string]interface{} `json:"template_vars,omitempty"`
	ScheduledAt    *time.Time             `json:"scheduled_at,omitempty"`
}

// BatchCreateRequest holds up to 1000 notifications.
type BatchCreateRequest struct {
	Notifications []CreateNotificationRequest `json:"notifications"`
}

// BatchCreateResponse is returned after batch creation.
type BatchCreateResponse struct {
	BatchID       string         `json:"batch_id"`
	Total         int            `json:"total"`
	Accepted      int            `json:"accepted"`
	Rejected      int            `json:"rejected"`
	Notifications []Notification `json:"notifications"`
	Errors        []BatchError   `json:"errors,omitempty"`
}

// BatchError represents a single error in batch creation.
type BatchError struct {
	Index   int    `json:"index"`
	Message string `json:"message"`
}

// ListFilter holds filtering params for listing notifications.
type ListFilter struct {
	Status   string
	Channel  string
	BatchID  string
	DateFrom *time.Time
	DateTo   *time.Time
	Page     int
	PageSize int
}

// ListResponse wraps paginated notification results.
type ListResponse struct {
	Data       []Notification `json:"data"`
	Total      int64          `json:"total"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
	TotalPages int            `json:"total_pages"`
}

// QueueItem is what gets enqueued in Redis.
type QueueItem struct {
	NotificationID string    `json:"notification_id"`
	Priority       string    `json:"priority"`
	EnqueuedAt     time.Time `json:"enqueued_at"`
	RetryCount     int       `json:"retry_count"`
}

// ProviderRequest is sent to the external webhook provider.
type ProviderRequest struct {
	To      string `json:"to"`
	Channel string `json:"channel"`
	Content string `json:"content"`
}

// ProviderResponse is received from the external webhook provider.
type ProviderResponse struct {
	MessageID string `json:"messageId"`
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

// RetryDelay returns the exponential backoff duration for a given retry attempt.
func RetryDelay(attempt int) time.Duration {
	delays := []time.Duration{
		5 * time.Second,
		15 * time.Second,
		60 * time.Second,
		5 * time.Minute,
		30 * time.Minute,
	}
	if attempt < len(delays) {
		return delays[attempt]
	}
	return delays[len(delays)-1]
}

// PriorityScore returns a numeric score for queue ordering (lower = higher priority).
func PriorityScore(priority string) float64 {
	switch priority {
	case PriorityHigh:
		return 0
	case PriorityNormal:
		return 1
	case PriorityLow:
		return 2
	default:
		return 1
	}
}
