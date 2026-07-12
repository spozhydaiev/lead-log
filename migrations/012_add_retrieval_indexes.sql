-- Retrieval MVP indexes. Uses existing pg_trgm extension from 001_init.sql.
CREATE INDEX IF NOT EXISTS idx_notes_retrieval_text_trgm ON notes USING gin ((raw_text || ' ' || COALESCE(summary, '')) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_notes_retrieval_fts ON notes USING gin (to_tsvector('simple', raw_text || ' ' || COALESCE(summary, '')));
CREATE INDEX IF NOT EXISTS idx_actions_user_person_created ON actions(user_id, linked_person_id, created_at DESC) WHERE linked_person_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_people_notes_user_person_created ON people_notes(user_id, person_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_people_notes_user_type_created ON people_notes(user_id, type, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_decisions_user_person_created ON decisions(user_id, linked_person_id, created_at DESC) WHERE linked_person_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_decisions_user_status_created ON decisions(user_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_person_aliases_user_normalized ON person_aliases(user_id, normalized_alias);
