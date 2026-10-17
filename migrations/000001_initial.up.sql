CREATE TABLE users (
    id BIGINT PRIMARY KEY CHECK (id > 0),
    display_name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE expenses (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    amount NUMERIC(14,2) NOT NULL CHECK (amount > 0),
    category TEXT NOT NULL CHECK (category IN ('еда', 'транспорт', 'жильё', 'развлечения', 'здоровье', 'покупки', 'другое')),
    spent_at DATE NOT NULL,
    comment TEXT NOT NULL DEFAULT '' CHECK (char_length(comment) <= 1000),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX expenses_user_date ON expenses (user_id, spent_at DESC, id DESC);
CREATE INDEX expenses_date ON expenses (spent_at DESC, id DESC);
CREATE INDEX expenses_category_date ON expenses (category, spent_at);

CREATE TABLE dialog_states (
    user_id BIGINT PRIMARY KEY REFERENCES users(id),
    context JSONB NOT NULL DEFAULT '{}',
    pending_action JSONB,
    expires_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE telegram_updates (
    update_id BIGINT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    chat_id BIGINT NOT NULL,
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processed', 'sent', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    send_attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    reply_text TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ,
    sent_at TIMESTAMPTZ
);
CREATE INDEX telegram_updates_queue ON telegram_updates (status, next_attempt_at, update_id);
CREATE INDEX telegram_updates_user ON telegram_updates (user_id, update_id);

