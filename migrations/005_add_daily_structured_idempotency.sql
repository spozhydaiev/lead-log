ALTER TABLE actions
    ADD COLUMN IF NOT EXISTS source_note_ids BIGINT[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS idempotency_key TEXT;

ALTER TABLE people_notes
    ADD COLUMN IF NOT EXISTS source_note_ids BIGINT[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS idempotency_key TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_actions_idempotency_key
    ON actions (idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_people_notes_idempotency_key
    ON people_notes (idempotency_key)
    WHERE idempotency_key IS NOT NULL;
