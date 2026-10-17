DROP INDEX telegram_updates_delivery;
ALTER TABLE telegram_updates
    DROP COLUMN delivery_error,
    DROP COLUMN delivery_next_attempt_at,
    DROP COLUMN reply_message_id,
    DROP COLUMN message_id;
