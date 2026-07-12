ALTER TABLE notes
    ADD COLUMN IF NOT EXISTS processing_status TEXT NOT NULL DEFAULT 'pending',
    ADD COLUMN IF NOT EXISTS processing_started_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS processed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS processing_failed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS processing_attempts INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS processing_error TEXT,
    ADD COLUMN IF NOT EXISTS processing_model TEXT,
    ADD COLUMN IF NOT EXISTS processing_prompt_version TEXT;

UPDATE notes
SET processing_status = CASE
        WHEN summary IS NOT NULL AND btrim(summary) <> '' THEN 'processed'
        ELSE 'pending'
    END,
    processed_at = CASE
        WHEN summary IS NOT NULL AND btrim(summary) <> '' THEN COALESCE(processed_at, created_at)
        ELSE processed_at
    END
WHERE processing_status = 'pending'
  AND processed_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_notes_processing_lookup
    ON notes (processing_status, processing_started_at, created_at)
    WHERE processing_status IN ('pending', 'failed', 'processing');

CREATE INDEX IF NOT EXISTS idx_notes_user_processing
    ON notes (user_id, processing_status, created_at DESC);
