package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/spozhydaiev/lead-log/internal/models"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
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
	return id, err
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
		personIDs[strings.ToLower(name)] = pid
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
			key := strings.ToLower(strings.TrimSpace(action.LinkedPersonName))
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
	var id int64
	err := tx.QueryRow(ctx, `
		INSERT INTO people (user_id, name)
		VALUES ($1, $2)
		ON CONFLICT (user_id, name)
		DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, userID, name).Scan(&id)
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

func (s *Store) GetPersonContext(ctx context.Context, userID int64, name string, since time.Time) (models.PersonContext, error) {
	var personID int64
	var canonicalName string
	err := s.pool.QueryRow(ctx, `
		SELECT id, name
		FROM people
		WHERE user_id = $1 AND lower(name) = lower($2)
		LIMIT 1
	`, userID, strings.TrimSpace(name)).Scan(&personID, &canonicalName)
	if err != nil {
		return models.PersonContext{}, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT type, COALESCE(theme, ''), text, COALESCE(note_id, 0), created_at
		FROM people_notes
		WHERE user_id = $1 AND person_id = $2 AND created_at >= $3
		ORDER BY created_at DESC
		LIMIT 50
	`, userID, personID, since)
	if err != nil {
		return models.PersonContext{}, err
	}
	defer rows.Close()

	ctxOut := models.PersonContext{PersonName: canonicalName}
	for rows.Next() {
		var n models.PersonContextNote
		if err := rows.Scan(&n.Type, &n.Theme, &n.Text, &n.NoteID, &n.CreatedAt); err != nil {
			return models.PersonContext{}, err
		}
		ctxOut.Notes = append(ctxOut.Notes, n)
	}

	actions, err := s.ListOpenActionsForPerson(ctx, userID, personID, 20)
	if err != nil {
		return models.PersonContext{}, err
	}
	ctxOut.Actions = actions
	return ctxOut, rows.Err()
}

func (s *Store) ListOpenActionsForPerson(ctx context.Context, userID, personID int64, limit int) ([]models.Action, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, title, status, COALESCE(output_type, ''), created_at
		FROM actions
		WHERE user_id = $1 AND linked_person_id = $2 AND status = 'open'
		ORDER BY created_at DESC
		LIMIT $3
	`, userID, personID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.Action
	for rows.Next() {
		var a models.Action
		if err := rows.Scan(&a.ID, &a.Title, &a.Status, &a.OutputType, &a.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

func (s *Store) RecentDailySource(ctx context.Context, userID int64, since time.Time) (string, error) {
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

	return err
}

func (s *Store) PersonSummarySource(ctx context.Context, userID int64, name string, since time.Time) (string, string, error) {
	pc, err := s.GetPersonContext(ctx, userID, name, since)
	if err != nil {
		return "", "", err
	}

	var b strings.Builder
	b.WriteString("Person: " + pc.PersonName + "\n\n")

	if len(pc.Actions) > 0 {
		b.WriteString("Open actions:\n")
		for _, a := range pc.Actions {
			b.WriteString(fmt.Sprintf("- #%d %s [%s]\n", a.ID, a.Title, a.OutputType))
		}
		b.WriteString("\n")
	}

	if len(pc.Notes) > 0 {
		b.WriteString("People notes:\n")
		for _, n := range pc.Notes {
			b.WriteString(fmt.Sprintf(
				"- %s | %s | %s | note #%d | %s\n",
				n.CreatedAt.Format("2006-01-02"),
				n.Type,
				n.Theme,
				n.Text,
				n.NoteID,
			))
		}
	}

	return pc.PersonName, b.String(), nil
}
