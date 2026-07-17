package store

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/spozhydaiev/lead-log/internal/models"
)

const RetrievalPerSourceLimit = 50

type ResolvedPerson struct {
	ID           int64
	Name         string
	MatchedAlias string
	Found        bool
	Ambiguous    bool
}

type RetrievalFilters struct {
	Text                                              string
	From                                              *time.Time
	To                                                *time.Time
	PersonID                                          *int64
	EntityType, EntityValue                           string
	ActionStatuses, PeopleNoteTypes, DecisionStatuses []string
	DecisionTopic                                     string
	Limit                                             int
}

func (s *Store) ResolvePerson(ctx context.Context, userID int64, name string) (ResolvedPerson, error) {
	norm := NormalizePersonName(name)
	if userID <= 0 || norm == "" {
		return ResolvedPerson{}, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT p.id, p.name, pa.alias FROM person_aliases pa JOIN people p ON p.id=pa.person_id AND p.user_id=pa.user_id WHERE pa.user_id=$1 AND pa.normalized_alias=$2 LIMIT 2`, userID, norm)
	if err != nil {
		return ResolvedPerson{}, err
	}
	defer rows.Close()
	var matches []ResolvedPerson
	for rows.Next() {
		var p ResolvedPerson
		if err := rows.Scan(&p.ID, &p.Name, &p.MatchedAlias); err != nil {
			return ResolvedPerson{}, err
		}
		p.Found = true
		matches = append(matches, p)
	}
	if err := rows.Err(); err != nil {
		return ResolvedPerson{}, err
	}
	if len(matches) > 1 {
		return ResolvedPerson{Ambiguous: true}, nil
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	rows, err = s.pool.Query(ctx, `SELECT id, name FROM people WHERE user_id=$1 AND lower(regexp_replace(trim(name), '\s+', ' ', 'g'))=$2 LIMIT 2`, userID, norm)
	if err != nil {
		return ResolvedPerson{}, err
	}
	defer rows.Close()
	matches = matches[:0]
	for rows.Next() {
		var p ResolvedPerson
		if err := rows.Scan(&p.ID, &p.Name); err != nil {
			return ResolvedPerson{}, err
		}
		p.Found = true
		matches = append(matches, p)
	}
	if err := rows.Err(); err != nil {
		return ResolvedPerson{}, err
	}
	if len(matches) > 1 {
		return ResolvedPerson{Ambiguous: true}, nil
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return ResolvedPerson{}, nil
}

func (s *Store) SearchNotes(ctx context.Context, userID int64, f RetrievalFilters) ([]models.RetrievalItem, error) {
	limit := boundedStoreLimit(f.Limit)
	rows, err := s.pool.Query(ctx, `
WITH q AS (SELECT plainto_tsquery('simple', $2) AS tsq), c AS (
 SELECT n.id,n.user_id,n.created_at,n.raw_text,COALESCE(n.summary,'') summary,
  CASE WHEN $2='' THEN 0
   WHEN lower(n.raw_text)=lower($2) OR lower(COALESCE(n.summary,''))=lower($2) THEN 100
   WHEN lower(n.raw_text) LIKE lower($2)||'%' OR lower(COALESCE(n.summary,'')) LIKE lower($2)||'%' THEN 90
   WHEN lower(n.raw_text) LIKE '%'||lower($2)||'%' OR lower(COALESCE(n.summary,'')) LIKE '%'||lower($2)||'%' THEN 80
   WHEN to_tsvector('simple', n.raw_text||' '||COALESCE(n.summary,'')) @@ (SELECT tsq FROM q) THEN 55 + ts_rank(to_tsvector('simple', n.raw_text||' '||COALESCE(n.summary,'')), (SELECT tsq FROM q))*10
   ELSE greatest(similarity(n.raw_text,$2), similarity(COALESCE(n.summary,''),$2))*40 END AS score
 FROM notes n WHERE n.user_id=$1 AND ($3::timestamptz IS NULL OR n.created_at >= $3) AND ($4::timestamptz IS NULL OR n.created_at < $4)
) SELECT id,user_id,created_at,raw_text,summary,score + extract(epoch from created_at)/315360000000.0 AS score FROM c
WHERE $2='' OR score >= 12 ORDER BY score DESC, created_at DESC LIMIT $5`, userID, strings.TrimSpace(f.Text), f.From, f.To, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.RetrievalItem
	for rows.Next() {
		var id, uid int64
		var created time.Time
		var raw, summary string
		var score float64
		if err := rows.Scan(&id, &uid, &created, &raw, &summary, &score); err != nil {
			return nil, err
		}
		text := raw
		title := "Note"
		if strings.TrimSpace(summary) != "" {
			title = summary
			text = summary + "\n" + raw
		}
		out = append(out, models.RetrievalItem{Kind: models.RetrievalKindNote, RecordID: id, SourceNoteID: id, UserID: uid, CreatedAt: created, Title: models.RetrievalSnippet(title, f.Text, 120), Text: models.RetrievalSnippet(text, f.Text, 240), Score: score})
	}
	return out, rows.Err()
}

func (s *Store) SearchEntityMentions(ctx context.Context, userID int64, f RetrievalFilters) ([]models.RetrievalItem, error) {
	limit := boundedStoreLimit(f.Limit)
	rows, err := s.pool.Query(ctx, `SELECT em.id,em.user_id,em.note_id,n.created_at,em.entity_type,em.display_value,COALESCE(em.context,''),100.0 FROM entity_mentions em JOIN notes n ON n.id=em.note_id AND n.user_id=em.user_id WHERE em.user_id=$1 AND em.entity_type=$2 AND em.normalized_value=$3 AND ($4::timestamptz IS NULL OR n.created_at >= $4) AND ($5::timestamptz IS NULL OR n.created_at < $5) ORDER BY n.created_at DESC LIMIT $6`, userID, f.EntityType, f.EntityValue, f.From, f.To, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.RetrievalItem
	for rows.Next() {
		var it models.RetrievalItem
		if err := rows.Scan(&it.RecordID, &it.UserID, &it.SourceNoteID, &it.CreatedAt, &it.EntityType, &it.EntityValue, &it.Context, &it.Score); err != nil {
			return nil, err
		}
		it.Kind = models.RetrievalKindEntityMention
		it.Title = it.EntityType + ": " + it.EntityValue
		it.Text = models.RetrievalSnippet(it.Context, f.EntityValue, 240)
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Store) SearchActions(ctx context.Context, userID int64, f RetrievalFilters) ([]models.RetrievalItem, error) {
	return s.searchStructured(ctx, userID, models.RetrievalKindAction, f)
}
func (s *Store) SearchPeopleNotes(ctx context.Context, userID int64, f RetrievalFilters) ([]models.RetrievalItem, error) {
	return s.searchStructured(ctx, userID, models.RetrievalKindPeopleNote, f)
}
func (s *Store) SearchDecisions(ctx context.Context, userID int64, f RetrievalFilters) ([]models.RetrievalItem, error) {
	return s.searchStructured(ctx, userID, models.RetrievalKindDecision, f)
}

func (s *Store) searchStructured(ctx context.Context, userID int64, kind models.RetrievalKind, f RetrievalFilters) ([]models.RetrievalItem, error) {
	limit := boundedStoreLimit(f.Limit)
	text := strings.TrimSpace(f.Text)
	var sql string
	args := []any{userID, text, f.From, f.To, f.PersonID, limit}
	scoreExpr := `CASE WHEN $2='' THEN 50 WHEN lower(body)=lower($2) THEN 95 WHEN lower(body) LIKE '%'||lower($2)||'%' THEN 80 ELSE greatest(similarity(body,$2),0)*40 END + extract(epoch from created_at)/315360000000.0`
	switch kind {
	case models.RetrievalKindAction:
		args = append(args, f.ActionStatuses)
		sql = fmt.Sprintf(`WITH x AS (SELECT a.id,a.user_id,COALESCE(a.note_id,0) note_id,COALESCE(n.created_at,a.created_at) created_at,a.title body,a.title title,a.status, a.linked_person_id person_id, COALESCE(p.name,'') person_name, a.due_at FROM actions a LEFT JOIN notes n ON n.id=a.note_id AND n.user_id=a.user_id LEFT JOIN people p ON p.id=a.linked_person_id AND p.user_id=a.user_id WHERE a.user_id=$1 AND ($3::timestamptz IS NULL OR COALESCE(n.created_at,a.created_at)>=$3) AND ($4::timestamptz IS NULL OR COALESCE(n.created_at,a.created_at)<$4) AND ($5::bigint IS NULL OR a.linked_person_id=$5) AND (cardinality($7::text[])=0 OR a.status=ANY($7))) SELECT *, %s score FROM x WHERE $2='' OR lower(body) LIKE '%%'||lower($2)||'%%' OR similarity(body,$2)>=0.2 ORDER BY score DESC, created_at DESC LIMIT $6`, scoreExpr)
	case models.RetrievalKindPeopleNote:
		args = append(args, f.PeopleNoteTypes)
		sql = fmt.Sprintf(`WITH x AS (SELECT pn.id,pn.user_id,COALESCE(pn.note_id,0) note_id,COALESCE(n.created_at,pn.created_at) created_at,pn.text body,COALESCE(pn.theme,pn.type) title,pn.type status,pn.person_id,COALESCE(p.name,'') person_name FROM people_notes pn JOIN people p ON p.id=pn.person_id AND p.user_id=pn.user_id LEFT JOIN notes n ON n.id=pn.note_id AND n.user_id=pn.user_id WHERE pn.user_id=$1 AND ($3::timestamptz IS NULL OR COALESCE(n.created_at,pn.created_at)>=$3) AND ($4::timestamptz IS NULL OR COALESCE(n.created_at,pn.created_at)<$4) AND ($5::bigint IS NULL OR pn.person_id=$5) AND (cardinality($7::text[])=0 OR pn.type=ANY($7))) SELECT *, %s score FROM x WHERE $2='' OR lower(body||' '||title) LIKE '%%'||lower($2)||'%%' OR similarity(body,$2)>=0.2 ORDER BY score DESC, created_at DESC LIMIT $6`, scoreExpr)
	case models.RetrievalKindDecision:
		args = append(args, f.DecisionStatuses, f.DecisionTopic)
		sql = fmt.Sprintf(`WITH x AS (SELECT d.id,d.user_id,d.note_id,COALESCE(n.created_at,d.created_at) created_at,d.text body,COALESCE(d.topic,'Decision') title,d.status,d.linked_person_id person_id,COALESCE(p.name,'') person_name FROM decisions d JOIN notes n ON n.id=d.note_id AND n.user_id=d.user_id LEFT JOIN people p ON p.id=d.linked_person_id AND p.user_id=d.user_id WHERE d.user_id=$1 AND ($3::timestamptz IS NULL OR n.created_at>=$3) AND ($4::timestamptz IS NULL OR n.created_at<$4) AND ($5::bigint IS NULL OR d.linked_person_id=$5) AND (cardinality($7::text[])=0 OR d.status=ANY($7)) AND ($8='' OR lower(COALESCE(d.topic,''))=lower($8))) SELECT *, %s score FROM x WHERE $2='' OR lower(body||' '||title) LIKE '%%'||lower($2)||'%%' OR similarity(body,$2)>=0.2 ORDER BY score DESC, created_at DESC LIMIT $6`, scoreExpr)
	}
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.RetrievalItem
	for rows.Next() {
		var it models.RetrievalItem
		var pid *int64
		if kind == models.RetrievalKindAction {
			if err := rows.Scan(&it.RecordID, &it.UserID, &it.SourceNoteID, &it.CreatedAt, &it.Text, &it.Title, &it.Status, &pid, &it.PersonName, &it.DueAt, &it.Score); err != nil {
				return nil, err
			}
		} else if err := rows.Scan(&it.RecordID, &it.UserID, &it.SourceNoteID, &it.CreatedAt, &it.Text, &it.Title, &it.Status, &pid, &it.PersonName, &it.Score); err != nil {
			return nil, err
		}
		it.Kind = kind
		it.PersonID = pid
		it.Text = models.RetrievalSnippet(it.Text, text, 240)
		out = append(out, it)
	}
	return out, rows.Err()
}
func boundedStoreLimit(n int) int {
	if n <= 0 || n > RetrievalPerSourceLimit {
		return RetrievalPerSourceLimit
	}
	return n
}

type TicketMentionBounds struct {
	First *time.Time
	Last  *time.Time
}

func (s *Store) GetTicketMentionBounds(ctx context.Context, userID int64, normalizedKey string) (TicketMentionBounds, error) {
	var first, last *time.Time
	err := s.pool.QueryRow(ctx, `SELECT min(n.created_at), max(n.created_at) FROM entity_mentions em JOIN notes n ON n.id=em.note_id AND n.user_id=em.user_id WHERE em.user_id=$1 AND em.entity_type=$2 AND em.normalized_value=$3`, userID, models.EntityTypeTicket, normalizedKey).Scan(&first, &last)
	if err != nil {
		return TicketMentionBounds{}, err
	}
	return TicketMentionBounds{First: first, Last: last}, nil
}

func (s *Store) SearchTicketFallbackNotes(ctx context.Context, userID int64, normalizedKey string, limit int) ([]models.RetrievalItem, error) {
	limit = boundedStoreLimit(limit)
	pattern := `(^|[^A-Za-z0-9-])` + normalizedKey + `([^A-Za-z0-9-]|$)`
	rows, err := s.pool.Query(ctx, `SELECT id,user_id,created_at,raw_text,COALESCE(summary,'') FROM notes WHERE user_id=$1 AND (raw_text ~* $2 OR COALESCE(summary,'') ~* $2) ORDER BY created_at DESC LIMIT $3`, userID, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.RetrievalItem
	for rows.Next() {
		var id, uid int64
		var created time.Time
		var raw, summary string
		if err := rows.Scan(&id, &uid, &created, &raw, &summary); err != nil {
			return nil, err
		}
		text := raw
		if strings.TrimSpace(summary) != "" {
			text = summary + "\n" + raw
		}
		out = append(out, models.RetrievalItem{Kind: models.RetrievalKindNote, RecordID: id, SourceNoteID: id, UserID: uid, CreatedAt: created, Title: "Note", Text: models.RetrievalSnippet(text, normalizedKey, 240), Score: 70})
	}
	return out, rows.Err()
}

func (s *Store) ListActionsBySourceNoteIDs(ctx context.Context, userID int64, noteIDs []int64, limit int) ([]models.RetrievalItem, error) {
	if len(noteIDs) == 0 {
		return []models.RetrievalItem{}, nil
	}
	limit = boundedStoreLimit(limit)
	rows, err := s.pool.Query(ctx, `SELECT a.id,a.user_id,COALESCE(a.note_id,0),COALESCE(n.created_at,a.created_at),a.title,a.status,COALESCE(p.name,'') FROM actions a LEFT JOIN notes n ON n.id=a.note_id AND n.user_id=a.user_id LEFT JOIN people p ON p.id=a.linked_person_id AND p.user_id=a.user_id WHERE a.user_id=$1 AND a.note_id=ANY($2::bigint[]) ORDER BY COALESCE(n.created_at,a.created_at) DESC, a.id DESC LIMIT $3`, userID, noteIDs, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.RetrievalItem
	for rows.Next() {
		var it models.RetrievalItem
		if err := rows.Scan(&it.RecordID, &it.UserID, &it.SourceNoteID, &it.CreatedAt, &it.Title, &it.Status, &it.PersonName); err != nil {
			return nil, err
		}
		it.Kind = models.RetrievalKindAction
		it.Text = models.RetrievalSnippet(it.Title, "", 240)
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Store) ListDecisionsBySourceNoteIDs(ctx context.Context, userID int64, noteIDs []int64, limit int) ([]models.RetrievalItem, error) {
	if len(noteIDs) == 0 {
		return []models.RetrievalItem{}, nil
	}
	limit = boundedStoreLimit(limit)
	rows, err := s.pool.Query(ctx, `SELECT d.id,d.user_id,d.note_id,COALESCE(n.created_at,d.created_at),d.text,COALESCE(d.topic,'Decision'),d.status FROM decisions d JOIN notes n ON n.id=d.note_id AND n.user_id=d.user_id WHERE d.user_id=$1 AND d.note_id=ANY($2::bigint[]) ORDER BY COALESCE(n.created_at,d.created_at) DESC, d.id DESC LIMIT $3`, userID, noteIDs, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.RetrievalItem
	for rows.Next() {
		var it models.RetrievalItem
		if err := rows.Scan(&it.RecordID, &it.UserID, &it.SourceNoteID, &it.CreatedAt, &it.Text, &it.Title, &it.Status); err != nil {
			return nil, err
		}
		it.Kind = models.RetrievalKindDecision
		it.Text = models.RetrievalSnippet(it.Text, "", 240)
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Store) ListPersonAliases(ctx context.Context, userID, personID int64, limit int) ([]string, error) {
	limit = boundedStoreLimit(limit)
	rows, err := s.pool.Query(ctx, `SELECT alias FROM person_aliases WHERE user_id=$1 AND person_id=$2 ORDER BY created_at DESC LIMIT $3`, userID, personID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) GetPersonLastMention(ctx context.Context, userID, personID int64) (*time.Time, error) {
	var t *time.Time
	err := s.pool.QueryRow(ctx, `SELECT max(at) FROM (
		SELECT COALESCE(n.created_at,pn.created_at) at FROM people_notes pn LEFT JOIN notes n ON n.id=pn.note_id AND n.user_id=pn.user_id WHERE pn.user_id=$1 AND pn.person_id=$2
		UNION ALL SELECT COALESCE(n.created_at,a.created_at) at FROM actions a LEFT JOIN notes n ON n.id=a.note_id AND n.user_id=a.user_id WHERE a.user_id=$1 AND a.linked_person_id=$2
		UNION ALL SELECT COALESCE(n.created_at,d.created_at) at FROM decisions d LEFT JOIN notes n ON n.id=d.note_id AND n.user_id=d.user_id WHERE d.user_id=$1 AND d.linked_person_id=$2
	) x`, userID, personID).Scan(&t)
	return t, err
}

func (s *Store) CountPersonMentionsSince(ctx context.Context, userID, personID int64, since time.Time) (int, error) {
	var c int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM (
		SELECT pn.id FROM people_notes pn LEFT JOIN notes n ON n.id=pn.note_id AND n.user_id=pn.user_id WHERE pn.user_id=$1 AND pn.person_id=$2 AND COALESCE(n.created_at,pn.created_at)>=$3
		UNION ALL SELECT a.id FROM actions a LEFT JOIN notes n ON n.id=a.note_id AND n.user_id=a.user_id WHERE a.user_id=$1 AND a.linked_person_id=$2 AND COALESCE(n.created_at,a.created_at)>=$3
		UNION ALL SELECT d.id FROM decisions d LEFT JOIN notes n ON n.id=d.note_id AND n.user_id=d.user_id WHERE d.user_id=$1 AND d.linked_person_id=$2 AND COALESCE(n.created_at,d.created_at)>=$3
	) x`, userID, personID, since).Scan(&c)
	return c, err
}

func (s *Store) SearchRecentNotesByAliases(ctx context.Context, userID int64, aliases []string, since time.Time, limit int) ([]models.RetrievalItem, error) {
	limit = boundedStoreLimit(limit)
	patterns := make([]string, 0, len(aliases))
	for _, a := range aliases {
		a = models.NormalizeSpace(a)
		if len([]rune(a)) < 4 {
			continue
		}
		patterns = append(patterns, `(^|[^[:alnum:]_])`+regexp.QuoteMeta(a)+`([^[:alnum:]_]|$)`)
	}
	if len(patterns) == 0 {
		return []models.RetrievalItem{}, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT id,user_id,created_at,raw_text,COALESCE(summary,'') FROM notes WHERE user_id=$1 AND created_at>=$2 AND (raw_text ~* ANY($3::text[]) OR COALESCE(summary,'') ~* ANY($3::text[])) ORDER BY created_at DESC LIMIT $4`, userID, since, patterns, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.RetrievalItem
	for rows.Next() {
		var id, uid int64
		var created time.Time
		var raw, summary string
		if err := rows.Scan(&id, &uid, &created, &raw, &summary); err != nil {
			return nil, err
		}
		text := raw
		if strings.TrimSpace(summary) != "" {
			text = summary + "\n" + raw
		}
		out = append(out, models.RetrievalItem{Kind: models.RetrievalKindNote, RecordID: id, SourceNoteID: id, UserID: uid, CreatedAt: created, Title: "Note", Text: models.RetrievalSnippet(text, "", 240)})
	}
	return out, rows.Err()
}
