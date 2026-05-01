# 🔔 Notification System

A scalable, event-driven notification system built in Go. Processes and delivers messages through SMS, Email, and Push channels with high throughput, reliable retry logic, and real-time observability.

## Architecture Overview

```
                           Client / Caller
                                  │
                                  ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                            HTTP API (chi)                               │
│  POST /api/v1/notifications        GET /api/v1/notifications            │
│  POST /api/v1/notifications/batch  GET /api/v1/notifications/{id}       │
│  POST /api/v1/templates            GET /health   GET /metrics           │
└───────────────┬───────────────────────────────────────────────┬──────────┘
                │ persist/query                                  │ enqueue/dequeue
                ▼                                                ▼
┌─────────────────────────────────┐        ┌────────────────────────────────────┐
│ PostgreSQL                      │        │ Redis Priority Queue               │
│ • notifications                 │        │ • queue:high   (sorted set)       │
│ • templates                     │        │ • queue:normal (sorted set)       │
│ • retry tracking + next_retry_at│        │ • queue:low    (sorted set)       │
│ • scheduled_at index            │        │ • queue:dlq                        │
└─────────────────────────────────┘        └──────────────────┬─────────────────┘
                                                              │
                                                              ▼
                                    ┌────────────────────────────────────┐
                                    │ Worker Pool (configurable)        │
                                    │ • per-channel rate limit           │
                                    │ • exponential retry backoff        │
                                    │ • scheduled/retry re-enqueue       │
                                    └──────────────────┬─────────────────┘
                                                       │ deliver
                                                       ▼
                                    ┌────────────────────────────────────┐
                                    │ External Provider                  │
                                    │ webhook.site (simulated)           │
                                    │ POST {to, channel, content}        │
                                    └────────────────────────────────────┘
```

### Key Design Decisions

**Priority Queue via Redis Sorted Sets**
Three sorted sets (`queue:high`, `queue:normal`, `queue:low`) are polled in priority order. Workers `ZPOPMIN` from high first, falling through to normal then low. Score = Unix nanosecond timestamp, ensuring FIFO within the same priority level.

**Retry Strategy (Exponential Backoff)**
| Attempt | Delay    |
|---------|----------|
| 1st     | 5s       |
| 2nd     | 15s      |
| 3rd     | 60s      |
| 4th     | 5min     |
| 5th     | 30min    |
| Final   | Dead Letter Queue |

The `scheduler` goroutine polls every 10s for notifications where `status = 'failed' AND next_retry_at <= NOW()` and re-enqueues them.

**Rate Limiting**
Per-channel token bucket limiters (`golang.org/x/time/rate`) with a default of 100 msg/s per channel. Workers call `limiter.Wait(ctx)` before sending, naturally applying backpressure.

**Idempotency**
Unique `idempotency_key` column with a database constraint. On create, if a key already exists, the existing notification is returned without re-processing.

**Scheduled Notifications**
If `scheduled_at` is in the future, status is set to `scheduled` (not enqueued). The scheduler goroutine polls every 5s for due scheduled notifications and transitions them to `pending` before enqueueing.

## Quick Start

```bash
# 1. Clone the repo
git clone https://github.com/sgnraft/insider-case

# 2. Set your webhook.site URL
export PROVIDER_WEBHOOK_URL=https://webhook.site/623283ec-922b-433b-9e6c-235a4e017c73

# 3. Start everything
docker-compose up --build

The API is available at http://localhost:8080
Swagger UI is available at http://localhost:8081
```

## Configuration

All configuration is via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_ADDR` | `:8080` | HTTP listen address |
| `DATABASE_DSN` | `postgres://...` | PostgreSQL connection string |
| `REDIS_ADDR` | `localhost:6379` | Redis address |
| `PROVIDER_WEBHOOK_URL` | — | External provider URL |
| `WORKER_CONCURRENCY` | `10` | Number of parallel worker goroutines |
| `WORKER_RATE_LIMIT` | `100` | Max messages/second per channel |
| `WORKER_MAX_RETRIES` | `5` | Max delivery attempts |
| `SCHEDULER_ENABLED` | `true` | Enable scheduled/retry scheduler |

## API Examples

### Create a Notification

```bash
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{
    "recipient": "+905551234567",
    "channel": "sms",
    "content": "Your OTP is 1234",
    "priority": "high"
  }'
```

### Create with Idempotency Key

```bash
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -H "X-Correlation-ID: my-trace-123" \
  -d '{
    "recipient": "+905551234567",
    "channel": "sms",
    "content": "Order confirmed",
    "idempotency_key": "order-999-confirm"
  }'
```

### Schedule a Future Notification

```bash
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{
    "recipient": "user@example.com",
    "channel": "email",
    "content": "Your flash sale starts now!",
    "scheduled_at": "2026-06-01T09:00:00Z"
  }'
```

### Create a Template and Use It

```bash
# Create template
curl -X POST http://localhost:8080/api/v1/templates \
  -H "Content-Type: application/json" \
  -d '{
    "name": "otp_sms",
    "channel": "sms",
    "content": "Hi {{name}}, your verification code is {{code}}. Valid for 5 minutes."
  }'

# Use template in notification
curl -X POST http://localhost:8080/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{
    "recipient": "+905551234567",
    "channel": "sms",
    "template_id": "<template-id-from-above>",
    "template_vars": {"name": "Ahmet", "code": "982341"},
    "priority": "high"
  }'
```

### Batch Create (up to 1000)

```bash
curl -X POST http://localhost:8080/api/v1/notifications/batch \
  -H "Content-Type: application/json" \
  -d '{
    "notifications": [
      {"recipient": "+905551111111", "channel": "sms", "content": "Flash sale!", "priority": "high"},
      {"recipient": "+905552222222", "channel": "sms", "content": "Flash sale!", "priority": "high"},
      {"recipient": "user@example.com", "channel": "email", "content": "Flash sale!", "priority": "normal"}
    ]
  }'
```

### Query Notifications

```bash
# Get by ID
curl http://localhost:8080/api/v1/notifications/{id}

# List with filters
curl "http://localhost:8080/api/v1/notifications?status=failed&channel=sms&page=1&page_size=20"

# List by batch
curl "http://localhost:8080/api/v1/notifications?batch_id={batch_id}"

# Date range
curl "http://localhost:8080/api/v1/notifications?date_from=2026-05-01T00:00:00Z&date_to=2026-05-02T00:00:00Z"
```

### Cancel a Notification

```bash
curl -X DELETE http://localhost:8080/api/v1/notifications/{id}
```

### Observability

```bash
# Health check
curl http://localhost:8080/health

# Queue depth summary
curl http://localhost:8080/api/v1/metrics/summary

# Prometheus metrics
curl http://localhost:8080/metrics
```

## Running Tests

```bash
# Unit tests only (no external dependencies)
go test ./internal/domain/... ./internal/template/... -v

# All tests (requires Postgres + Redis)
DATABASE_DSN="postgres://postgres:postgres@localhost:5432/notifications?sslmode=disable" \
REDIS_ADDR="localhost:6379" \
go test ./... -v -race

# With coverage
go test ./... -coverprofile=coverage.out && go tool cover -html=coverage.out
```

## Prometheus Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `notifications_delivered_total` | Counter | Successful deliveries by channel |
| `notifications_failed_total` | Counter | Failed deliveries by channel |
| `notification_delivery_duration_seconds` | Histogram | End-to-end delivery latency |
| `notification_queue_depth` | Gauge | Items in each priority queue |
| `notifications_retry_total` | Counter | Retry attempts by channel |

## Content Limits

| Channel | Max Length |
|---------|-----------|
| SMS | 160 characters |
| Email | 10,000 characters |
| Push | 256 characters |

## Project Structure

```
├── cmd/server/main.go          # Application entrypoint
├── internal/
│   ├── api/
│   │   ├── router.go           # Chi router setup
│   │   ├── handlers/           # HTTP handlers
│   │   └── middleware/         # Logging, correlation IDs, recovery
│   ├── config/                 # Environment-based configuration
│   ├── domain/                 # Core domain models and constants
│   ├── delivery/               # External provider HTTP client
│   ├── metrics/                # Prometheus metrics
│   ├── queue/                  # Redis priority queue + worker pool
│   ├── repository/             # PostgreSQL data access layer
│   ├── scheduler/              # Scheduled notification + retry scheduler
│   └── template/               # Template rendering with {{var}} syntax
├── migrations/                 # Versioned SQL migrations
├── docker-compose.yml
├── Dockerfile
├── swagger.yaml                # OpenAPI 3.0 specification
└── .github/workflows/ci.yml   # GitHub Actions CI
```
