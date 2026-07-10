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

func (s *Store) SaveRawNote(ctx context.Context, userID int64, raw string) (int64, error) {
	var noteID int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO notes (user_id, raw_text)
		VALUES ($1, $2)
		RETURNING id
	`, userID, raw).Scan(&noteID)
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

func (s *Store) GetPersonContext(ctx context.Context, userID int64, name string, since time.Time) (models.PersonContext, error) {
	var personID int64
	var canonicalName string
	err := s.pool.QueryRow(ctx, `
		SELECT p.id, p.name
		FROM people p
		LEFT JOIN person_aliases pa ON pa.person_id = p.id
		WHERE p.user_id = $1
		  AND (pa.normalized_alias = $2)
		LIMIT 1
	`, userID, NormalizePersonName(name)).Scan(&personID, &canonicalName)
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

type PersonListItem struct {
	ID      int64
	Name    string
	Aliases []string
}

func (s *Store) ListPeople(ctx context.Context, userID int64) ([]PersonListItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.name, COALESCE(array_remove(array_agg(pa.alias ORDER BY pa.alias), NULL), '{}')
		FROM people p
		LEFT JOIN person_aliases pa
		  ON pa.person_id = p.id
		 AND pa.normalized_alias <> lower(regexp_replace(trim(p.name), '\s+', ' ', 'g'))
		WHERE p.user_id = $1
		GROUP BY p.id, p.name
		ORDER BY lower(p.name)
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var people []PersonListItem
	for rows.Next() {
		var p PersonListItem
		if err := rows.Scan(&p.ID, &p.Name, &p.Aliases); err != nil {
			return nil, err
		}
		people = append(people, p)
	}
	return people, rows.Err()
}

func (s *Store) AddPersonAlias(ctx context.Context, userID int64, alias, canonicalName string) (string, error) {
	alias = strings.TrimSpace(alias)
	canonicalName = strings.TrimSpace(canonicalName)
	if alias == "" || canonicalName == "" {
		return "", fmt.Errorf("alias and canonical name are required")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	personID, err := upsertPerson(ctx, tx, userID, canonicalName)
	if err != nil {
		return "", err
	}
	if err := addAlias(ctx, tx, userID, personID, alias); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return canonicalName, nil
}

func (s *Store) MergePeople(ctx context.Context, userID int64, sourceName, targetName string) (string, error) {
	sourceName = strings.TrimSpace(sourceName)
	targetName = strings.TrimSpace(targetName)
	if sourceName == "" || targetName == "" {
		return "", fmt.Errorf("source and target person are required")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	sourceID, sourceCanonical, err := findPerson(ctx, tx, userID, sourceName)
	if err != nil {
		return "", fmt.Errorf("source person not found: %w", err)
	}
	targetID, err := upsertPerson(ctx, tx, userID, targetName)
	if err != nil {
		return "", err
	}
	if sourceID == targetID {
		if err := addAlias(ctx, tx, userID, targetID, sourceName); err != nil {
			return "", err
		}
		if err := tx.Commit(ctx); err != nil {
			return "", err
		}
		return targetName, nil
	}

	if _, err := tx.Exec(ctx, `UPDATE people_notes SET person_id = $1 WHERE user_id = $2 AND person_id = $3`, targetID, userID, sourceID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE actions SET linked_person_id = $1 WHERE user_id = $2 AND linked_person_id = $3`, targetID, userID, sourceID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO person_aliases (user_id, person_id, alias, normalized_alias)
		SELECT user_id, $1, alias, normalized_alias
		FROM person_aliases
		WHERE user_id = $2 AND person_id = $3
		ON CONFLICT (user_id, normalized_alias) DO NOTHING
	`, targetID, userID, sourceID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM person_aliases WHERE user_id = $1 AND person_id = $2`, userID, sourceID); err != nil {
		return "", err
	}
	if err := addAlias(ctx, tx, userID, targetID, sourceCanonical); err != nil {
		return "", err
	}
	if err := addAlias(ctx, tx, userID, targetID, sourceName); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM people WHERE user_id = $1 AND id = $2`, userID, sourceID); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return targetName, nil
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

func (s *Store) PersistDailyStructured(ctx context.Context, userID int64, start, end time.Time, parsed models.ParsedNote) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	personIDs := map[string]int64{}
	ensurePerson := func(name string) (int64, error) {
		name = strings.TrimSpace(name)
		key := NormalizePersonName(name)
		if key == "" {
			return 0, fmt.Errorf("person name is empty")
		}
		if id, ok := personIDs[key]; ok {
			return id, nil
		}
		id, err := upsertPerson(ctx, tx, userID, name)
		if err != nil {
			return 0, err
		}
		personIDs[key] = id
		return id, nil
	}

	for _, name := range parsed.PeopleMentioned {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if _, err := ensurePerson(name); err != nil {
			return err
		}
	}

	for _, pn := range parsed.PeopleNotes {
		name := strings.TrimSpace(pn.PersonName)
		text := strings.TrimSpace(pn.Text)
		if name == "" || text == "" {
			continue
		}
		pid, err := ensurePerson(name)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO people_notes (user_id, person_id, note_id, type, theme, text, include_in_review)
			SELECT $1, $2, NULL, $3, $4, $5, $6
			WHERE NOT EXISTS (
				SELECT 1 FROM people_notes
				WHERE user_id = $1
				  AND person_id = $2
				  AND note_id IS NULL
				  AND type = $3
				  AND COALESCE(theme, '') = COALESCE($4, '')
				  AND text = $5
				  AND created_at >= $7 AND created_at < $8
			)
		`, userID, pid, emptyTo(pn.Type, "context"), pn.Theme, text, pn.IncludeInReview, start, end); err != nil {
			return err
		}
	}

	for _, action := range parsed.Actions {
		title := strings.TrimSpace(action.Title)
		if title == "" {
			continue
		}
		var linkedPersonID *int64
		if strings.TrimSpace(action.LinkedPersonName) != "" {
			pid, err := ensurePerson(action.LinkedPersonName)
			if err != nil {
				return err
			}
			linkedPersonID = &pid
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO actions (user_id, note_id, linked_person_id, title, output_type)
			SELECT $1, NULL, $2, $3, $4
			WHERE NOT EXISTS (
				SELECT 1 FROM actions
				WHERE user_id = $1
				  AND note_id IS NULL
				  AND title = $3
				  AND COALESCE(output_type, '') = COALESCE($4, '')
				  AND COALESCE(linked_person_id, 0) = COALESCE($2, 0)
				  AND created_at >= $5 AND created_at < $6
			)
		`, userID, linkedPersonID, title, action.OutputType, start, end); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
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
				n.NoteID,
				n.Text,
			))
		}
	}

	return pc.PersonName, b.String(), nil
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
	return exists, err
}

func (s *Store) RecordDailySummarySend(ctx context.Context, userID int64, scopeKey string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO daily_summary_sends (user_id, scope_key)
		VALUES ($1, $2)
		ON CONFLICT (user_id, scope_key) DO NOTHING
	`, userID, scopeKey)
	return err
}

func findPerson(ctx context.Context, tx pgx.Tx, userID int64, name string) (int64, string, error) {
	var id int64
	var canonicalName string
	err := tx.QueryRow(ctx, `
		SELECT p.id, p.name
		FROM people p
		LEFT JOIN person_aliases pa ON pa.person_id = p.id
		WHERE p.user_id = $1 AND (pa.normalized_alias = $2 OR lower(p.name) = $2)
		LIMIT 1
	`, userID, NormalizePersonName(name)).Scan(&id, &canonicalName)
	return id, canonicalName, err
}

func addAlias(ctx context.Context, tx pgx.Tx, userID, personID int64, alias string) error {
	alias = strings.TrimSpace(alias)
	normalized := NormalizePersonName(alias)
	if normalized == "" {
		return fmt.Errorf("alias is empty")
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO person_aliases (user_id, person_id, alias, normalized_alias)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, normalized_alias)
		DO UPDATE SET person_id = EXCLUDED.person_id, alias = EXCLUDED.alias
	`, userID, personID, alias, normalized)
	return err
}
