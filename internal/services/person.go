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
	PersonContextDays           = 30
	PersonPeopleNotesLimit      = 15
	PersonOpenActionsLimit      = 15
	PersonCompletedActionsLimit = 5
	PersonDecisionsLimit        = 10
	PersonFallbackNotesLimit    = 10
	PersonSourceLimit           = 30
)

var ErrAmbiguousPerson = errors.New("ambiguous person alias")

func (s *Service) Person(ctx context.Context, userID int64, arg string) (string, error) {
	name := strings.TrimSpace(arg)
	m := s.language.CommonMessages()
	if name == "" {
		return m.PersonUsage, nil
	}
	pc, found, err := s.GetPersonContext(ctx, userID, name)
	if errors.Is(err, ErrAmbiguousPerson) {
		return m.PersonAmbiguous, nil
	}
	if err != nil {
		return "", err
	}
	if !found {
		return fmt.Sprintf(m.PersonNotFound, safeSnippet(name, 80)), nil
	}
	return formatPersonContext(pc, s.language), nil
}

func (s *Service) GetPersonContext(ctx context.Context, userID int64, inputName string) (models.PersonContext, bool, error) {
	started := time.Now()
	since := time.Now().AddDate(0, 0, -60)
	contextSince := time.Now().AddDate(0, 0, -PersonContextDays)
	log := s.logger.With("component", "person_context", "operation", "retrieve", "operation_id", logging.OperationID(ctx))
	p, err := s.store.ResolvePerson(ctx, userID, inputName)
	if err != nil {
		log.Error("person context failed", logging.WithSafeError([]any{"failure_stage", "resolve"}, err)...)
		return models.PersonContext{}, false, err
	}
	if p.Ambiguous {
		log.Warn("person context ambiguous", "error_class", "ambiguous_person_alias", "result", "ambiguous")
		return models.PersonContext{}, false, ErrAmbiguousPerson
	}
	if !p.Found {
		log.Info("person context completed", "result", "not_found", "duration_ms", time.Since(started).Milliseconds())
		return models.PersonContext{}, false, nil
	}
	out := models.PersonContext{CanonicalName: p.Name}
	pid := p.ID
	pn, err := s.Retrieve(ctx, models.RetrievalQuery{UserID: userID, From: &since, Kinds: []models.RetrievalKind{models.RetrievalKindPeopleNote}, PersonID: &pid, Limit: PersonPeopleNotesLimit})
	if err != nil {
		return out, true, err
	}
	open, err := s.Retrieve(ctx, models.RetrievalQuery{UserID: userID, Kinds: []models.RetrievalKind{models.RetrievalKindAction}, PersonID: &pid, ActionStatuses: []string{"open"}, Limit: PersonOpenActionsLimit})
	if err != nil {
		return out, true, err
	}
	done, err := s.Retrieve(ctx, models.RetrievalQuery{UserID: userID, From: &contextSince, Kinds: []models.RetrievalKind{models.RetrievalKindAction}, PersonID: &pid, ActionStatuses: []string{"done"}, Limit: PersonCompletedActionsLimit})
	if err != nil {
		return out, true, err
	}
	dec, err := s.Retrieve(ctx, models.RetrievalQuery{UserID: userID, From: &contextSince, Kinds: []models.RetrievalKind{models.RetrievalKindDecision}, PersonID: &pid, DecisionStatuses: []string{models.DecisionStatusActive}, Limit: PersonDecisionsLimit})
	if err != nil {
		return out, true, err
	}
	last, err := s.store.GetPersonLastMention(ctx, userID, pid)
	if err != nil {
		return out, true, err
	}
	out.LastMentionAt = last
	cnt, err := s.store.CountPersonMentionsSince(ctx, userID, pid, contextSince)
	if err != nil {
		return out, true, err
	}
	out.MentionCount = cnt
	structuredNotes := map[int64]bool{}
	decisionKeys := map[string]bool{}
	for _, d := range dec {
		out.Decisions = append(out.Decisions, models.PersonContextDecision{Text: d.Text, Topic: d.Title, Status: d.Status, SourceNoteID: d.SourceNoteID, Date: d.CreatedAt})
		structuredNotes[d.SourceNoteID] = true
		decisionKeys[dedupeKey(d.SourceNoteID, d.Text)] = true
	}
	for _, a := range open {
		out.OpenActions = append(out.OpenActions, models.PersonContextAction{ID: a.RecordID, Title: a.Title, Status: a.Status, SourceNoteID: a.SourceNoteID, Date: a.CreatedAt, DueAt: a.DueAt})
		structuredNotes[a.SourceNoteID] = true
	}
	for _, a := range done {
		out.CompletedActions = append(out.CompletedActions, models.PersonContextAction{ID: a.RecordID, Title: a.Title, Status: a.Status, SourceNoteID: a.SourceNoteID, Date: a.CreatedAt, DueAt: a.DueAt})
		structuredNotes[a.SourceNoteID] = true
	}
	for _, it := range pn {
		structuredNotes[it.SourceNoteID] = true
		if it.Status == "decision" && decisionKeys[dedupeKey(it.SourceNoteID, it.Text)] {
			continue
		}
		item := models.PersonContextItem{Text: it.Text, Type: it.Status, SourceNoteID: it.SourceNoteID, Date: it.CreatedAt}
		switch it.Status {
		case "commitment":
			out.Commitments = append(out.Commitments, item)
		case "follow_up_needed", "follow_up":
			out.FollowUps = append(out.FollowUps, item)
		case "feedback", "positive_signal", "growth_topic", "review_evidence", "collaboration":
			out.Feedback = append(out.Feedback, item)
		case "achievement":
			out.Achievements = append(out.Achievements, item)
		case "concern", "risk", "blocker":
			out.Concerns = append(out.Concerns, item)
		case "question":
			out.OpenQuestions = append(out.OpenQuestions, item)
		case "decision":
			out.Decisions = append(out.Decisions, models.PersonContextDecision{Text: it.Text, Status: "recorded", SourceNoteID: it.SourceNoteID, Date: it.CreatedAt})
		default:
			out.RecentNotes = append(out.RecentNotes, item)
		}
	}
	aliases, _ := s.store.ListPersonAliases(ctx, userID, pid, 20)
	aliases = append(aliases, p.Name)
	fb, err := s.store.SearchRecentNotesByAliases(ctx, userID, aliases, contextSince, PersonFallbackNotesLimit)
	if err != nil {
		return out, true, err
	}
	for _, it := range fb {
		if structuredNotes[it.SourceNoteID] {
			continue
		}
		out.PossibleMentions = append(out.PossibleMentions, models.PersonContextItem{Text: it.Text, Type: "possible", SourceNoteID: it.SourceNoteID, Date: it.CreatedAt})
		if out.LastMentionAt == nil || it.CreatedAt.After(*out.LastMentionAt) {
			t := it.CreatedAt
			out.LastMentionAt = &t
		}
	}
	out.Sources = personSources(out, PersonSourceLimit)
	log.Info("person context completed", "result", "success", "people_notes_count", len(pn), "open_actions_count", len(open), "completed_actions_count", len(done), "decision_count", len(dec), "fallback_note_count", len(out.PossibleMentions), "source_count", len(out.Sources), "duration_ms", time.Since(started).Milliseconds())
	return out, true, nil
}

func dedupeKey(noteID int64, text string) string {
	return fmt.Sprintf("%d:%s", noteID, strings.ToLower(models.NormalizeSpace(text)))
}

func personSources(pc models.PersonContext, limit int) []models.PersonContextSource {
	seen := map[int64]time.Time{}
	add := func(id int64, d time.Time) {
		if id == 0 {
			return
		}
		if old, ok := seen[id]; !ok || d.After(old) {
			seen[id] = d
		}
	}
	for _, x := range pc.RecentNotes {
		add(x.SourceNoteID, x.Date)
	}
	for _, x := range pc.Commitments {
		add(x.SourceNoteID, x.Date)
	}
	for _, x := range pc.FollowUps {
		add(x.SourceNoteID, x.Date)
	}
	for _, x := range pc.Feedback {
		add(x.SourceNoteID, x.Date)
	}
	for _, x := range pc.Achievements {
		add(x.SourceNoteID, x.Date)
	}
	for _, x := range pc.Concerns {
		add(x.SourceNoteID, x.Date)
	}
	for _, x := range pc.OpenQuestions {
		add(x.SourceNoteID, x.Date)
	}
	for _, x := range pc.OpenActions {
		add(x.SourceNoteID, x.Date)
	}
	for _, x := range pc.CompletedActions {
		add(x.SourceNoteID, x.Date)
	}
	for _, x := range pc.Decisions {
		add(x.SourceNoteID, x.Date)
	}
	for _, x := range pc.PossibleMentions {
		add(x.SourceNoteID, x.Date)
	}
	out := make([]models.PersonContextSource, 0, len(seen))
	for id, d := range seen {
		out = append(out, models.PersonContextSource{NoteID: id, Date: d})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date.After(out[j].Date) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func formatPersonContext(pc models.PersonContext, lang models.ResponseLanguage) string {
	pc = personContextWithin(pc, time.Now().AddDate(0, 0, -PersonContextDays))
	m := lang.CommonMessages()
	if pc.LastMentionAt == nil && len(pc.Sources) == 0 {
		return fmt.Sprintf(m.PersonNoContext, pc.CanonicalName)
	}
	var b strings.Builder
	b.WriteString(pc.CanonicalName + "\n")
	fmt.Fprintf(&b, "%s: %s\n", m.PersonContextPeriod, m.PersonLast30Days)
	if pc.LastMentionAt != nil {
		fmt.Fprintf(&b, "%s: %s\n", m.PersonLastMentioned, formatTicketDate(*pc.LastMentionAt, true))
	}
	fmt.Fprintf(&b, "%s: %d\n", m.PersonMentionCount30Days, pc.MentionCount)
	writePersonActions(&b, m.PersonOpenActions, pc.OpenActions)
	writePersonActions(&b, m.PersonCompletedActions, pc.CompletedActions)
	writeItems := func(title string, xs []models.PersonContextItem) {
		if len(xs) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n%s:\n", title)
		for _, x := range xs {
			fmt.Fprintf(&b, "- %s\n  note #%d — %s\n", safeSnippet(x.Text, 180), x.SourceNoteID, formatTicketDate(x.Date, false))
		}
	}
	writeItems(m.PersonCommitments, pc.Commitments)
	writeItems(m.PersonFollowUps, pc.FollowUps)
	writeItems(m.PersonFeedback, pc.Feedback)
	writeItems(m.PersonAchievements, pc.Achievements)
	writeItems(m.PersonConcerns, pc.Concerns)
	writePersonDecisions(&b, m.PersonDecisions, pc.Decisions)
	writeItems(m.PersonOpenQuestions, pc.OpenQuestions)
	writeItems(m.PersonRecentContext, pc.RecentNotes)
	writeItems(m.PersonPossibleMentions, pc.PossibleMentions)
	if len(pc.Sources) > 0 {
		fmt.Fprintf(&b, "\n%s:\n", m.PersonSources)
		for _, s := range pc.Sources {
			fmt.Fprintf(&b, "- note #%d — %s\n", s.NoteID, formatTicketDate(s.Date, true))
		}
	}
	return strings.TrimSpace(b.String())
}

func personContextWithin(pc models.PersonContext, since time.Time) models.PersonContext {
	filterItems := func(xs []models.PersonContextItem) []models.PersonContextItem {
		out := make([]models.PersonContextItem, 0, len(xs))
		for _, x := range xs {
			if !x.Date.Before(since) {
				out = append(out, x)
			}
		}
		return out
	}
	pc.RecentNotes = filterItems(pc.RecentNotes)
	pc.Commitments = filterItems(pc.Commitments)
	pc.FollowUps = filterItems(pc.FollowUps)
	pc.Feedback = filterItems(pc.Feedback)
	pc.Achievements = filterItems(pc.Achievements)
	pc.Concerns = filterItems(pc.Concerns)
	pc.OpenQuestions = filterItems(pc.OpenQuestions)
	pc.PossibleMentions = filterItems(pc.PossibleMentions)
	pc.Sources = personSources(pc, PersonSourceLimit)
	return pc
}
func writePersonActions(b *strings.Builder, title string, xs []models.PersonContextAction) {
	if len(xs) == 0 {
		return
	}
	fmt.Fprintf(b, "\n%s:\n", title)
	for _, x := range xs {
		fmt.Fprintf(b, "- #%d %s (%s)\n  note #%d — %s\n", x.ID, safeSnippet(x.Title, 160), x.Status, x.SourceNoteID, formatTicketDate(x.Date, false))
	}
}
func writePersonDecisions(b *strings.Builder, title string, xs []models.PersonContextDecision) {
	if len(xs) == 0 {
		return
	}
	fmt.Fprintf(b, "\n%s:\n", title)
	for _, x := range xs {
		topic := ""
		if strings.TrimSpace(x.Topic) != "" && x.Topic != "Decision" {
			topic = " — " + x.Topic
		}
		fmt.Fprintf(b, "- %s%s (%s)\n  note #%d — %s\n", safeSnippet(x.Text, 180), topic, x.Status, x.SourceNoteID, formatTicketDate(x.Date, false))
	}
}
