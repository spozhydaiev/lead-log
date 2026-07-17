package services

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spozhydaiev/lead-log/internal/logging"
	"github.com/spozhydaiev/lead-log/internal/models"
)

const (
	AgendaMustDiscussLimit   = 10
	AgendaFollowUpsLimit     = 8
	AgendaOpenQuestionsLimit = 8
	AgendaDecisionsLimit     = 5
	AgendaPositiveLimit      = 5
	AgendaContextLimit       = 5
	AgendaSourceLimit        = 30
)

func (s *Service) Agenda(ctx context.Context, userID int64, arg string) (string, error) {
	m := s.language.CommonMessages()
	if strings.TrimSpace(arg) == "" {
		return m.AgendaUsage, nil
	}
	started := time.Now()
	log := s.logger.With("component", "agenda", "operation", "build", "operation_id", logging.OperationID(ctx))
	pc, found, err := s.GetPersonContext(ctx, userID, strings.TrimSpace(arg))
	if errors.Is(err, ErrAmbiguousPerson) {
		log.Info("agenda completed", "result", "ambiguous")
		return m.AgendaAmbiguous, nil
	}
	if err != nil {
		log.Error("agenda failed", logging.WithSafeError([]any{"failure_stage", "retrieve"}, err)...)
		return "", err
	}
	if !found {
		log.Info("agenda completed", "result", "not_found", "duration_ms", time.Since(started).Milliseconds())
		return m.AgendaNotFound, nil
	}
	agenda := BuildAgendaFromPersonContext(pc, time.Now())
	if agendaEmpty(agenda) {
		log.Info("agenda completed", "result", "empty", "duration_ms", time.Since(started).Milliseconds())
		return m.AgendaEmpty, nil
	}
	log.Info("agenda completed", "result", "success", "must_discuss_count", len(agenda.MustDiscuss), "follow_up_count", len(agenda.FollowUps), "open_question_count", len(agenda.OpenQuestions), "decision_count", len(agenda.Decisions), "positive_note_count", len(agenda.PositiveNotes), "context_count", len(agenda.Context), "duration_ms", time.Since(started).Milliseconds())
	return FormatPersonAgenda(agenda, s.language), nil
}

func BuildAgendaFromPersonContext(pc models.PersonContext, now time.Time) models.PersonAgenda {
	a := models.PersonAgenda{CanonicalName: pc.CanonicalName, GeneratedAt: now}
	seen := map[string]bool{}
	add := func(dst *[]models.AgendaItem, item models.AgendaItem) {
		item.Text = strings.TrimSpace(item.Text)
		if item.Text == "" || item.SourceNoteID <= 0 || item.SourceDate.IsZero() {
			return
		}
		key := dedupeKey(item.SourceNoteID, item.Text)
		if seen[key] {
			return
		}
		seen[key] = true
		*dst = append(*dst, item)
	}
	for _, x := range pc.OpenActions {
		if x.Status != "open" {
			continue
		}
		id := x.ID
		p := models.AgendaPriorityNormal
		if x.DueAt != nil && x.DueAt.Before(now) {
			p = models.AgendaPriorityHigh
		}
		add(&a.MustDiscuss, models.AgendaItem{Kind: models.AgendaItemOpenAction, Text: x.Title, SourceNoteID: x.SourceNoteID, SourceDate: x.Date, ActionID: &id, DueAt: x.DueAt, Priority: p})
	}
	cutoff60, cutoff30, cutoff14 := now.AddDate(0, 0, -60), now.AddDate(0, 0, -30), now.AddDate(0, 0, -14)
	completedFacts := map[string]bool{}
	for _, x := range pc.CompletedActions {
		if x.Status == "done" {
			completedFacts[dedupeKey(x.SourceNoteID, x.Title)] = true
		}
	}
	for _, x := range pc.Commitments {
		if !x.Date.Before(cutoff60) && !completedFacts[dedupeKey(x.SourceNoteID, x.Text)] {
			add(&a.MustDiscuss, agendaContextItem(x, models.AgendaItemCommitment, models.AgendaPriorityNormal))
		}
	}
	for _, x := range pc.FollowUps {
		if !x.Date.Before(cutoff60) {
			add(&a.FollowUps, agendaContextItem(x, models.AgendaItemFollowUp, models.AgendaPriorityNormal))
		}
	}
	for _, x := range pc.Concerns {
		if !x.Date.Before(cutoff30) {
			add(&a.MustDiscuss, agendaContextItem(x, models.AgendaItemConcern, models.AgendaPriorityHigh))
		}
	}
	for _, x := range pc.OpenQuestions {
		if !x.Date.Before(cutoff30) {
			add(&a.OpenQuestions, agendaContextItem(x, models.AgendaItemOpenQuestion, models.AgendaPriorityNormal))
		}
	}
	for _, x := range pc.Decisions {
		if !x.Date.Before(cutoff30) && (x.Status == "active" || x.Status == "recorded" || x.Status == "") {
			add(&a.Decisions, models.AgendaItem{Kind: models.AgendaItemDecision, Text: x.Text, SourceNoteID: x.SourceNoteID, SourceDate: x.Date, Priority: models.AgendaPriorityNormal})
		}
	}
	for _, xs := range [][]models.PersonContextItem{pc.Achievements, pc.Feedback} {
		for _, x := range xs {
			if !x.Date.Before(cutoff30) && (x.Type == "" || x.Type == "achievement" || x.Type == "feedback" || x.Type == "positive_signal" || x.Type == "collaboration") {
				add(&a.PositiveNotes, agendaContextItem(x, models.AgendaItemAchievement, models.AgendaPriorityLow))
			}
		}
	}
	for _, x := range pc.CompletedActions {
		if x.Status == "done" && !x.Date.Before(cutoff14) {
			id := x.ID
			add(&a.PositiveNotes, models.AgendaItem{Kind: models.AgendaItemAchievement, Text: x.Title, SourceNoteID: x.SourceNoteID, SourceDate: x.Date, ActionID: &id, Priority: models.AgendaPriorityLow})
		}
	}
	for _, x := range pc.RecentNotes {
		if !x.Date.Before(cutoff14) {
			add(&a.Context, agendaContextItem(x, models.AgendaItemContext, models.AgendaPriorityLow))
		}
	}
	for _, x := range pc.PossibleMentions {
		if !x.Date.Before(cutoff14) {
			item := agendaContextItem(x, models.AgendaItemContext, models.AgendaPriorityLow)
			item.IsInferred = true
			add(&a.Context, item)
		}
	}
	sortAndLimit := func(xs *[]models.AgendaItem, limit int) {
		sort.SliceStable(*xs, func(i, j int) bool {
			pi, pj := priorityRank((*xs)[i].Priority), priorityRank((*xs)[j].Priority)
			if pi != pj {
				return pi < pj
			}
			return (*xs)[i].SourceDate.After((*xs)[j].SourceDate)
		})
		if len(*xs) > limit {
			if xs == &a.MustDiscuss {
				a.HiddenMustDiscuss = len(*xs) - limit
			}
			*xs = (*xs)[:limit]
		}
	}
	sortAndLimit(&a.MustDiscuss, AgendaMustDiscussLimit)
	sortAndLimit(&a.FollowUps, AgendaFollowUpsLimit)
	sortAndLimit(&a.OpenQuestions, AgendaOpenQuestionsLimit)
	sortAndLimit(&a.Decisions, AgendaDecisionsLimit)
	sortAndLimit(&a.PositiveNotes, AgendaPositiveLimit)
	sortAndLimit(&a.Context, AgendaContextLimit)
	sources := map[int64]time.Time{}
	for _, xs := range [][]models.AgendaItem{a.MustDiscuss, a.FollowUps, a.OpenQuestions, a.Decisions, a.PositiveNotes, a.Context} {
		for _, x := range xs {
			if old, ok := sources[x.SourceNoteID]; !ok || x.SourceDate.After(old) {
				sources[x.SourceNoteID] = x.SourceDate
			}
		}
	}
	for id, d := range sources {
		a.Sources = append(a.Sources, models.AgendaSource{NoteID: id, Date: d})
	}
	sort.Slice(a.Sources, func(i, j int) bool { return a.Sources[i].Date.After(a.Sources[j].Date) })
	if len(a.Sources) > AgendaSourceLimit {
		a.Sources = a.Sources[:AgendaSourceLimit]
	}
	return a
}
func agendaContextItem(x models.PersonContextItem, k models.AgendaItemKind, p models.AgendaPriority) models.AgendaItem {
	return models.AgendaItem{Kind: k, Text: x.Text, SourceNoteID: x.SourceNoteID, SourceDate: x.Date, Priority: p}
}
func priorityRank(p models.AgendaPriority) int {
	if p == models.AgendaPriorityHigh {
		return 0
	}
	if p == models.AgendaPriorityNormal {
		return 1
	}
	return 2
}
func agendaEmpty(a models.PersonAgenda) bool {
	return len(a.MustDiscuss)+len(a.FollowUps)+len(a.OpenQuestions)+len(a.Decisions)+len(a.PositiveNotes)+len(a.Context) == 0
}

func FormatPersonAgenda(a models.PersonAgenda, lang models.ResponseLanguage) string {
	l := lang.AgendaLabels()
	var b strings.Builder
	fmt.Fprintf(&b, "%s — %s\n", l.Title, a.CanonicalName)
	write := func(title string, xs []models.AgendaItem) {
		if len(xs) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n%s:\n", title)
		for i, x := range xs {
			fmt.Fprintf(&b, "%d. %s\n   %s", i+1, safeSnippet(x.Text, 180), agendaKindLabel(x.Kind, l))
			if x.ActionID != nil {
				fmt.Fprintf(&b, " #%d", *x.ActionID)
			}
			fmt.Fprintf(&b, "\n   note #%d — %s\n", x.SourceNoteID, formatTicketDate(x.SourceDate, false))
		}
	}
	write(l.MustDiscuss, a.MustDiscuss)
	if a.HiddenMustDiscuss > 0 {
		fmt.Fprintf(&b, "%s\n", fmt.Sprintf(l.MoreOpen, a.HiddenMustDiscuss))
	}
	write(l.FollowUps, a.FollowUps)
	write(l.OpenQuestions, a.OpenQuestions)
	write(l.Decisions, a.Decisions)
	write(l.PositiveNotes, a.PositiveNotes)
	write(l.Context, a.Context)
	fmt.Fprintf(&b, "\n%s", fmt.Sprintf(l.Prepared, len(a.Sources)))
	return strings.TrimSpace(b.String())
}
func agendaKindLabel(k models.AgendaItemKind, l models.AgendaLabels) string {
	switch k {
	case models.AgendaItemOpenAction:
		return l.OpenAction
	case models.AgendaItemCommitment:
		return l.Commitment
	case models.AgendaItemFollowUp:
		return l.FollowUp
	case models.AgendaItemOpenQuestion:
		return l.OpenQuestion
	case models.AgendaItemConcern:
		return l.Concern
	case models.AgendaItemDecision:
		return l.Decision
	case models.AgendaItemAchievement:
		return l.Achievement
	default:
		return l.ContextKind
	}
}
