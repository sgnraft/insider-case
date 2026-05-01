-- Migration: 001_initial_schema
-- Creates the core tables for the notification system

BEGIN;

-- Notifications table
CREATE TABLE IF NOT EXISTS notifications (
    id               TEXT PRIMARY KEY,
    batch_id         TEXT NOT NULL DEFAULT '',
    recipient        TEXT NOT NULL,
    channel          TEXT NOT NULL CHECK (channel IN ('sms', 'email', 'push')),
    content          TEXT NOT NULL,
    priority         TEXT NOT NULL DEFAULT 'normal' CHECK (priority IN ('high', 'normal', 'low')),
    status           TEXT NOT NULL DEFAULT 'pending'
                       CHECK (status IN ('pending', 'processing', 'delivered', 'failed', 'cancelled', 'scheduled')),
    idempotency_key  TEXT UNIQUE,
    template_id      TEXT NOT NULL DEFAULT '',
    template_vars    JSONB,
    provider_message_id TEXT NOT NULL DEFAULT '',
    retry_count      INTEGER NOT NULL DEFAULT 0,
    max_retries      INTEGER NOT NULL DEFAULT 5,
    next_retry_at    TIMESTAMPTZ,
    scheduled_at     TIMESTAMPTZ,
    delivered_at     TIMESTAMPTZ,
    error_message    TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_notifications_status    ON notifications(status);
CREATE INDEX IF NOT EXISTS idx_notifications_channel   ON notifications(channel);
CREATE INDEX IF NOT EXISTS idx_notifications_batch_id  ON notifications(batch_id);
CREATE INDEX IF NOT EXISTS idx_notifications_created_at ON notifications(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_retry     ON notifications(status, retry_count, next_retry_at)
    WHERE status = 'failed';
CREATE INDEX IF NOT EXISTS idx_notifications_scheduled ON notifications(status, scheduled_at)
    WHERE status = 'scheduled';
CREATE INDEX IF NOT EXISTS idx_notifications_idempotency ON notifications(idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- Templates table
CREATE TABLE IF NOT EXISTS templates (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    channel    TEXT NOT NULL CHECK (channel IN ('sms', 'email', 'push')),
    content    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Automatic updated_at trigger
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_notifications_updated_at
    BEFORE UPDATE ON notifications
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_templates_updated_at
    BEFORE UPDATE ON templates
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

COMMIT;
