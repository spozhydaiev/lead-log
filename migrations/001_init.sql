CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS users (
                                     id BIGSERIAL PRIMARY KEY,
                                     telegram_user_id BIGINT UNIQUE NOT NULL,
                                     username TEXT,
                                     created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS people (
                                      id BIGSERIAL PRIMARY KEY,
                                      user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                                      name TEXT NOT NULL,
                                      aliases TEXT[] NOT NULL DEFAULT '{}',
                                      role TEXT,
                                      created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
                                      UNIQUE(user_id, name)
);

CREATE TABLE IF NOT EXISTS notes (
                                     id BIGSERIAL PRIMARY KEY,
                                     user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                                     raw_text TEXT NOT NULL,
                                     summary TEXT,
                                     tags TEXT[] NOT NULL DEFAULT '{}',
                                     created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS actions (
                                       id BIGSERIAL PRIMARY KEY,
                                       user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                                       note_id BIGINT REFERENCES notes(id) ON DELETE SET NULL,
                                       linked_person_id BIGINT REFERENCES people(id) ON DELETE SET NULL,
                                       title TEXT NOT NULL,
                                       status TEXT NOT NULL DEFAULT 'open',
                                       due_at TIMESTAMPTZ,
                                       output_type TEXT,
                                       source_note_ids BIGINT[] NOT NULL DEFAULT '{}',
                                       idempotency_key TEXT,
                                       created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
                                       completed_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS people_notes (
                                            id BIGSERIAL PRIMARY KEY,
                                            user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
                                            person_id BIGINT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
                                            note_id BIGINT REFERENCES notes(id) ON DELETE SET NULL,
                                            type TEXT NOT NULL,
                                            theme TEXT,
                                            text TEXT NOT NULL,
                                            include_in_review BOOLEAN NOT NULL DEFAULT true,
                                            source_note_ids BIGINT[] NOT NULL DEFAULT '{}',
                                            idempotency_key TEXT,
                                            created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_notes_user_created ON notes(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_actions_user_status ON actions(user_id, status, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_actions_idempotency_key ON actions(idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_people_notes_person_created ON people_notes(person_id, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_people_notes_idempotency_key ON people_notes(idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_people_name_trgm ON people USING gin (name gin_trgm_ops);
