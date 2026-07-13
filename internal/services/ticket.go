package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spozhydaiev/lead-log/internal/models"
)

const (
	TicketMentionLimit  = 10
	TicketFallbackLimit = 10
	TicketActionLimit   = 10
	TicketDecisionLimit = 10
	TicketSourceLimit   = 20
)

var ErrInvalidTicketKey = errors.New("invalid ticket key")

func (s *Service) GetTicketContext(ctx context.Context, userID int64, ticketKey string) (models.TicketContext, error) {
	started := time.Now()
	log := s.logger.With("operation", "ticket.context", "user_id", userID, "ticket_key_length", len(strings.TrimSpace(ticketKey)), "ticket_key_hash", shortHash(strings.TrimSpace(ticketKey)))
	log.Info("ticket command started")
	normalized, ok := models.NormalizeTicketKey(ticketKey)
	log.Info("ticket key validation completed", "valid", ok)
	if !ok {
		return models.TicketContext{}, ErrInvalidTicketKey
	}
	out := models.TicketContext{TicketKey: normalized, KnownStatus: "not recorded"}
	items, err := s.Retrieve(ctx, models.RetrievalQuery{UserID: userID, Kinds: []models.RetrievalKind{models.RetrievalKindEntityMention}, EntityType: models.EntityTypeTicket, EntityValue: normalized, Limit: TicketMentionLimit})
	if err != nil {
		log.Error("ticket command failed", "failure_stage", "exact_lookup", "error", err)
		return out, err
	}
	log.Info("ticket exact mentions loaded", "exact_mention_count", len(items))
	bounds, err := s.store.GetTicketMentionBounds(ctx, userID, normalized)
	if err != nil {
		log.Error("ticket command failed", "failure_stage", "mention_bounds", "error", err)
		return out, err
	}
	out.FirstMentionAt, out.LastMentionAt = bounds.First, bounds.Last
	noteSeen := map[int64]bool{}
	noteIDs := make([]int64, 0, TicketSourceLimit)
	for _, it := range items {
		if it.SourceNoteID == 0 || noteSeen[it.SourceNoteID] {
			continue
		}
		noteSeen[it.SourceNoteID] = true
		noteIDs = append(noteIDs, it.SourceNoteID)
		out.Mentions = append(out.Mentions, models.TicketMention{SourceNoteID: it.SourceNoteID, Date: it.CreatedAt, Snippet: firstNonEmpty(it.Text, it.Context, it.Title), FromEntity: true})
	}
	fallback, err := s.store.SearchTicketFallbackNotes(ctx, userID, normalized, TicketFallbackLimit)
	if err != nil {
		log.Error("ticket command failed", "failure_stage", "raw_fallback", "error", err)
		return out, err
	}
	fallbackAdded := 0
	for _, it := range fallback {
		if it.SourceNoteID == 0 || noteSeen[it.SourceNoteID] || len(out.Mentions) >= TicketMentionLimit {
			continue
		}
		noteSeen[it.SourceNoteID] = true
		noteIDs = append(noteIDs, it.SourceNoteID)
		fallbackAdded++
		out.Mentions = append(out.Mentions, models.TicketMention{SourceNoteID: it.SourceNoteID, Date: it.CreatedAt, Snippet: it.Text, FromEntity: false})
		if out.FirstMentionAt == nil || it.CreatedAt.Before(*out.FirstMentionAt) {
			t := it.CreatedAt
			out.FirstMentionAt = &t
		}
		if out.LastMentionAt == nil || it.CreatedAt.After(*out.LastMentionAt) {
			t := it.CreatedAt
			out.LastMentionAt = &t
		}
	}
	log.Info("ticket fallback notes loaded", "fallback_note_count", fallbackAdded)
	sort.SliceStable(out.Mentions, func(i, j int) bool { return out.Mentions[i].Date.After(out.Mentions[j].Date) })
	if len(noteIDs) > TicketSourceLimit {
		noteIDs = noteIDs[:TicketSourceLimit]
	}
	actions, err := s.store.ListActionsBySourceNoteIDs(ctx, userID, noteIDs, TicketActionLimit)
	if err != nil {
		log.Error("ticket command failed", "failure_stage", "actions", "error", err)
		return out, err
	}
	for _, a := range actions {
		out.Actions = append(out.Actions, models.TicketAction{ID: a.RecordID, Title: a.Title, Status: a.Status, PersonName: a.PersonName, SourceNoteID: a.SourceNoteID, Date: a.CreatedAt, AssociationType: associationType(a.Title, normalized)})
	}
	decisions, err := s.store.ListDecisionsBySourceNoteIDs(ctx, userID, noteIDs, TicketDecisionLimit)
	if err != nil {
		log.Error("ticket command failed", "failure_stage", "decisions", "error", err)
		return out, err
	}
	for _, d := range decisions {
		out.Decisions = append(out.Decisions, models.TicketDecision{ID: d.RecordID, Text: d.Text, Status: d.Status, SourceNoteID: d.SourceNoteID, Date: d.CreatedAt, AssociationType: associationType(d.Text, normalized)})
	}
	out.Sources = ticketSources(out.Mentions, TicketSourceLimit)
	log.Info("ticket context completed", "associated_action_count", len(out.Actions), "associated_decision_count", len(out.Decisions), "final_source_count", len(out.Sources), "no_result", len(out.Mentions) == 0, "duration_ms", time.Since(started).Milliseconds())
	return out, nil
}

func (s *Service) Ticket(ctx context.Context, userID int64, arg string) (string, error) {
	if strings.TrimSpace(arg) == "" {
		return s.language.CommonMessages().TicketUsage, nil
	}
	ctxModel, err := s.GetTicketContext(ctx, userID, arg)
	if errors.Is(err, ErrInvalidTicketKey) {
		return s.language.CommonMessages().TicketInvalid, nil
	}
	if err != nil {
		return "", err
	}
	return formatTicketContext(ctxModel, s.language), nil
}

func formatTicketContext(tc models.TicketContext, lang models.ResponseLanguage) string {
	m := lang.CommonMessages()
	if len(tc.Mentions) == 0 {
		return fmt.Sprintf(m.TicketNoResults, tc.TicketKey)
	}
	var b strings.Builder
	b.WriteString(tc.TicketKey + "\n\n")
	if tc.LastMentionAt != nil {
		fmt.Fprintf(&b, "%s: %s\n", m.TicketLastMentioned, formatTicketDate(*tc.LastMentionAt, true))
	}
	if tc.FirstMentionAt != nil {
		fmt.Fprintf(&b, "%s: %s\n", m.TicketFirstMentioned, formatTicketDate(*tc.FirstMentionAt, true))
	}
	fmt.Fprintf(&b, "%s: %s\n", m.TicketKnownStatus, m.TicketStatusNotRecorded)
	writeMentions(&b, m, tc.Mentions)
	writeActions(&b, m, tc.Actions)
	writeDecisions(&b, m, tc.Decisions)
	if len(tc.Sources) > 0 {
		fmt.Fprintf(&b, "\n%s:\n", m.TicketSources)
		for _, s := range tc.Sources {
			fmt.Fprintf(&b, "- note #%d — %s\n", s.NoteID, formatTicketDate(s.Date, true))
		}
	}
	return strings.TrimSpace(b.String())
}

func writeMentions(b *strings.Builder, m models.CommonMessages, mentions []models.TicketMention) {
	if len(mentions) == 0 {
		return
	}
	fmt.Fprintf(b, "\n%s:\n", m.TicketRecentMentions)
	for i, x := range mentions {
		fmt.Fprintf(b, "%d. %s\n   note #%d — %s\n\n", i+1, safeSnippet(x.Snippet, 180), x.SourceNoteID, formatTicketDate(x.Date, false))
	}
}
func writeActions(b *strings.Builder, m models.CommonMessages, actions []models.TicketAction) {
	if len(actions) == 0 {
		return
	}
	fmt.Fprintf(b, "%s:\n", m.TicketRelatedActions)
	for _, a := range actions {
		rel := m.TicketPossible
		if a.AssociationType == models.TicketAssociationDirect {
			rel = m.TicketDirect
		}
		person := ""
		if strings.TrimSpace(a.PersonName) != "" {
			person = " — " + a.PersonName
		}
		fmt.Fprintf(b, "- %s (%s%s) [%s, action #%d, note #%d]\n", safeSnippet(a.Title, 160), a.Status, person, rel, a.ID, a.SourceNoteID)
	}
}
func writeDecisions(b *strings.Builder, m models.CommonMessages, decisions []models.TicketDecision) {
	if len(decisions) == 0 {
		return
	}
	fmt.Fprintf(b, "\n%s:\n", m.TicketDecisions)
	for _, d := range decisions {
		rel := m.TicketPossible
		if d.AssociationType == models.TicketAssociationDirect {
			rel = m.TicketDirect
		}
		fmt.Fprintf(b, "- %s (%s) [%s, decision #%d, note #%d]\n", safeSnippet(d.Text, 160), d.Status, rel, d.ID, d.SourceNoteID)
	}
}

func associationType(text, key string) string {
	if containsTicketToken(text, key) {
		return models.TicketAssociationDirect
	}
	return models.TicketAssociationPossible
}
func containsTicketToken(text, key string) bool {
	for _, f := range strings.FieldsFunc(text, func(r rune) bool {
		return !(r == '-' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z')
	}) {
		if n, ok := models.NormalizeTicketKey(f); ok && n == key {
			return true
		}
	}
	return false
}
func ticketSources(ms []models.TicketMention, limit int) []models.TicketSource {
	seen := map[int64]bool{}
	var out []models.TicketSource
	for _, m := range ms {
		if !seen[m.SourceNoteID] {
			seen[m.SourceNoteID] = true
			out = append(out, models.TicketSource{NoteID: m.SourceNoteID, Date: m.Date})
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}
func safeSnippet(s string, max int) string {
	s = models.NormalizeSpace(s)
	if s == "" {
		return "—"
	}
	r := []rune(s)
	if len(r) > max {
		return string(r[:max-1]) + "…"
	}
	if !utf8.ValidString(s) {
		return strings.ToValidUTF8(s, "")
	}
	return s
}
func formatTicketDate(t time.Time, year bool) string {
	if year {
		return t.Format("2 January 2006")
	}
	return t.Format("2 January")
}
func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
func shortHash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:])[:12] }
