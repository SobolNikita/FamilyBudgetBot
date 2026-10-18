DROP INDEX users_removed;
ALTER TABLE expense_requests DROP COLUMN revoked;
ALTER TABLE users DROP COLUMN membership_version, DROP COLUMN removed_at;
