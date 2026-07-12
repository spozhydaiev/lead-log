package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/spozhydaiev/lead-log/internal/models"
)

const (
	TelegramUpdateStatusProcessing = "processing"
	TelegramUpdateStatusProcessed  = "processed"
	TelegramUpdateStatusFailed     = "failed"
	TelegramUpdateMaxAttempts      = 3
)

type TelegramUpdateMeta struct {
	UpdateID       int64
	ChatID         int64
	MessageID      int64
	TelegramUserID int64
	UserID         int64
	Command        string
}

type TelegramUpdateClaim struct {
	Claimed             bool
	Status              string
	AttemptCount        int
	StaleReclaimed      bool
	ProcessingStartedAt time.Time
}

type Store struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

func New(pool *pgxpool.Pool, logger ...*slog.Logger) *Store {
	l := slog.Default()
	if len(logger) > 0 && logger[0] != nil {
		l = logger[0]
	}
	return &Store{pool: pool, logger: l}
}

func (s *Store) logDBError(operation string, err error) {
	if err != nil {
		s.logger.Error("database error", "operation", operation, "error", err)
	}
}

func (s *Store) UpsertUser(ctx context.Context, telegramUserID int64, username string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (telegram_user_id, username)
		VALUES ($1, $2)
		ON CONFLICT (telegram_user_id)
		DO UPDATE SET username = EXCLUDED.username
		RETURNING id
	`, telegramUserID, username).Scan(&id)
	if err != nil {
		s.logDBError("store.upsert_user", err)
	}
	return id, err
}

func (s *Store) SaveRawNote(ctx context.Context, userID int64, raw string) (int64, error) {
	var noteID int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO notes (user_id, raw_text)
		VALUES ($1, $2)
		RETURNING id
	`, userID, raw).Scan(&noteID)
	if err != nil {
		s.logDBError("store.save_raw_note", err)
	}
	return noteID, err
}

func (s *Store) SaveParsedNote(ctx context.Context, userID int64, raw string, parsed models.ParsedNote) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var noteID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO notes (user_id, raw_text, summary, tags)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, userID, raw, parsed.Summary, parsed.Tags).Scan(&noteID); err != nil {
		return 0, err
	}

	personIDs := map[string]int64{}
	for _, pn := range parsed.PeopleNotes {
		name := strings.TrimSpace(pn.PersonName)
		if name == "" {
			continue
		}
		pid, err := upsertPerson(ctx, tx, userID, name)
		if err != nil {
			return 0, err
		}
		personIDs[NormalizePersonName(name)] = pid
		if _, err := tx.Exec(ctx, `
			INSERT INTO people_notes (user_id, person_id, note_id, type, theme, text, include_in_review)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, userID, pid, noteID, emptyTo(pn.Type, "context"), pn.Theme, pn.Text, pn.IncludeInReview); err != nil {
			return 0, err
		}
	}

	for _, action := range parsed.Actions {
		title := strings.TrimSpace(action.Title)
		if title == "" {
			continue
		}
		var linkedPersonID *int64
		if action.LinkedPersonName != "" {
			key := NormalizePersonName(action.LinkedPersonName)
			pid, ok := personIDs[key]
			if !ok {
				pid, err = upsertPerson(ctx, tx, userID, action.LinkedPersonName)
				if err != nil {
					return 0, err
				}
			}
			linkedPersonID = &pid
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO actions (user_id, note_id, linked_person_id, title, output_type)
			VALUES ($1, $2, $3, $4, $5)
		`, userID, noteID, linkedPersonID, title, action.OutputType); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return noteID, nil
}

func upsertPerson(ctx context.Context, tx pgx.Tx, userID int64, name string) (int64, error) {
	name = strings.TrimSpace(name)
	normalized := NormalizePersonName(name)
	if normalized == "" {
		return 0, fmt.Errorf("person name is empty")
	}

	var id int64
	err := tx.QueryRow(ctx, `
		SELECT person_id
		FROM person_aliases
		WHERE user_id = $1 AND normalized_alias = $2
		LIMIT 1
	`, userID, normalized).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO people (user_id, name)
		VALUES ($1, $2)
		ON CONFLICT (user_id, name)
		DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, userID, name).Scan(&id)
	if err != nil {
		return 0, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO person_aliases (user_id, person_id, alias, normalized_alias)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, normalized_alias) DO NOTHING
	`, userID, id, name, normalized)
	return id, err
}

func (s *Store) ListOpenActions(ctx context.Context, userID int64, limit int) ([]models.Action, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.title, a.status, COALESCE(a.output_type, ''), a.created_at, p.name
		FROM actions a
		LEFT JOIN people p ON p.id = a.linked_person_id
		WHERE a.user_id = $1 AND a.status = 'open'
		ORDER BY a.created_at DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.Action
	for rows.Next() {
		var a models.Action
		var personName *string
		if err := rows.Scan(&a.ID, &a.Title, &a.Status, &a.OutputType, &a.CreatedAt, &personName); err != nil {
			return nil, err
		}
		a.PersonName = personName
		result = append(result, a)
	}
	return result, rows.Err()
}

func (s *Store) MarkActionDone(ctx context.Context, userID, actionID int64) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE actions
		SET status = 'done', completed_at = now()
		WHERE user_id = $1 AND id = $2 AND status = 'open'
	`, userID, actionID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("action not found or already done")
	}
	return nil
}

func (s *Store) RecentDailySource(ctx context.Context, userID int64, since, before time.Time) (string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT n.id, n.created_at, n.raw_text, COALESCE(n.summary, '')
		FROM notes n
		WHERE n.user_id = $1 AND n.created_at >= $2 AND n.created_at < $3
		ORDER BY n.created_at ASC
	`, userID, since, before)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var b strings.Builder
	for rows.Next() {
		var id int64
		var created time.Time
		var raw, summary string
		if err := rows.Scan(&id, &created, &raw, &summary); err != nil {
			return "", err
		}
		b.WriteString(fmt.Sprintf("Note #%d at %s\nSummary: %s\nRaw: %s\n\n", id, created.Format(time.RFC3339), summary, raw))
	}
	return b.String(), rows.Err()
}

func (s *Store) RecentWeeklySource(ctx context.Context, userID int64, since time.Time) (string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT n.id, n.created_at, n.raw_text, COALESCE(n.summary, '')
		FROM notes n
		WHERE n.user_id = $1 AND n.created_at >= $2
		ORDER BY n.created_at ASC
	`, userID, since)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var b strings.Builder
	for rows.Next() {
		var id int64
		var created time.Time
		var raw, summary string
		if err := rows.Scan(&id, &created, &raw, &summary); err != nil {
			return "", err
		}
		b.WriteString(fmt.Sprintf("Note #%d at %s\nSummary: %s\nRaw: %s\n\n", id, created.Format(time.RFC3339), summary, raw))
	}
	return b.String(), rows.Err()
}

func emptyTo(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func (s *Store) GetCachedAgentResponse(
	ctx context.Context,
	userID int64,
	kind string,
	scopeKey string,
	sourceHash string,
	promptVersion string,
) (*models.AgentResponse, error) {
	var r models.AgentResponse

	err := s.pool.QueryRow(ctx, `
		UPDATE agent_responses
		SET last_used_at = now()
		WHERE id = (
			SELECT id
			FROM agent_responses
			WHERE user_id = $1
			  AND kind = $2
			  AND scope_key = $3
			  AND source_hash = $4
			  AND prompt_version = $5
			ORDER BY created_at DESC
			LIMIT 1
		)
		RETURNING
			id,
			user_id,
			kind,
			scope_key,
			period_start,
			period_end,
			source_hash,
			prompt_version,
			model,
			response_text,
			COALESCE(response_json::text, ''),
			created_at,
			last_used_at
	`, userID, kind, scopeKey, sourceHash, promptVersion).Scan(
		&r.ID,
		&r.UserID,
		&r.Kind,
		&r.ScopeKey,
		&r.PeriodStart,
		&r.PeriodEnd,
		&r.SourceHash,
		&r.PromptVersion,
		&r.Model,
		&r.ResponseText,
		&r.ResponseJSON,
		&r.CreatedAt,
		&r.LastUsedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &r, nil
}

func (s *Store) SaveAgentResponse(ctx context.Context, r models.AgentResponse) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO agent_responses (
			user_id,
			kind,
			scope_key,
			period_start,
			period_end,
			source_hash,
			prompt_version,
			model,
			response_text,
			response_json
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, '')::jsonb)
		ON CONFLICT (user_id, kind, scope_key, prompt_version, source_hash)
		DO UPDATE SET
			response_text = EXCLUDED.response_text,
			response_json = EXCLUDED.response_json,
			last_used_at = now()
	`, r.UserID,
		r.Kind,
		r.ScopeKey,
		r.PeriodStart,
		r.PeriodEnd,
		r.SourceHash,
		r.PromptVersion,
		r.Model,
		r.ResponseText,
		r.ResponseJSON,
	)

	if err != nil {
		s.logDBError("store.save_agent_response", err)
	}
	return err
}

func (s *Store) HasDailySummarySend(ctx context.Context, userID int64, scopeKey string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM daily_summary_sends
			WHERE user_id = $1 AND scope_key = $2
		)
	`, userID, scopeKey).Scan(&exists)
	if err != nil {
		s.logDBError("store.has_daily_summary_send", err)
	}
	return exists, err
}

func (s *Store) RecordDailySummarySend(ctx context.Context, userID int64, scopeKey string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO daily_summary_sends (user_id, scope_key)
		VALUES ($1, $2)
		ON CONFLICT (user_id, scope_key) DO NOTHING
	`, userID, scopeKey)
	if err != nil {
		s.logDBError("store.record_daily_summary_send", err)
	}
	return err
}

func (s *Store) ClaimTelegramUpdate(ctx context.Context, meta TelegramUpdateMeta, staleAfter time.Duration) (TelegramUpdateClaim, error) {
	var claim TelegramUpdateClaim
	if meta.UpdateID == 0 && (meta.ChatID == 0 || meta.MessageID == 0) {
		return claim, fmt.Errorf("telegram update identity is empty")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return claim, err
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx, `
		INSERT INTO telegram_updates (telegram_update_id, telegram_chat_id, telegram_message_id, telegram_user_id, user_id, status, command, processing_started_at, attempt_count)
		VALUES ($1,$2,$3,$4,$5,'processing',$6,now(),1)
		ON CONFLICT DO NOTHING
		RETURNING status, attempt_count, processing_started_at
	`, meta.UpdateID, meta.ChatID, meta.MessageID, meta.TelegramUserID, meta.UserID, meta.Command).Scan(&claim.Status, &claim.AttemptCount, &claim.ProcessingStartedAt)
	if err == nil {
		claim.Claimed = true
		if err := tx.Commit(ctx); err != nil {
			return TelegramUpdateClaim{}, err
		}
		return claim, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return claim, err
	}

	staleSeconds := int64(staleAfter.Seconds())
	err = tx.QueryRow(ctx, `
		UPDATE telegram_updates
		SET status='processing', processing_started_at=now(), failed_at=NULL, last_error=NULL, attempt_count=attempt_count+1, command=$6, user_id=$5
		WHERE (telegram_update_id=$1 OR (telegram_chat_id=$2 AND telegram_message_id=$3))
		  AND status IN ('failed','processing')
		  AND attempt_count < $7
		  AND (status='failed' OR processing_started_at < now() - make_interval(secs => $8))
		RETURNING status, attempt_count, processing_started_at, (status='processing')
	`, meta.UpdateID, meta.ChatID, meta.MessageID, meta.TelegramUserID, meta.UserID, meta.Command, TelegramUpdateMaxAttempts, staleSeconds).Scan(&claim.Status, &claim.AttemptCount, &claim.ProcessingStartedAt, &claim.StaleReclaimed)
	if err == nil {
		claim.Claimed = true
		if err := tx.Commit(ctx); err != nil {
			return TelegramUpdateClaim{}, err
		}
		return claim, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return claim, err
	}

	err = tx.QueryRow(ctx, `SELECT status, attempt_count, processing_started_at FROM telegram_updates WHERE telegram_update_id=$1 OR (telegram_chat_id=$2 AND telegram_message_id=$3)`, meta.UpdateID, meta.ChatID, meta.MessageID).Scan(&claim.Status, &claim.AttemptCount, &claim.ProcessingStartedAt)
	if err != nil {
		return claim, err
	}
	return claim, tx.Commit(ctx)
}

func (s *Store) MarkTelegramUpdateProcessed(ctx context.Context, meta TelegramUpdateMeta, startedAt time.Time) error {
	tag, err := s.pool.Exec(ctx, `UPDATE telegram_updates SET status='processed', processed_at=now(), last_error=NULL WHERE (telegram_update_id=$1 OR (telegram_chat_id=$3 AND telegram_message_id=$4)) AND status='processing' AND processing_started_at=$2`, meta.UpdateID, startedAt, meta.ChatID, meta.MessageID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("telegram update claim no longer active")
	}
	return nil
}

func (s *Store) MarkTelegramUpdateFailed(ctx context.Context, meta TelegramUpdateMeta, startedAt time.Time, cause error) error {
	msg := ""
	if cause != nil {
		msg = cause.Error()
		if len(msg) > 500 {
			msg = msg[:500]
		}
	}
	_, err := s.pool.Exec(ctx, `UPDATE telegram_updates SET status='failed', failed_at=now(), last_error=$3 WHERE (telegram_update_id=$1 OR (telegram_chat_id=$4 AND telegram_message_id=$5)) AND status='processing' AND processing_started_at=$2`, meta.UpdateID, startedAt, msg, meta.ChatID, meta.MessageID)
	return err
}

func (s *Store) SaveRawNoteAndMarkTelegramUpdateProcessed(ctx context.Context, userID int64, raw string, meta TelegramUpdateMeta, startedAt time.Time) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var noteID int64
	if err := tx.QueryRow(ctx, `INSERT INTO notes (user_id, raw_text) VALUES ($1,$2) RETURNING id`, userID, raw).Scan(&noteID); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `UPDATE telegram_updates SET status='processed', processed_at=now(), last_error=NULL WHERE (telegram_update_id=$1 OR (telegram_chat_id=$3 AND telegram_message_id=$4)) AND status='processing' AND processing_started_at=$2`, meta.UpdateID, startedAt, meta.ChatID, meta.MessageID)
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() == 0 {
		return 0, fmt.Errorf("telegram update claim no longer active")
	}
	return noteID, tx.Commit(ctx)
}

func (s *Store) SaveParsedNoteAndMarkTelegramUpdateProcessed(ctx context.Context, userID int64, raw string, parsed models.ParsedNote, meta TelegramUpdateMeta, startedAt time.Time) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	noteID, err := saveParsedNoteTx(ctx, tx, userID, raw, parsed)
	if err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `UPDATE telegram_updates SET status='processed', processed_at=now(), last_error=NULL WHERE (telegram_update_id=$1 OR (telegram_chat_id=$3 AND telegram_message_id=$4)) AND status='processing' AND processing_started_at=$2`, meta.UpdateID, startedAt, meta.ChatID, meta.MessageID)
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() == 0 {
		return 0, fmt.Errorf("telegram update claim no longer active")
	}
	return noteID, tx.Commit(ctx)
}

func (s *Store) MarkActionDoneAndMarkTelegramUpdateProcessed(ctx context.Context, userID, actionID int64, meta TelegramUpdateMeta, startedAt time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE actions SET status='done', completed_at=now() WHERE user_id=$1 AND id=$2 AND status='open'`, userID, actionID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("action not found or already done")
	}
	tag, err = tx.Exec(ctx, `UPDATE telegram_updates SET status='processed', processed_at=now(), last_error=NULL WHERE (telegram_update_id=$1 OR (telegram_chat_id=$3 AND telegram_message_id=$4)) AND status='processing' AND processing_started_at=$2`, meta.UpdateID, startedAt, meta.ChatID, meta.MessageID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("telegram update claim no longer active")
	}
	return tx.Commit(ctx)
}

func saveParsedNoteTx(ctx context.Context, tx pgx.Tx, userID int64, raw string, parsed models.ParsedNote) (int64, error) {
	var noteID int64
	if err := tx.QueryRow(ctx, `INSERT INTO notes (user_id, raw_text, summary, tags) VALUES ($1,$2,$3,$4) RETURNING id`, userID, raw, parsed.Summary, parsed.Tags).Scan(&noteID); err != nil {
		return 0, err
	}
	personIDs := map[string]int64{}
	for _, pn := range parsed.PeopleNotes {
		name := strings.TrimSpace(pn.PersonName)
		if name == "" {
			continue
		}
		pid, err := upsertPerson(ctx, tx, userID, name)
		if err != nil {
			return 0, err
		}
		personIDs[NormalizePersonName(name)] = pid
		if _, err := tx.Exec(ctx, `INSERT INTO people_notes (user_id, person_id, note_id, type, theme, text, include_in_review) VALUES ($1,$2,$3,$4,$5,$6,$7)`, userID, pid, noteID, emptyTo(pn.Type, "context"), pn.Theme, pn.Text, pn.IncludeInReview); err != nil {
			return 0, err
		}
	}
	for _, action := range parsed.Actions {
		title := strings.TrimSpace(action.Title)
		if title == "" {
			continue
		}
		var linkedPersonID *int64
		if action.LinkedPersonName != "" {
			key := NormalizePersonName(action.LinkedPersonName)
			pid, ok := personIDs[key]
			var err error
			if !ok {
				pid, err = upsertPerson(ctx, tx, userID, action.LinkedPersonName)
				if err != nil {
					return 0, err
				}
			}
			linkedPersonID = &pid
		}
		if _, err := tx.Exec(ctx, `INSERT INTO actions (user_id, note_id, linked_person_id, title, output_type) VALUES ($1,$2,$3,$4,$5)`, userID, noteID, linkedPersonID, title, action.OutputType); err != nil {
			return 0, err
		}
	}
	return noteID, nil
}
