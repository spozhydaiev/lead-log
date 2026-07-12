CREATE TABLE IF NOT EXISTS decisions (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    note_id BIGINT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    text TEXT NOT NULL CHECK (btrim(text) <> ''),
    normalized_text TEXT NOT NULL CHECK (btrim(normalized_text) <> ''),
    linked_person_id BIGINT REFERENCES people(id) ON DELETE SET NULL,
    topic TEXT,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'superseded', 'reversed')),
    decided_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_decisions_user_decided_at ON decisions(user_id, decided_at DESC);
CREATE INDEX IF NOT EXISTS idx_decisions_note_id ON decisions(note_id);
CREATE INDEX IF NOT EXISTS idx_decisions_linked_person_id ON decisions(linked_person_id) WHERE linked_person_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS entity_mentions (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    note_id BIGINT NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    entity_type TEXT NOT NULL CHECK (entity_type IN ('ticket', 'project', 'service', 'component', 'repository', 'document', 'other')),
    raw_value TEXT NOT NULL CHECK (btrim(raw_value) <> ''),
    normalized_value TEXT NOT NULL CHECK (btrim(normalized_value) <> ''),
    display_value TEXT NOT NULL CHECK (btrim(display_value) <> ''),
    context TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(note_id, entity_type, normalized_value)
);

CREATE INDEX IF NOT EXISTS idx_entity_mentions_user_type_value ON entity_mentions(user_id, entity_type, normalized_value);
CREATE INDEX IF NOT EXISTS idx_entity_mentions_note_id ON entity_mentions(note_id);
