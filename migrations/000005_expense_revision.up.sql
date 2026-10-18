ALTER TABLE users ADD COLUMN expense_revision BIGINT NOT NULL DEFAULT 1;

-- Invalidate a conversation's pagination snapshot after any expense mutation,
-- including changes made by another client and changes to the sorting key.
CREATE FUNCTION bump_expense_revision() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        UPDATE users SET expense_revision = expense_revision + 1 WHERE id = NEW.user_id;
    ELSIF TG_OP = 'DELETE' THEN
        UPDATE users SET expense_revision = expense_revision + 1 WHERE id = OLD.user_id;
    ELSE
        PERFORM id FROM users WHERE id IN (OLD.user_id, NEW.user_id) ORDER BY id FOR UPDATE;
        UPDATE users SET expense_revision = expense_revision + 1 WHERE id IN (OLD.user_id, NEW.user_id);
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER expenses_revision AFTER INSERT OR UPDATE OR DELETE ON expenses
    FOR EACH ROW EXECUTE FUNCTION bump_expense_revision();
