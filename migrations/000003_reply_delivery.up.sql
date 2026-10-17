ALTER TABLE telegram_updates
    ADD COLUMN message_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN reply_message_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN delivery_next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN delivery_error TEXT NOT NULL DEFAULT '';

CREATE INDEX telegram_updates_delivery ON telegram_updates (delivery_next_attempt_at, update_id)
    WHERE status IN ('pending', 'processed');
