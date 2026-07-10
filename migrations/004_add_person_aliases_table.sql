CREATE TABLE IF NOT EXISTS person_aliases (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    person_id BIGINT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
    alias TEXT NOT NULL,
    normalized_alias TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(user_id, normalized_alias)
);

CREATE INDEX IF NOT EXISTS idx_person_aliases_person ON person_aliases(person_id);

INSERT INTO person_aliases (user_id, person_id, alias, normalized_alias)
SELECT user_id, id, name, lower(regexp_replace(trim(name), '\s+', ' ', 'g'))
FROM people
ON CONFLICT (user_id, normalized_alias) DO NOTHING;
