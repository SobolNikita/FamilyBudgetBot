DROP TABLE expense_requests;
DROP TABLE budget_settings;
DROP INDEX dialog_states_expiry;
DROP INDEX telegram_updates_sent_cleanup;
DROP INDEX telegram_updates_pending_user;
DROP INDEX expenses_user_recent;
ALTER TABLE users DROP COLUMN pending_name, DROP COLUMN processing_at, DROP COLUMN delivery_at, DROP COLUMN delivery_paused;
ALTER TABLE telegram_updates DROP COLUMN message_at, DROP COLUMN delivery_suspended, DROP COLUMN delivery_touched_at;
