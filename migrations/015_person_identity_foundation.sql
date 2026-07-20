ALTER TABLE people
    ADD COLUMN IF NOT EXISTS first_name TEXT,
    ADD COLUMN IF NOT EXISTS last_name TEXT,
    ADD COLUMN IF NOT EXISTS job_title TEXT,
    ADD COLUMN IF NOT EXISTS team TEXT,
    ADD COLUMN IF NOT EXISTS company TEXT,
    ADD COLUMN IF NOT EXISTS notes TEXT,
    ADD COLUMN IF NOT EXISTS normalized_name TEXT,
    ADD COLUMN IF NOT EXISTS merged_into_person_id BIGINT NULL REFERENCES people(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- This matches the current Go NormalizePersonName behavior for persisted names:
-- trim surrounding whitespace, collapse repeated whitespace, lowercase, and normalize supported apostrophe variants.
UPDATE people
SET normalized_name = lower(regexp_replace(trim(replace(replace(replace(replace(replace(replace(name, '’', chr(39)), '‘', chr(39)), '`', chr(39)), '´', chr(39)), 'ʼ', chr(39)), 'ʹ', chr(39))), '\s+', ' ', 'g'))
WHERE normalized_name IS NULL AND trim(name) <> '';

CREATE INDEX IF NOT EXISTS idx_people_user_active_normalized_name
    ON people(user_id, normalized_name)
    WHERE merged_into_person_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_people_user_merged_into
    ON people(user_id, merged_into_person_id)
    WHERE merged_into_person_id IS NOT NULL;

-- A unique active-person normalized-name index is intentionally deferred: historical rows may
-- already contain duplicate normalized names per user. This step relies on person_aliases
-- uniqueness plus transactional alias ownership to prevent new orphan duplicates.
