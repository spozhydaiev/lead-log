-- Multi-user web authentication and Telegram identity linking.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM users GROUP BY telegram_user_id HAVING count(*) > 1) THEN
        RAISE EXCEPTION 'duplicate telegram_user_id values exist in users';
    END IF;
END $$;

ALTER TABLE users ADD COLUMN IF NOT EXISTS display_name TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS timezone TEXT NOT NULL DEFAULT 'Europe/Warsaw';
ALTER TABLE users ADD COLUMN IF NOT EXISTS response_language TEXT NOT NULL DEFAULT 'en';
ALTER TABLE users ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE users ALTER COLUMN telegram_user_id DROP NOT NULL;

UPDATE users SET display_name = COALESCE(display_name, NULLIF(username, ''));

CREATE TABLE IF NOT EXISTS auth_identities (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    provider_subject TEXT,
    email TEXT,
    email_normalized TEXT,
    email_verified BOOLEAN NOT NULL DEFAULT false,
    password_hash TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT auth_identities_provider_check CHECK (provider IN ('local')),
    CONSTRAINT auth_identities_local_email_required CHECK (provider <> 'local' OR (email_normalized IS NOT NULL AND password_hash IS NOT NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_auth_identities_local_email_normalized ON auth_identities(email_normalized) WHERE provider='local';
CREATE INDEX IF NOT EXISTS idx_auth_identities_user ON auth_identities(user_id);

CREATE TABLE IF NOT EXISTS telegram_accounts (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    telegram_user_id BIGINT NOT NULL UNIQUE,
    telegram_chat_id BIGINT,
    linked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_telegram_accounts_user ON telegram_accounts(user_id);

INSERT INTO telegram_accounts (user_id, telegram_user_id, linked_at, created_at, updated_at)
SELECT id, telegram_user_id, created_at, created_at, updated_at
FROM users
WHERE telegram_user_id IS NOT NULL
ON CONFLICT (telegram_user_id) DO NOTHING;

CREATE TABLE IF NOT EXISTS web_sessions (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_web_sessions_active_lookup ON web_sessions(token_hash, expires_at) WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_web_sessions_cleanup ON web_sessions(expires_at, revoked_at);
CREATE INDEX IF NOT EXISTS idx_web_sessions_user ON web_sessions(user_id);

CREATE TABLE IF NOT EXISTS telegram_link_tokens (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_telegram_link_tokens_active ON telegram_link_tokens(token_hash, expires_at) WHERE used_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_telegram_link_tokens_user_unused ON telegram_link_tokens(user_id, created_at) WHERE used_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_telegram_link_tokens_cleanup ON telegram_link_tokens(expires_at, used_at);
