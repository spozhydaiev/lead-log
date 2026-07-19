CREATE INDEX IF NOT EXISTS idx_entity_mentions_ticket_workspace
    ON entity_mentions(user_id, entity_type, normalized_value, note_id)
    WHERE entity_type = 'ticket';

CREATE INDEX IF NOT EXISTS idx_actions_user_note_status_open
    ON actions(user_id, note_id, created_at DESC, id DESC)
    WHERE status = 'open' AND note_id IS NOT NULL;
