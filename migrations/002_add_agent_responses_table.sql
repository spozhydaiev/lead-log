CREATE TABLE IF NOT EXISTS agent_responses (
                                               id BIGSERIAL PRIMARY KEY,

                                               user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,

                                               kind TEXT NOT NULL,
                                               scope_key TEXT NOT NULL,

                                               period_start TIMESTAMPTZ,
                                               period_end TIMESTAMPTZ,

                                               source_hash TEXT NOT NULL,
                                               prompt_version TEXT NOT NULL DEFAULT 'v1',
                                               model TEXT NOT NULL,

                                               response_text TEXT NOT NULL,
                                               response_json JSONB,

                                               created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
                                               last_used_at TIMESTAMPTZ NOT NULL DEFAULT now(),

                                               UNIQUE (user_id, kind, scope_key, prompt_version, source_hash)
);

CREATE INDEX IF NOT EXISTS idx_agent_responses_lookup
    ON agent_responses (user_id, kind, scope_key, prompt_version, created_at DESC);
