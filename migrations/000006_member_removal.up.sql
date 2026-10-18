ALTER TABLE users
    ADD COLUMN removed_at TIMESTAMPTZ,
    ADD COLUMN membership_version INTEGER NOT NULL DEFAULT 1 CHECK (membership_version > 0);
ALTER TABLE expense_requests ADD COLUMN revoked BOOLEAN NOT NULL DEFAULT false;
CREATE INDEX users_removed ON users (removed_at) WHERE removed_at IS NOT NULL;
