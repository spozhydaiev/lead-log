CREATE TABLE IF NOT EXISTS telegram_updates (
    id BIGSERIAL PRIMARY KEY,
    telegram_update_id BIGINT NOT NULL,
    telegram_chat_id BIGINT NOT NULL,
    telegram_message_id BIGINT NOT NULL,
    telegram_user_id BIGINT NOT NULL,
    user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    status TEXT NOT NULL CHECK (status IN ('processing', 'processed', 'failed')),
    command TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    processing_started_at TIMESTAMPTZ,
    processed_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    last_error TEXT,
    attempt_count INTEGER NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_telegram_updates_update_id
    ON telegram_updates (telegram_update_id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_telegram_updates_chat_message
    ON telegram_updates (telegram_chat_id, telegram_message_id);

CREATE INDEX IF NOT EXISTS idx_telegram_updates_status_processing_started
    ON telegram_updates (status, processing_started_at);
