ALTER TABLE notes
    ADD COLUMN IF NOT EXISTS next_processing_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_notes_enrichment_queue
    ON notes (processing_status, next_processing_at, processing_attempts, created_at, id)
    WHERE processing_status IN ('pending', 'failed', 'processing');
