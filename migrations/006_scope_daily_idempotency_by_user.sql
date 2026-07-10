ALTER TABLE actions
    ADD COLUMN IF NOT EXISTS idempotency_key TEXT;

ALTER TABLE people_notes
    ADD COLUMN IF NOT EXISTS idempotency_key TEXT;

DROP INDEX IF EXISTS idx_actions_idempotency_key;
DROP INDEX IF EXISTS idx_people_notes_idempotency_key;

CREATE UNIQUE INDEX IF NOT EXISTS idx_actions_user_idempotency_key
    ON actions (user_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_people_notes_user_idempotency_key
    ON people_notes (user_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
