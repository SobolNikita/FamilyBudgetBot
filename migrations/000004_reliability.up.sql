ALTER TABLE telegram_updates
    ADD COLUMN message_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN delivery_suspended BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN delivery_touched_at TIMESTAMPTZ NOT NULL DEFAULT now();
UPDATE telegram_updates SET message_at=created_at, delivery_touched_at=created_at;
ALTER TABLE users
    ADD COLUMN pending_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN processing_at TIMESTAMPTZ NOT NULL DEFAULT 'epoch',
    ADD COLUMN delivery_at TIMESTAMPTZ NOT NULL DEFAULT 'epoch',
    ADD COLUMN delivery_paused BOOLEAN NOT NULL DEFAULT false;
CREATE INDEX expenses_user_recent ON expenses (user_id, id DESC);
CREATE INDEX telegram_updates_pending_user ON telegram_updates (user_id, update_id) WHERE status='pending';
CREATE INDEX telegram_updates_sent_cleanup ON telegram_updates (sent_at) WHERE status='sent';
CREATE INDEX dialog_states_expiry ON dialog_states (expires_at);
CREATE TABLE budget_settings (
    singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
    currency TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$')
);
CREATE TABLE expense_requests (
    request_key TEXT PRIMARY KEY,
    request_hash TEXT NOT NULL,
    response JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
