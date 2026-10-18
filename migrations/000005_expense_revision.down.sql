DROP TRIGGER expenses_revision ON expenses;
DROP FUNCTION bump_expense_revision();
ALTER TABLE users DROP COLUMN expense_revision;
