-- Notes history API pagination/filter indexes.
CREATE INDEX IF NOT EXISTS idx_notes_user_created_id
    ON notes(user_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_notes_user_status_created_id
    ON notes(user_id, processing_status, created_at DESC, id DESC);

-- pg_trgm is created in the initial migration; keep this migration safe for partially migrated databases.
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX IF NOT EXISTS idx_notes_history_text_trgm
    ON notes USING gin ((raw_text || ' ' || COALESCE(summary, '')) gin_trgm_ops);
