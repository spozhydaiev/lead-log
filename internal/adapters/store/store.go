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

	"github.com/spozhydaiev/lead-log/internal/apperrors"
	"github.com/spozhydaiev/lead-log/internal/logging"

	"github.com/spozhydaiev/lead-log/internal/models"
)

const (
	NoteProcessingStatusPending    = "pending"
	NoteProcessingStatusProcessing = "processing"
	NoteProcessingStatusProcessed  = "processed"
	NoteProcessingStatusFailed     = "failed"
)

type NoteForEnrichment struct {
	ID                  int64
	UserID              int64
	RawText             string
	ProcessingStatus    string
	ProcessingStartedAt time.Time
	ProcessingAttempts  int
	StaleReclaimed      bool
}

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

type WebUser struct {
	ID       int64
	Username *string
}
type APINote struct {
	ID               int64
	RawText          string
	Summary          *string
	Tags             []string
	ProcessingStatus string
	CreatedAt        time.Time
	ProcessedAt      *time.Time
}

type NotesListFilter struct {
	FromUTC *time.Time
	ToUTC   *time.Time
	Status  string
	Query   string
}
type APIAction struct {
	ID            int64
	NoteID        *int64
	Title, Status string
	PersonName    *string
	PersonID      *int64
	DueAt         *time.Time
	CreatedAt     time.Time
	CompletedAt   *time.Time
}
type TodayCounts struct{ Actions, People, Decisions, Entities int }
type PersonHighlight struct {
	PersonID                int64
	Name, Type, Theme, Text string
}
type EntityView struct{ Type, Value string }
type DecisionView struct {
	ID                  int64
	Text, Status, Topic string
}
type NoteDetailRecord struct {
	APINote
	ProcessedAt *time.Time
}
type DailyCache struct {
	ResponseJSON string
	CreatedAt    time.Time
}

// PeopleListFilter constrains the People workspace list query.
type PeopleListFilter struct {
	Query          string
	HasOpenActions bool
}

type PeoplePageCursor struct {
	LastMentionedAt time.Time
	PersonID        int64
}

type PeopleListItem struct {
	PersonID         int64
	Name             string
	Aliases          []string
	FirstMentionedAt time.Time
	LastMentionedAt  time.Time
	MentionCount     int
	OpenActionCount  int
	RecentNote       *PeopleRecentNote
}

type PeopleRecentNote struct {
	ID               int64
	CreatedAt        time.Time
	Summary          *string
	RawText          string
	ProcessingStatus string
	Tickets          []string
}

type PersonProfile struct {
	PersonID         int64
	Name             string
	Aliases          []string
	FirstMentionedAt time.Time
	LastMentionedAt  time.Time
	MentionCount     int
}

// TicketsListFilter constrains the Tickets workspace list query.
type TicketsListFilter struct {
	Query          string
	HasOpenActions bool
}

type TicketsPageCursor struct {
	LastMentionedAt time.Time
	Key             string
}

type TicketListItem struct {
	Key              string
	FirstMentionedAt time.Time
	LastMentionedAt  time.Time
	MentionCount     int
	OpenActionCount  int
	RecentNote       *TicketRecentNote
}

type TicketRecentNote struct {
	ID               int64
	CreatedAt        time.Time
	Summary          *string
	RawText          string
	ProcessingStatus string
	People           []PersonRef
}

type PersonRef struct {
	PersonID int64
	Name     string
}

type TicketProfile struct {
	Key              string
	FirstMentionedAt time.Time
	LastMentionedAt  time.Time
	MentionCount     int
}

type PageCursor struct {
	CreatedAt time.Time
	ID        int64
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
		s.logger.Error("database error", logging.WithSafeError([]any{"operation", operation}, err)...)
	}
}

func (s *Store) UpsertUser(ctx context.Context, telegramUserID int64, username string) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var id int64
	err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_accounts WHERE telegram_user_id=$1`, telegramUserID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO users (telegram_user_id, username, display_name) VALUES ($1,$2,NULLIF($2,'')) RETURNING id`, telegramUserID, username).Scan(&id)
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO telegram_accounts (user_id,telegram_user_id,linked_at) VALUES ($1,$2,now())`, id, telegramUserID)
		}
	}
	if err != nil {
		s.logDBError("store.upsert_user", err)
		return 0, err
	}
	return id, tx.Commit(ctx)
}

func (s *Store) WebUserByTelegramID(ctx context.Context, telegramUserID int64) (WebUser, error) {
	var u WebUser
	err := s.pool.QueryRow(ctx, `SELECT u.id, u.display_name FROM users u JOIN telegram_accounts ta ON ta.user_id=u.id WHERE ta.telegram_user_id=$1`, telegramUserID).Scan(&u.ID, &u.Username)
	return u, err
}

func (s *Store) ListAPINotes(ctx context.Context, userID int64, limit int, cursor *PageCursor) ([]APINote, error) {
	return s.ListAPINotesHistory(ctx, userID, NotesListFilter{}, limit, cursor)
}

func (s *Store) ListAPINotesHistory(ctx context.Context, userID int64, f NotesListFilter, limit int, cursor *PageCursor) ([]APINote, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id,raw_text,summary,tags,processing_status,created_at,processed_at
		FROM notes
		WHERE user_id=$1
		  AND ($2::timestamptz IS NULL OR (created_at,id)<($2,$3))
		  AND ($4::timestamptz IS NULL OR created_at >= $4)
		  AND ($5::timestamptz IS NULL OR created_at < $5)
		  AND ($6::text = '' OR processing_status = $6)
		  AND ($7::text = '' OR (raw_text || ' ' || COALESCE(summary,'')) ILIKE '%' || $7 || '%')
		ORDER BY created_at DESC,id DESC
		LIMIT $8`, userID, cursorTime(cursor), cursorID(cursor), f.FromUTC, f.ToUTC, f.Status, f.Query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APINote
	for rows.Next() {
		var n APINote
		if err := rows.Scan(&n.ID, &n.RawText, &n.Summary, &n.Tags, &n.ProcessingStatus, &n.CreatedAt, &n.ProcessedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
func cursorTime(c *PageCursor) any {
	if c == nil {
		return nil
	}
	return c.CreatedAt
}
func cursorID(c *PageCursor) int64 {
	if c == nil {
		return 0
	}
	return c.ID
}

func (s *Store) ListAPIActions(ctx context.Context, userID int64, status string, limit int, cursor *PageCursor) ([]APIAction, error) {
	rows, err := s.pool.Query(ctx, `SELECT a.id,n.id,a.title,a.status,p.name,p.id,a.due_at,a.created_at,a.completed_at FROM actions a LEFT JOIN notes n ON n.id=a.note_id AND n.user_id=a.user_id LEFT JOIN people p ON p.id=a.linked_person_id AND p.user_id=a.user_id WHERE a.user_id=$1 AND ($2='all' OR a.status=$2) AND ($3::timestamptz IS NULL OR (a.created_at,a.id)<($3,$4)) ORDER BY a.created_at DESC,a.id DESC LIMIT $5`, userID, status, cursorTime(cursor), cursorID(cursor), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIAction
	for rows.Next() {
		var a APIAction
		if err := rows.Scan(&a.ID, &a.NoteID, &a.Title, &a.Status, &a.PersonName, &a.PersonID, &a.DueAt, &a.CreatedAt, &a.CompletedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) SetActionStatus(ctx context.Context, userID, actionID int64, status string) (APIAction, error) {
	var a APIAction
	err := s.pool.QueryRow(ctx, `UPDATE actions SET status=$3,completed_at=CASE WHEN $3='done' THEN COALESCE(completed_at,now()) ELSE NULL END WHERE user_id=$1 AND id=$2 RETURNING id,note_id,title,status,NULL::text,NULL::bigint,due_at,created_at,completed_at`, userID, actionID, status).Scan(&a.ID, &a.NoteID, &a.Title, &a.Status, &a.PersonName, &a.PersonID, &a.DueAt, &a.CreatedAt, &a.CompletedAt)
	return a, err
}

func (s *Store) ListTodayNotes(ctx context.Context, userID int64, from, to time.Time, limit int) ([]APINote, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,raw_text,summary,tags,processing_status,created_at,processed_at FROM notes WHERE user_id=$1 AND created_at >= $2 AND created_at < $3 ORDER BY created_at DESC,id DESC LIMIT $4`, userID, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APINote
	for rows.Next() {
		var n APINote
		if err := rows.Scan(&n.ID, &n.RawText, &n.Summary, &n.Tags, &n.ProcessingStatus, &n.CreatedAt, &n.ProcessedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
func (s *Store) NoteCounts(ctx context.Context, userID int64, ids []int64) (map[int64]TodayCounts, error) {
	out := map[int64]TodayCounts{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT n.id,(SELECT count(*) FROM actions a WHERE a.user_id=$1 AND a.note_id=n.id),(SELECT count(DISTINCT pn.person_id) FROM people_notes pn WHERE pn.user_id=$1 AND pn.note_id=n.id),(SELECT count(*) FROM decisions d WHERE d.user_id=$1 AND d.note_id=n.id),(SELECT count(*) FROM entity_mentions e WHERE e.user_id=$1 AND e.note_id=n.id) FROM notes n WHERE n.user_id=$1 AND n.id=ANY($2)`, userID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var c TodayCounts
		if err := rows.Scan(&id, &c.Actions, &c.People, &c.Decisions, &c.Entities); err != nil {
			return nil, err
		}
		out[id] = c
	}
	return out, rows.Err()
}
func (s *Store) HighlightsForNotes(ctx context.Context, userID int64, ids []int64, limit int) (map[int64][]PersonHighlight, error) {
	out := map[int64][]PersonHighlight{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT note_id,person_id,name,type,COALESCE(theme,''),text FROM (SELECT pn.note_id,pn.person_id,p.name,pn.type,pn.theme,pn.text,row_number() OVER(PARTITION BY pn.note_id ORDER BY pn.created_at,pn.id) rn FROM people_notes pn JOIN people p ON p.id=pn.person_id AND p.user_id=pn.user_id WHERE pn.user_id=$1 AND pn.note_id=ANY($2)) x WHERE rn <= $3 ORDER BY note_id,rn`, userID, ids, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var x PersonHighlight
		if err := rows.Scan(&id, &x.PersonID, &x.Name, &x.Type, &x.Theme, &x.Text); err != nil {
			return nil, err
		}
		out[id] = append(out[id], x)
	}
	return out, rows.Err()
}
func (s *Store) EntitiesForNotes(ctx context.Context, userID int64, ids []int64, limit int) (map[int64][]EntityView, error) {
	out := map[int64][]EntityView{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT note_id,entity_type,normalized_value FROM (SELECT note_id,entity_type,normalized_value,row_number() OVER(PARTITION BY note_id ORDER BY id) rn FROM entity_mentions WHERE user_id=$1 AND note_id=ANY($2)) x WHERE rn <= $3 ORDER BY note_id,rn`, userID, ids, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var x EntityView
		if err := rows.Scan(&id, &x.Type, &x.Value); err != nil {
			return nil, err
		}
		out[id] = append(out[id], x)
	}
	return out, rows.Err()
}
func (s *Store) NoteDetail(ctx context.Context, userID, noteID int64) (NoteDetailRecord, error) {
	var n NoteDetailRecord
	err := s.pool.QueryRow(ctx, `SELECT id,raw_text,summary,tags,processing_status,created_at,processed_at FROM notes WHERE user_id=$1 AND id=$2`, userID, noteID).Scan(&n.ID, &n.RawText, &n.Summary, &n.Tags, &n.ProcessingStatus, &n.CreatedAt, &n.ProcessedAt)
	return n, err
}
func (s *Store) ActionsForNote(ctx context.Context, userID, noteID int64, limit int) ([]APIAction, error) {
	rows, err := s.pool.Query(ctx, `SELECT a.id,a.note_id,a.title,a.status,p.name,p.id,a.due_at,a.created_at,a.completed_at FROM actions a JOIN notes n ON n.id=a.note_id AND n.user_id=a.user_id LEFT JOIN people p ON p.id=a.linked_person_id AND p.user_id=a.user_id WHERE a.user_id=$1 AND a.note_id=$2 ORDER BY a.created_at,a.id LIMIT $3`, userID, noteID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIAction
	for rows.Next() {
		var a APIAction
		if err := rows.Scan(&a.ID, &a.NoteID, &a.Title, &a.Status, &a.PersonName, &a.PersonID, &a.DueAt, &a.CreatedAt, &a.CompletedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) DecisionsForNote(ctx context.Context, userID, noteID int64, limit int) ([]DecisionView, error) {
	rows, err := s.pool.Query(ctx, `SELECT d.id,d.text,d.status,COALESCE(d.topic,'') FROM decisions d JOIN notes n ON n.id=d.note_id AND n.user_id=d.user_id WHERE d.user_id=$1 AND d.note_id=$2 ORDER BY d.id LIMIT $3`, userID, noteID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DecisionView
	for rows.Next() {
		var x DecisionView
		if err := rows.Scan(&x.ID, &x.Text, &x.Status, &x.Topic); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Store) LatestDailyCache(ctx context.Context, userID int64, scope string) (*DailyCache, error) {
	var x DailyCache
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(response_json::text,''),created_at FROM agent_responses WHERE user_id=$1 AND kind='daily' AND scope_key LIKE $2||':%' ORDER BY created_at DESC LIMIT 1`, userID, scope).Scan(&x.ResponseJSON, &x.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &x, err
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

func (s *Store) SaveRawAPINote(ctx context.Context, userID int64, raw string) (APINote, error) {
	var note APINote
	err := s.pool.QueryRow(ctx, `INSERT INTO notes (user_id, raw_text) VALUES ($1,$2) RETURNING id,raw_text,summary,tags,processing_status,created_at,processed_at`, userID, raw).Scan(&note.ID, &note.RawText, &note.Summary, &note.Tags, &note.ProcessingStatus, &note.CreatedAt, &note.ProcessedAt)
	if err != nil {
		s.logDBError("store.save_raw_api_note", err)
	}
	return note, err
}

func (s *Store) CreateAndClaimNoteForEnrichment(ctx context.Context, userID int64, raw string) (NoteForEnrichment, error) {
	var note NoteForEnrichment
	err := s.pool.QueryRow(ctx, `
		INSERT INTO notes (user_id, raw_text, processing_status, processing_started_at, processing_attempts)
		VALUES ($1, $2, $3, now(), 1)
		RETURNING id, user_id, raw_text, processing_status, processing_started_at, processing_attempts, false
	`, userID, raw, NoteProcessingStatusProcessing).Scan(&note.ID, &note.UserID, &note.RawText, &note.ProcessingStatus, &note.ProcessingStartedAt, &note.ProcessingAttempts, &note.StaleReclaimed)
	if err != nil {
		s.logDBError("store.create_and_claim_note_for_enrichment", err)
	}
	return note, err
}

func (s *Store) ClaimNextNotesForEnrichment(ctx context.Context, limit, maxAttempts int, staleAfter time.Duration) ([]NoteForEnrichment, error) {
	if limit <= 0 {
		return nil, nil
	}
	if maxAttempts <= 0 {
		return nil, nil
	}
	staleSeconds := int64(staleAfter.Seconds())
	rows, err := s.pool.Query(ctx, `
		WITH candidate AS (
			SELECT id, processing_status AS old_status
			FROM notes
			WHERE processing_attempts < $1
			  AND (
				processing_status = $2
				OR (processing_status = $3 AND COALESCE(next_processing_at, processing_failed_at, created_at) <= now())
				OR (processing_status = $4 AND processing_started_at < now() - make_interval(secs => $5))
			  )
			ORDER BY COALESCE(next_processing_at, created_at), id
			LIMIT $6
			FOR UPDATE SKIP LOCKED
		)
		UPDATE notes n
		SET processing_status=$4,
		    processing_started_at=now(),
		    processing_failed_at=NULL,
		    processing_error=NULL,
		    next_processing_at=NULL,
		    processing_attempts=processing_attempts+1
		FROM candidate
		WHERE n.id = candidate.id
		RETURNING n.id, n.user_id, n.raw_text, n.processing_status, n.processing_started_at, n.processing_attempts,
		          (candidate.old_status = $4)
	`, maxAttempts, NoteProcessingStatusPending, NoteProcessingStatusFailed, NoteProcessingStatusProcessing, staleSeconds, limit)
	if err != nil {
		s.logDBError("store.claim_next_notes_for_enrichment", err)
		return nil, err
	}
	defer rows.Close()
	var notes []NoteForEnrichment
	for rows.Next() {
		var note NoteForEnrichment
		if err := rows.Scan(&note.ID, &note.UserID, &note.RawText, &note.ProcessingStatus, &note.ProcessingStartedAt, &note.ProcessingAttempts, &note.StaleReclaimed); err != nil {
			return nil, err
		}
		notes = append(notes, note)
	}
	return notes, rows.Err()
}

func (s *Store) ScheduleNoteEnrichmentRetry(ctx context.Context, userID, noteID int64, startedAt time.Time, nextAt time.Time, cause error) error {
	msg := ""
	if cause != nil {
		msg = cause.Error()
		if len(msg) > 500 {
			msg = msg[:500]
		}
	}
	_, err := s.pool.Exec(ctx, `UPDATE notes SET processing_status='failed', processing_failed_at=now(), processing_error=$4, next_processing_at=$5 WHERE user_id=$1 AND id=$2 AND processing_started_at=$3`, userID, noteID, startedAt, msg, nextAt)
	return err
}

func (s *Store) MarkNoteEnrichmentPermanentlyFailed(ctx context.Context, userID, noteID int64, startedAt time.Time, cause error) error {
	msg := ""
	if cause != nil {
		msg = cause.Error()
		if len(msg) > 500 {
			msg = msg[:500]
		}
	}
	_, err := s.pool.Exec(ctx, `UPDATE notes SET processing_status='failed', processing_failed_at=now(), processing_error=$4, next_processing_at=NULL WHERE user_id=$1 AND id=$2 AND processing_started_at=$3`, userID, noteID, startedAt, msg)
	return err
}

func (s *Store) CreateAndClaimNoteForEnrichmentAndMarkTelegramUpdateProcessed(ctx context.Context, userID int64, raw string, meta TelegramUpdateMeta, startedAt time.Time) (NoteForEnrichment, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return NoteForEnrichment{}, err
	}
	defer tx.Rollback(ctx)
	var note NoteForEnrichment
	if err := tx.QueryRow(ctx, `
		INSERT INTO notes (user_id, raw_text, processing_status, processing_started_at, processing_attempts)
		VALUES ($1, $2, $3, now(), 1)
		RETURNING id, user_id, raw_text, processing_status, processing_started_at, processing_attempts, false
	`, userID, raw, NoteProcessingStatusProcessing).Scan(&note.ID, &note.UserID, &note.RawText, &note.ProcessingStatus, &note.ProcessingStartedAt, &note.ProcessingAttempts, &note.StaleReclaimed); err != nil {
		return NoteForEnrichment{}, err
	}
	tag, err := tx.Exec(ctx, `UPDATE telegram_updates SET status='processed', processed_at=now(), last_error=NULL WHERE (telegram_update_id=$1 OR (telegram_chat_id=$3 AND telegram_message_id=$4)) AND status='processing' AND processing_started_at=$2`, meta.UpdateID, startedAt, meta.ChatID, meta.MessageID)
	if err != nil {
		return NoteForEnrichment{}, err
	}
	if tag.RowsAffected() == 0 {
		return NoteForEnrichment{}, fmt.Errorf("telegram update claim no longer active")
	}
	return note, tx.Commit(ctx)
}

func (s *Store) SaveParsedNote(ctx context.Context, userID int64, raw string, parsed models.ParsedNote) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var noteID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO notes (user_id, raw_text, summary, tags, processing_status, processed_at)
		VALUES ($1, $2, $3, $4, 'processed', now())
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

	for _, d := range parsed.Decisions {
		decision, ok := models.NormalizeDecision(d)
		if !ok {
			continue
		}
		var linkedPersonID *int64
		if decision.LinkedPersonName != "" {
			key := NormalizePersonName(decision.LinkedPersonName)
			pid, ok := personIDs[key]
			var err error
			if !ok {
				pid, err = upsertPerson(ctx, tx, userID, decision.LinkedPersonName)
				if err != nil {
					return 0, err
				}
			}
			linkedPersonID = &pid
		}
		if _, err := tx.Exec(ctx, `INSERT INTO decisions (user_id, note_id, text, normalized_text, linked_person_id, topic, status) VALUES ($1,$2,$3,$4,$5,$6,$7)`, userID, noteID, decision.Text, models.NormalizeDecisionText(decision.Text), linkedPersonID, decision.Topic, models.DecisionStatusActive); err != nil {
			return 0, err
		}
	}
	normalizedMentions, _ := models.NormalizeEntityMentionsForNote(parsed.EntityMentions)
	for _, m := range normalizedMentions {
		if _, err := tx.Exec(ctx, `INSERT INTO entity_mentions (user_id, note_id, entity_type, raw_value, normalized_value, display_value, context) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (note_id, entity_type, normalized_value) DO UPDATE SET raw_value=EXCLUDED.raw_value, display_value=EXCLUDED.display_value, context=EXCLUDED.context`, userID, noteID, m.Type, m.RawValue, m.NormalizedValue, m.DisplayValue, m.Context); err != nil {
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

func (s *Store) ClaimNoteForEnrichment(ctx context.Context, userID, noteID int64, staleAfter time.Duration, allowProcessed bool) (NoteForEnrichment, error) {
	var note NoteForEnrichment
	staleSeconds := int64(staleAfter.Seconds())
	statuses := []string{NoteProcessingStatusPending, NoteProcessingStatusFailed}
	if allowProcessed {
		statuses = append(statuses, NoteProcessingStatusProcessed)
	}
	err := s.pool.QueryRow(ctx, `
		WITH candidate AS (
			SELECT id, processing_status AS old_status
			FROM notes
			WHERE id=$1 AND user_id=$2
			  AND (processing_status = ANY($3) OR (processing_status=$4 AND processing_started_at < now() - make_interval(secs => $5)))
			FOR UPDATE
		)
		UPDATE notes n
		SET processing_status=$6,
		    processing_started_at=now(),
		    processing_failed_at=NULL,
		    processing_error=NULL,
		    processing_attempts=processing_attempts+1
		FROM candidate
		WHERE n.id = candidate.id
		RETURNING n.id, n.user_id, n.raw_text, n.processing_status, n.processing_started_at, n.processing_attempts,
		          (candidate.old_status = $4)
	`, noteID, userID, statuses, NoteProcessingStatusProcessing, staleSeconds, NoteProcessingStatusProcessing).Scan(&note.ID, &note.UserID, &note.RawText, &note.ProcessingStatus, &note.ProcessingStartedAt, &note.ProcessingAttempts, &note.StaleReclaimed)
	if err == nil {
		return note, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return note, err
	}
	err = s.pool.QueryRow(ctx, `SELECT id, user_id, raw_text, processing_status, COALESCE(processing_started_at, created_at), processing_attempts FROM notes WHERE id=$1 AND user_id=$2`, noteID, userID).Scan(&note.ID, &note.UserID, &note.RawText, &note.ProcessingStatus, &note.ProcessingStartedAt, &note.ProcessingAttempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return note, fmt.Errorf("note not found")
	}
	return note, err
}

func (s *Store) SaveNoteEnrichmentResult(ctx context.Context, userID, noteID int64, startedAt time.Time, parsed models.ParsedNote, model, promptVersion string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `DELETE FROM actions WHERE user_id=$1 AND note_id=$2`, userID, noteID)
	_ = tag
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM people_notes WHERE user_id=$1 AND note_id=$2`, userID, noteID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM decisions WHERE user_id=$1 AND note_id=$2`, userID, noteID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM entity_mentions WHERE user_id=$1 AND note_id=$2`, userID, noteID); err != nil {
		return err
	}
	if err := saveStructuredRecordsForNoteTx(ctx, tx, userID, noteID, parsed); err != nil {
		return err
	}
	tag, err = tx.Exec(ctx, `
		UPDATE notes
		SET summary=$4, tags=$5, processing_status='processed', processed_at=now(), processing_failed_at=NULL,
		    processing_error=NULL, next_processing_at=NULL, processing_model=$6, processing_prompt_version=$7
		WHERE user_id=$1 AND id=$2 AND processing_status='processing' AND processing_started_at=$3
	`, userID, noteID, startedAt, parsed.Summary, parsed.Tags, model, promptVersion)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("note enrichment claim no longer active")
	}
	return tx.Commit(ctx)
}

func (s *Store) MarkNoteEnrichmentFailed(ctx context.Context, userID, noteID int64, startedAt time.Time, cause error) error {
	msg := ""
	if cause != nil {
		msg = cause.Error()
		if len(msg) > 500 {
			msg = msg[:500]
		}
	}
	_, err := s.pool.Exec(ctx, `UPDATE notes SET processing_status='failed', processing_failed_at=now(), processing_error=$4 WHERE user_id=$1 AND id=$2 AND processing_status='processing' AND processing_started_at=$3`, userID, noteID, startedAt, msg)
	return err
}

func saveStructuredRecordsForNoteTx(ctx context.Context, tx pgx.Tx, userID, noteID int64, parsed models.ParsedNote) error {
	personIDs := map[string]int64{}
	for _, pn := range parsed.PeopleNotes {
		name := strings.TrimSpace(pn.PersonName)
		if name == "" {
			continue
		}
		pid, err := upsertPerson(ctx, tx, userID, name)
		if err != nil {
			return err
		}
		personIDs[NormalizePersonName(name)] = pid
		if _, err := tx.Exec(ctx, `INSERT INTO people_notes (user_id, person_id, note_id, type, theme, text, include_in_review) VALUES ($1,$2,$3,$4,$5,$6,$7)`, userID, pid, noteID, emptyTo(pn.Type, "context"), pn.Theme, pn.Text, pn.IncludeInReview); err != nil {
			return err
		}
	}
	for _, d := range parsed.Decisions {
		decision, ok := models.NormalizeDecision(d)
		if !ok {
			continue
		}
		var linkedPersonID *int64
		if decision.LinkedPersonName != "" {
			key := NormalizePersonName(decision.LinkedPersonName)
			pid, ok := personIDs[key]
			var err error
			if !ok {
				pid, err = upsertPerson(ctx, tx, userID, decision.LinkedPersonName)
				if err != nil {
					return err
				}
			}
			linkedPersonID = &pid
		}
		if _, err := tx.Exec(ctx, `INSERT INTO decisions (user_id, note_id, text, normalized_text, linked_person_id, topic, status) VALUES ($1,$2,$3,$4,$5,$6,$7)`, userID, noteID, decision.Text, models.NormalizeDecisionText(decision.Text), linkedPersonID, decision.Topic, models.DecisionStatusActive); err != nil {
			return err
		}
	}
	normalizedMentions, _ := models.NormalizeEntityMentionsForNote(parsed.EntityMentions)
	for _, m := range normalizedMentions {
		if _, err := tx.Exec(ctx, `INSERT INTO entity_mentions (user_id, note_id, entity_type, raw_value, normalized_value, display_value, context) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (note_id, entity_type, normalized_value) DO UPDATE SET raw_value=EXCLUDED.raw_value, display_value=EXCLUDED.display_value, context=EXCLUDED.context`, userID, noteID, m.Type, m.RawValue, m.NormalizedValue, m.DisplayValue, m.Context); err != nil {
			return err
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
					return err
				}
			}
			linkedPersonID = &pid
		}
		if _, err := tx.Exec(ctx, `INSERT INTO actions (user_id, note_id, linked_person_id, title, output_type) VALUES ($1,$2,$3,$4,$5)`, userID, noteID, linkedPersonID, title, action.OutputType); err != nil {
			return err
		}
	}
	return nil
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
	if err := tx.QueryRow(ctx, `INSERT INTO notes (user_id, raw_text, summary, tags, processing_status, processed_at) VALUES ($1,$2,$3,$4,'processed',now()) RETURNING id`, userID, raw, parsed.Summary, parsed.Tags).Scan(&noteID); err != nil {
		return 0, err
	}
	if err := saveStructuredRecordsForNoteTx(ctx, tx, userID, noteID, parsed); err != nil {
		return 0, err
	}
	return noteID, nil
}

func (s *Store) ListDecisionsByNote(ctx context.Context, userID, noteID int64) ([]models.DecisionRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT d.id, d.user_id, d.note_id, d.text, d.normalized_text, d.linked_person_id, p.name, COALESCE(d.topic,''), d.status, d.decided_at, d.created_at, d.updated_at FROM decisions d LEFT JOIN people p ON p.id=d.linked_person_id WHERE d.user_id=$1 AND d.note_id=$2 ORDER BY d.id`, userID, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.DecisionRecord
	for rows.Next() {
		var r models.DecisionRecord
		if err := rows.Scan(&r.ID, &r.UserID, &r.NoteID, &r.Text, &r.NormalizedText, &r.LinkedPersonID, &r.LinkedPersonName, &r.Topic, &r.Status, &r.DecidedAt, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ListEntityMentionsByNote(ctx context.Context, userID, noteID int64) ([]models.EntityMentionRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, user_id, note_id, entity_type, raw_value, normalized_value, display_value, COALESCE(context,''), created_at FROM entity_mentions WHERE user_id=$1 AND note_id=$2 ORDER BY id`, userID, noteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.EntityMentionRecord
	for rows.Next() {
		var r models.EntityMentionRecord
		if err := rows.Scan(&r.ID, &r.UserID, &r.NoteID, &r.Type, &r.RawValue, &r.NormalizedValue, &r.DisplayValue, &r.Context, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type CurrentUser struct {
	ID               int64
	Email            *string
	DisplayName      *string
	Timezone         string
	ResponseLanguage string
}

type TelegramStatus struct {
	Connected bool       `json:"connected"`
	LinkedAt  *time.Time `json:"linked_at"`
}

type LinkConsumeResult string

const (
	LinkConsumeSuccess  LinkConsumeResult = "success"
	LinkConsumeInvalid  LinkConsumeResult = "invalid"
	LinkConsumeConflict LinkConsumeResult = "conflict"
)

func (s *Store) CreateUserWithIdentityAndSession(ctx context.Context, email, emailNorm, passwordHash, displayName, timezone, language, tokenHash string, expiresAt time.Time) (CurrentUser, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return CurrentUser{}, err
	}
	defer tx.Rollback(ctx)
	var u CurrentUser
	err = tx.QueryRow(ctx, `INSERT INTO users (display_name, timezone, response_language) VALUES (NULLIF($1,''),$2,$3) RETURNING id,display_name,timezone,response_language`, displayName, timezone, language).Scan(&u.ID, &u.DisplayName, &u.Timezone, &u.ResponseLanguage)
	if err != nil {
		return CurrentUser{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO auth_identities (user_id,provider,email,email_normalized,password_hash) VALUES ($1,'local',$2,$3,$4)`, u.ID, email, emailNorm, passwordHash)
	if err != nil {
		return CurrentUser{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO web_sessions (user_id,token_hash,expires_at,last_seen_at) VALUES ($1,$2,$3,now())`, u.ID, tokenHash, expiresAt)
	if err != nil {
		return CurrentUser{}, err
	}
	u.Email = &email
	return u, tx.Commit(ctx)
}

func (s *Store) LocalIdentityByEmail(ctx context.Context, emailNorm string) (int64, string, error) {
	var userID int64
	var hash string
	err := s.pool.QueryRow(ctx, `SELECT user_id,password_hash FROM auth_identities WHERE provider='local' AND email_normalized=$1`, emailNorm).Scan(&userID, &hash)
	return userID, hash, err
}
func (s *Store) CreateSession(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO web_sessions (user_id,token_hash,expires_at,last_seen_at) VALUES ($1,$2,$3,now())`, userID, tokenHash, expiresAt)
	return err
}
func (s *Store) CurrentUserByID(ctx context.Context, userID int64) (CurrentUser, error) {
	var u CurrentUser
	err := s.pool.QueryRow(ctx, `SELECT u.id, ai.email, u.display_name, u.timezone, u.response_language FROM users u LEFT JOIN LATERAL (SELECT email FROM auth_identities WHERE user_id=u.id AND provider='local' ORDER BY id LIMIT 1) ai ON true WHERE u.id=$1`, userID).Scan(&u.ID, &u.Email, &u.DisplayName, &u.Timezone, &u.ResponseLanguage)
	return u, err
}
func (s *Store) CurrentUserBySessionHash(ctx context.Context, tokenHash string, now time.Time) (CurrentUser, error) {
	var u CurrentUser
	var last time.Time
	err := s.pool.QueryRow(ctx, `SELECT u.id, ai.email, u.display_name, u.timezone, u.response_language, ws.last_seen_at FROM web_sessions ws JOIN users u ON u.id=ws.user_id LEFT JOIN LATERAL (SELECT email FROM auth_identities WHERE user_id=u.id AND provider='local' ORDER BY id LIMIT 1) ai ON true WHERE ws.token_hash=$1 AND ws.expires_at>$2 AND ws.revoked_at IS NULL`, tokenHash, now).Scan(&u.ID, &u.Email, &u.DisplayName, &u.Timezone, &u.ResponseLanguage, &last)
	if err != nil {
		return u, err
	}
	if now.Sub(last) > time.Hour {
		_, _ = s.pool.Exec(ctx, `UPDATE web_sessions SET last_seen_at=$2 WHERE token_hash=$1 AND last_seen_at<$2 - interval '1 hour'`, tokenHash, now)
	}
	return u, nil
}
func (s *Store) RevokeSession(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx, `UPDATE web_sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE token_hash=$1`, tokenHash)
	return err
}
func (s *Store) TelegramStatus(ctx context.Context, userID int64) (TelegramStatus, error) {
	var st TelegramStatus
	var linked *time.Time
	err := s.pool.QueryRow(ctx, `SELECT linked_at FROM telegram_accounts WHERE user_id=$1`, userID).Scan(&linked)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	st.Connected = true
	st.LinkedAt = linked
	return st, nil
}
func (s *Store) CreateTelegramLinkToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `UPDATE telegram_link_tokens SET used_at=now() WHERE user_id=$1 AND used_at IS NULL`, userID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO telegram_link_tokens (user_id,token_hash,expires_at) VALUES ($1,$2,$3)`, userID, tokenHash, expiresAt)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) UnlinkTelegram(ctx context.Context, userID int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM telegram_accounts WHERE user_id=$1`, userID)
	return err
}
func (s *Store) ResolveTelegramUser(ctx context.Context, telegramUserID int64, chatID int64) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `UPDATE telegram_accounts SET telegram_chat_id=$2,updated_at=now() WHERE telegram_user_id=$1 RETURNING user_id`, telegramUserID, chatID).Scan(&id)
	return id, err
}
func (s *Store) LinkTelegramByToken(ctx context.Context, rawHash string, telegramUserID, chatID int64, newSessionHash string, sessionExpires time.Time) (LinkConsumeResult, int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return LinkConsumeInvalid, 0, err
	}
	defer tx.Rollback(ctx)
	var target int64
	err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_link_tokens WHERE token_hash=$1 AND expires_at>now() AND used_at IS NULL FOR UPDATE`, rawHash).Scan(&target)
	if errors.Is(err, pgx.ErrNoRows) {
		return LinkConsumeInvalid, 0, nil
	}
	if err != nil {
		return LinkConsumeInvalid, 0, err
	}
	var existing int64
	err = tx.QueryRow(ctx, `SELECT user_id FROM telegram_accounts WHERE telegram_user_id=$1 FOR UPDATE`, telegramUserID).Scan(&existing)
	if err == nil && existing != target {
		var webCount int
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM auth_identities WHERE user_id=$1 AND provider='local'`, existing).Scan(&webCount)
		if webCount > 0 {
			return LinkConsumeConflict, 0, nil
		}
		var targetData int
		_ = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM notes WHERE user_id=$1)+(SELECT count(*) FROM actions WHERE user_id=$1)+(SELECT count(*) FROM people WHERE user_id=$1)+(SELECT count(*) FROM people_notes WHERE user_id=$1)+(SELECT count(*) FROM decisions WHERE user_id=$1)`, target).Scan(&targetData)
		var identityCount int
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM auth_identities WHERE user_id=$1`, target).Scan(&identityCount)
		if targetData > 0 || identityCount != 1 {
			return LinkConsumeConflict, 0, nil
		}
		_, err = tx.Exec(ctx, `UPDATE auth_identities SET user_id=$1,updated_at=now() WHERE user_id=$2`, existing, target)
		if err != nil {
			return LinkConsumeInvalid, 0, err
		}
		_, err = tx.Exec(ctx, `UPDATE web_sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE user_id=$1`, target)
		if err != nil {
			return LinkConsumeInvalid, 0, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO web_sessions (user_id,token_hash,expires_at,last_seen_at) VALUES ($1,$2,$3,now())`, existing, newSessionHash, sessionExpires)
		if err != nil {
			return LinkConsumeInvalid, 0, err
		}
		_, err = tx.Exec(ctx, `DELETE FROM users WHERE id=$1`, target)
		if err != nil {
			return LinkConsumeInvalid, 0, err
		}
		target = existing
	} else if errors.Is(err, pgx.ErrNoRows) {
		_, err = tx.Exec(ctx, `INSERT INTO telegram_accounts (user_id,telegram_user_id,telegram_chat_id,linked_at) VALUES ($1,$2,$3,now())`, target, telegramUserID, chatID)
		if err != nil {
			return LinkConsumeConflict, 0, nil
		}
	} else if err != nil {
		return LinkConsumeInvalid, 0, err
	} else {
		_, err = tx.Exec(ctx, `UPDATE telegram_accounts SET telegram_chat_id=$2,updated_at=now() WHERE telegram_user_id=$1`, telegramUserID, chatID)
		if err != nil {
			return LinkConsumeInvalid, 0, err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE telegram_link_tokens SET used_at=now() WHERE token_hash=$1 AND used_at IS NULL`, rawHash)
	if err != nil {
		return LinkConsumeInvalid, 0, err
	}
	return LinkConsumeSuccess, target, tx.Commit(ctx)
}
func (s *Store) CleanupAuth(ctx context.Context, now time.Time, limit int) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM web_sessions WHERE id IN (SELECT id FROM web_sessions WHERE expires_at<$1 OR (revoked_at IS NOT NULL AND revoked_at<$1 - interval '7 days') ORDER BY id LIMIT $2)`, now, limit)
	if err != nil {
		return 0, err
	}
	tag2, err := s.pool.Exec(ctx, `DELETE FROM telegram_link_tokens WHERE id IN (SELECT id FROM telegram_link_tokens WHERE expires_at<$1 OR used_at IS NOT NULL ORDER BY id LIMIT $2)`, now, limit)
	return tag.RowsAffected() + tag2.RowsAffected(), err
}

func (s *Store) ListPeopleWorkspace(ctx context.Context, userID int64, f PeopleListFilter, limit int, cursor *PeoplePageCursor) ([]PeopleListItem, error) {
	rows, err := s.pool.Query(ctx, `
		WITH base AS (
			SELECT p.id, p.name, min(pn.created_at) AS first_mentioned_at, max(pn.created_at) AS last_mentioned_at, count(*)::int AS mention_count
			FROM people p
			JOIN people_notes pn ON pn.person_id=p.id AND pn.user_id=p.user_id
			WHERE p.user_id=$1
			  AND ($2::text = '' OR p.name ILIKE '%' || $2 || '%' OR EXISTS (SELECT 1 FROM person_aliases pa WHERE pa.user_id=p.user_id AND pa.person_id=p.id AND pa.alias ILIKE '%' || $2 || '%'))
			  AND ($3::bool = false OR EXISTS (SELECT 1 FROM actions a WHERE a.user_id=p.user_id AND a.linked_person_id=p.id AND a.status='open'))
			GROUP BY p.id, p.name
		), page AS (
			SELECT * FROM base
			WHERE ($4::timestamptz IS NULL OR (last_mentioned_at,id)<($4,$5))
			ORDER BY last_mentioned_at DESC,id DESC
			LIMIT $6
		), aliases AS (
			SELECT person_id, array_agg(alias ORDER BY alias) AS aliases
			FROM (SELECT DISTINCT ON (pa.person_id, pa.normalized_alias) pa.person_id, pa.alias FROM person_aliases pa JOIN page pg ON pg.id=pa.person_id WHERE pa.user_id=$1 AND lower(pa.alias) <> lower(pg.name) ORDER BY pa.person_id, pa.normalized_alias, pa.alias LIMIT 1000) x
			GROUP BY person_id
		), open_actions AS (
			SELECT linked_person_id AS person_id, count(*)::int AS cnt FROM actions a JOIN page pg ON pg.id=a.linked_person_id WHERE a.user_id=$1 AND a.status='open' GROUP BY linked_person_id
		), recent AS (
			SELECT DISTINCT ON (pn.person_id) pn.person_id,n.id,n.created_at,n.summary,n.raw_text,n.processing_status
			FROM people_notes pn JOIN page pg ON pg.id=pn.person_id JOIN notes n ON n.id=pn.note_id AND n.user_id=pn.user_id
			WHERE pn.user_id=$1 ORDER BY pn.person_id,n.created_at DESC,n.id DESC
		)
		SELECT pg.id,pg.name,COALESCE(al.aliases,'{}'),pg.first_mentioned_at,pg.last_mentioned_at,pg.mention_count,COALESCE(oa.cnt,0),r.id,r.created_at,r.summary,r.raw_text,r.processing_status
		FROM page pg LEFT JOIN aliases al ON al.person_id=pg.id LEFT JOIN open_actions oa ON oa.person_id=pg.id LEFT JOIN recent r ON r.person_id=pg.id
		ORDER BY pg.last_mentioned_at DESC,pg.id DESC`, userID, f.Query, f.HasOpenActions, peopleCursorTime(cursor), peopleCursorID(cursor), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PeopleListItem
	for rows.Next() {
		var x PeopleListItem
		var rn PeopleRecentNote
		var nid *int64
		var ncreated *time.Time
		var summary *string
		var raw *string
		var status *string
		if err := rows.Scan(&x.PersonID, &x.Name, &x.Aliases, &x.FirstMentionedAt, &x.LastMentionedAt, &x.MentionCount, &x.OpenActionCount, &nid, &ncreated, &summary, &raw, &status); err != nil {
			return nil, err
		}
		if nid != nil && ncreated != nil {
			rn.ID = *nid
			rn.CreatedAt = *ncreated
			rn.Summary = summary
			if raw != nil {
				rn.RawText = *raw
			}
			if status != nil {
				rn.ProcessingStatus = *status
			}
			x.RecentNote = &rn
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func peopleCursorTime(c *PeoplePageCursor) any {
	if c == nil {
		return nil
	}
	return c.LastMentionedAt
}
func peopleCursorID(c *PeoplePageCursor) int64 {
	if c == nil {
		return 0
	}
	return c.PersonID
}

func (s *Store) GetPersonWorkspaceProfile(ctx context.Context, userID, personID int64, aliasLimit int) (PersonProfile, error) {
	var p PersonProfile
	err := s.pool.QueryRow(ctx, `SELECT p.id,p.name,min(pn.created_at),max(pn.created_at),count(*)::int FROM people p JOIN people_notes pn ON pn.person_id=p.id AND pn.user_id=p.user_id WHERE p.user_id=$1 AND p.id=$2 GROUP BY p.id,p.name`, userID, personID).Scan(&p.PersonID, &p.Name, &p.FirstMentionedAt, &p.LastMentionedAt, &p.MentionCount)
	if err != nil {
		return p, err
	}
	p.Aliases, err = s.ListPersonAliases(ctx, userID, personID, aliasLimit+1)
	if err != nil {
		return p, err
	}
	return p, nil
}

func (s *Store) RecentNotesForPerson(ctx context.Context, userID, personID int64, limit int) ([]PeopleRecentNote, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,created_at,summary,raw_text,processing_status FROM (SELECT n.id,n.created_at,n.summary,n.raw_text,n.processing_status,row_number() OVER (PARTITION BY n.id ORDER BY pn.created_at DESC,pn.id DESC) rn FROM people_notes pn JOIN notes n ON n.id=pn.note_id AND n.user_id=pn.user_id WHERE pn.user_id=$1 AND pn.person_id=$2) x WHERE rn=1 ORDER BY created_at DESC,id DESC LIMIT $3`, userID, personID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PeopleRecentNote
	for rows.Next() {
		var x PeopleRecentNote
		if err := rows.Scan(&x.ID, &x.CreatedAt, &x.Summary, &x.RawText, &x.ProcessingStatus); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) OpenActionsForPerson(ctx context.Context, userID, personID int64, limit int) ([]APIAction, error) {
	rows, err := s.pool.Query(ctx, `SELECT a.id,a.note_id,a.title,a.status,p.name,p.id,a.due_at,a.created_at,a.completed_at FROM actions a LEFT JOIN people p ON p.id=a.linked_person_id AND p.user_id=a.user_id WHERE a.user_id=$1 AND a.linked_person_id=$2 AND a.status='open' ORDER BY (a.due_at IS NULL),a.due_at ASC,a.created_at DESC,a.id DESC LIMIT $3`, userID, personID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIAction
	for rows.Next() {
		var a APIAction
		if err := rows.Scan(&a.ID, &a.NoteID, &a.Title, &a.Status, &a.PersonName, &a.PersonID, &a.DueAt, &a.CreatedAt, &a.CompletedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) RecentDecisionsForPerson(ctx context.Context, userID, personID int64, limit int) ([]DecisionView, error) {
	rows, err := s.pool.Query(ctx, `SELECT d.id,d.text,d.status,COALESCE(d.topic,'') FROM decisions d JOIN notes n ON n.id=d.note_id AND n.user_id=d.user_id WHERE d.user_id=$1 AND d.linked_person_id=$2 ORDER BY d.decided_at DESC,d.id DESC LIMIT $3`, userID, personID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DecisionView
	for rows.Next() {
		var x DecisionView
		if err := rows.Scan(&x.ID, &x.Text, &x.Status, &x.Topic); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) ListTicketsWorkspace(ctx context.Context, userID int64, f TicketsListFilter, limit int, cursor *TicketsPageCursor) ([]TicketListItem, error) {
	rows, err := s.pool.Query(ctx, `
		WITH base AS (
			SELECT em.normalized_value AS key, min(n.created_at) AS first_mentioned_at, max(n.created_at) AS last_mentioned_at, count(DISTINCT em.note_id)::int AS mention_count
			FROM entity_mentions em JOIN notes n ON n.id=em.note_id AND n.user_id=em.user_id
			WHERE em.user_id=$1 AND em.entity_type='ticket'
			  AND ($2::text = '' OR em.normalized_value ILIKE '%' || $2 || '%')
			GROUP BY em.normalized_value
		), page AS (
			SELECT * FROM base
			WHERE ($3::timestamptz IS NULL OR (last_mentioned_at,key)<($3,$4))
			  AND ($5::bool = false OR EXISTS (SELECT 1 FROM actions a JOIN entity_mentions em2 ON em2.user_id=a.user_id AND em2.note_id=a.note_id AND em2.entity_type='ticket' AND em2.normalized_value=base.key WHERE a.user_id=$1 AND a.status='open'))
			ORDER BY last_mentioned_at DESC,key DESC LIMIT $6
		), open_actions AS (
			SELECT em.normalized_value AS key, count(DISTINCT a.id)::int AS cnt FROM actions a JOIN entity_mentions em ON em.user_id=a.user_id AND em.note_id=a.note_id AND em.entity_type='ticket' JOIN page pg ON pg.key=em.normalized_value WHERE a.user_id=$1 AND a.status='open' GROUP BY em.normalized_value
		), recent AS (
			SELECT DISTINCT ON (em.normalized_value) em.normalized_value AS key,n.id,n.created_at,n.summary,n.raw_text,n.processing_status
			FROM entity_mentions em JOIN page pg ON pg.key=em.normalized_value JOIN notes n ON n.id=em.note_id AND n.user_id=em.user_id
			WHERE em.user_id=$1 AND em.entity_type='ticket' ORDER BY em.normalized_value,n.created_at DESC,n.id DESC
		)
		SELECT pg.key,pg.first_mentioned_at,pg.last_mentioned_at,pg.mention_count,COALESCE(oa.cnt,0),r.id,r.created_at,r.summary,r.raw_text,r.processing_status
		FROM page pg LEFT JOIN open_actions oa ON oa.key=pg.key LEFT JOIN recent r ON r.key=pg.key
		ORDER BY pg.last_mentioned_at DESC,pg.key DESC`, userID, f.Query, ticketCursorTime(cursor), ticketCursorKey(cursor), f.HasOpenActions, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TicketListItem
	for rows.Next() {
		var x TicketListItem
		var rn TicketRecentNote
		var nid *int64
		var ct *time.Time
		var summary *string
		var raw, status *string
		if err := rows.Scan(&x.Key, &x.FirstMentionedAt, &x.LastMentionedAt, &x.MentionCount, &x.OpenActionCount, &nid, &ct, &summary, &raw, &status); err != nil {
			return nil, err
		}
		if nid != nil && ct != nil {
			rn.ID = *nid
			rn.CreatedAt = *ct
			rn.Summary = summary
			if raw != nil {
				rn.RawText = *raw
			}
			if status != nil {
				rn.ProcessingStatus = *status
			}
			x.RecentNote = &rn
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func ticketCursorTime(c *TicketsPageCursor) any {
	if c == nil {
		return nil
	}
	return c.LastMentionedAt
}
func ticketCursorKey(c *TicketsPageCursor) string {
	if c == nil {
		return ""
	}
	return c.Key
}

func (s *Store) GetTicketWorkspaceProfile(ctx context.Context, userID int64, key string) (TicketProfile, error) {
	var p TicketProfile
	err := s.pool.QueryRow(ctx, `SELECT em.normalized_value,min(n.created_at),max(n.created_at),count(DISTINCT em.note_id)::int FROM entity_mentions em JOIN notes n ON n.id=em.note_id AND n.user_id=em.user_id WHERE em.user_id=$1 AND em.entity_type='ticket' AND em.normalized_value=$2 GROUP BY em.normalized_value`, userID, key).Scan(&p.Key, &p.FirstMentionedAt, &p.LastMentionedAt, &p.MentionCount)
	if err != nil {
		return p, apperrors.Wrap("ticket_repository.get_profile", apperrors.ClassDatabaseQuery, err)
	}
	return p, nil
}
func (s *Store) RecentNotesForTicket(ctx context.Context, userID int64, key string, limit int) ([]TicketRecentNote, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT n.id,n.created_at,n.summary,COALESCE(n.raw_text,''),COALESCE(n.processing_status,'pending') FROM entity_mentions em JOIN notes n ON n.id=em.note_id AND n.user_id=em.user_id WHERE em.user_id=$1 AND em.entity_type='ticket' AND em.normalized_value=$2 ORDER BY n.created_at DESC,n.id DESC LIMIT $3`, userID, key, limit)
	if err != nil {
		return nil, apperrors.Wrap("ticket_repository.list_recent_notes", apperrors.ClassDatabaseQuery, err)
	}
	defer rows.Close()
	var out []TicketRecentNote
	for rows.Next() {
		var x TicketRecentNote
		if err := rows.Scan(&x.ID, &x.CreatedAt, &x.Summary, &x.RawText, &x.ProcessingStatus); err != nil {
			return nil, apperrors.Wrap("ticket_repository.list_recent_notes", apperrors.ClassDatabaseScan, fmt.Errorf("scan recent ticket note: %w", err))
		}
		out = append(out, x)
	}
	if err := rows.Err(); err != nil {
		return nil, apperrors.Wrap("ticket_repository.list_recent_notes", apperrors.ClassDatabaseQuery, err)
	}
	return out, nil
}
func (s *Store) PeopleForNotes(ctx context.Context, userID int64, noteIDs []int64, perNote int) (map[int64][]PersonRef, error) {
	out := map[int64][]PersonRef{}
	if len(noteIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT note_id,person_id,name FROM (SELECT pn.note_id,pn.person_id,p.name,row_number() OVER(PARTITION BY pn.note_id ORDER BY p.name,p.id) rn FROM people_notes pn JOIN people p ON p.id=pn.person_id AND p.user_id=pn.user_id WHERE pn.user_id=$1 AND pn.note_id=ANY($2)) x WHERE rn <= $3 ORDER BY note_id,rn`, userID, noteIDs, perNote)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var note int64
		var pr PersonRef
		if err := rows.Scan(&note, &pr.PersonID, &pr.Name); err != nil {
			return nil, err
		}
		out[note] = append(out[note], pr)
	}
	return out, rows.Err()
}
func (s *Store) OpenActionsForTicket(ctx context.Context, userID int64, key string, limit int) ([]APIAction, error) {
	rows, err := s.pool.Query(ctx, `SELECT a.id,a.note_id,COALESCE(a.title,''),COALESCE(a.status,'open'),p.name,p.id,a.due_at,a.created_at,a.completed_at FROM actions a LEFT JOIN people p ON p.id=a.linked_person_id AND p.user_id=a.user_id WHERE a.user_id=$1 AND a.status='open' AND EXISTS (SELECT 1 FROM entity_mentions em WHERE em.user_id=a.user_id AND em.note_id=a.note_id AND em.entity_type='ticket' AND em.normalized_value=$2) ORDER BY (a.due_at IS NULL),a.due_at ASC,a.created_at DESC,a.id DESC LIMIT $3`, userID, key, limit)
	if err != nil {
		return nil, apperrors.Wrap("ticket_repository.list_open_actions", apperrors.ClassDatabaseQuery, err)
	}
	defer rows.Close()
	var out []APIAction
	for rows.Next() {
		var a APIAction
		if err := rows.Scan(&a.ID, &a.NoteID, &a.Title, &a.Status, &a.PersonName, &a.PersonID, &a.DueAt, &a.CreatedAt, &a.CompletedAt); err != nil {
			return nil, apperrors.Wrap("ticket_repository.list_open_actions", apperrors.ClassDatabaseScan, fmt.Errorf("scan ticket action: %w", err))
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, apperrors.Wrap("ticket_repository.list_open_actions", apperrors.ClassDatabaseQuery, err)
	}
	return out, nil
}
func (s *Store) RecentDecisionsForTicket(ctx context.Context, userID int64, key string, limit int) ([]DecisionView, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT d.id,d.text,d.status,COALESCE(d.topic,'') FROM decisions d JOIN entity_mentions em ON em.user_id=d.user_id AND em.note_id=d.note_id AND em.entity_type='ticket' AND em.normalized_value=$2 WHERE d.user_id=$1 ORDER BY d.id DESC LIMIT $3`, userID, key, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DecisionView
	for rows.Next() {
		var d DecisionView
		if err := rows.Scan(&d.ID, &d.Text, &d.Status, &d.Topic); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
