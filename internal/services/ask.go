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

const AskPlanningPromptVersion = "v1"
const AskAnswerPromptVersion = "v1"
const MaxAskQuestionRunes = 1500
const MaxAskCandidates = 30
const MaxAskTotalTextRunes = 6000
const MaxAskSnippetRunes = 500

var ErrInvalidAskQuestion = errors.New("invalid ask question")
var ErrAskPlanning = errors.New("ask planning failed")
var ErrAskAnswer = errors.New("ask answer failed")

func (s *Service) Ask(ctx context.Context, userID int64, question string) (string, error) {
	started := time.Now()
	q := strings.TrimSpace(question)
	if q == "" {
		return s.language.CommonMessages().AskUsage, nil
	}
	if utf8.RuneCountInString(q) > MaxAskQuestionRunes {
		return s.language.CommonMessages().AskTooLong, nil
	}
	log := s.logger.With("operation", "ask", "user_id", userID, "question_length", len(q), "question_hash", hashPrefix(q), "planning_prompt_version", AskPlanningPromptVersion, "answer_prompt_version", AskAnswerPromptVersion, "model", s.llm.Model())
	log.Info("ask started")
	det := deterministicAskIntent(q)
	planStart := time.Now()
	intent, err := s.llm.PlanAskQuery(ctx, q, time.Now().In(s.dailyLocation).Format("2006-01-02"), s.dailyLocation.String(), string(s.language))
	if err != nil {
		log.Error("ask failed", "failure_stage", "planning", "duration_ms", time.Since(planStart).Milliseconds(), "error", err)
		return "", fmt.Errorf("%w: %v", ErrAskPlanning, err)
	}
	intent = mergeDeterministicIntent(intent, det)
	intent, err = normalizeAskIntent(intent, q, time.Now(), s.dailyLocation)
	if err != nil {
		log.Error("ask failed", "failure_stage", "intent_validation", "error", err)
		return "", fmt.Errorf("%w: %v", ErrAskPlanning, err)
	}
	log.Info("ask intent planned", "planner_duration_ms", time.Since(planStart).Milliseconds(), "intent_type", intent.IntentType, "date_range_type", intent.DateRange.Type, "requested_kinds", kindStrings(intent.Kinds))
	queries := BuildRetrievalPlan(userID, intent)
	log.Info("ask retrieval plan built", "retrieval_query_count", len(queries))
	var all []models.RetrievalItem
	for _, rq := range queries {
		items, err := s.Retrieve(ctx, rq)
		if err != nil {
			log.Error("ask failed", "failure_stage", "retrieval", "error", err)
			return "", err
		}
		all = append(all, items...)
	}
	candidates := selectAskCandidates(all)
	log.Info("ask candidates selected", "raw_candidate_count", len(all), "final_candidate_count", len(candidates), "candidate_counts", askCandidateCounts(candidates))
	if len(candidates) == 0 {
		log.Info("ask no results", "no_result", true, "total_duration_ms", time.Since(started).Milliseconds())
		return s.language.CommonMessages().AskNoResults, nil
	}
	answerStart := time.Now()
	ans, err := s.llm.GenerateAskAnswer(ctx, q, intent, candidates, string(s.language))
	if err != nil {
		log.Error("ask failed", "failure_stage", "answer_generation", "duration_ms", time.Since(answerStart).Milliseconds(), "error", err)
		return "", fmt.Errorf("%w: %v", ErrAskAnswer, err)
	}
	ans = validateAskAnswer(ans, candidates)
	log.Info("ask completed", "answer_generation_duration_ms", time.Since(answerStart).Milliseconds(), "cited_source_count", citedCount(ans), "insufficient_data", ans.InsufficientData, "total_duration_ms", time.Since(started).Milliseconds())
	return formatAskAnswer(ans, candidates, s.language), nil
}

func deterministicAskIntent(q string) models.AskIntent {
	lower := strings.ToLower(q)
	out := models.AskIntent{DateRange: models.AskDateRange{Type: ""}}
	for _, m := range ticketRegexp().FindAllString(q, -1) {
		if n, ok := models.NormalizeTicketKey(m); ok {
			out.Entities = append(out.Entities, models.AskEntity{Type: models.EntityTypeTicket, Value: n})
			out.IntentType = models.AskIntentLatestMention
			out.SortOrder = "newest"
		}
	}
	switch {
	case strings.Contains(lower, "вчора") || strings.Contains(lower, "учора") || strings.Contains(lower, "yesterday"):
		out.DateRange.Type = models.AskDateYesterday
	case strings.Contains(lower, "сьогодні") || strings.Contains(lower, "today"):
		out.DateRange.Type = models.AskDateToday
	case strings.Contains(lower, "цього тижня") || strings.Contains(lower, "this week"):
		out.DateRange.Type = models.AskDateCurrentWeek
	case strings.Contains(lower, "last 7 days") || strings.Contains(lower, "останні 7 днів"):
		out.DateRange.Type = models.AskDateLast7Days
	case strings.Contains(lower, "цього місяця") || strings.Contains(lower, "this month"):
		out.DateRange.Type = models.AskDateCurrentMonth
	case strings.Contains(lower, "останній місяць") || strings.Contains(lower, "last month"):
		out.DateRange.Type = models.AskDateLast30Days
	}
	if strings.Contains(lower, "залишилося зробити") || strings.Contains(lower, "відкриті задач") || strings.Contains(lower, "still open") {
		out.IntentType = models.AskIntentOpenActions
		out.ActionStatuses = []string{"open"}
	}
	return out
}
func ticketRegexp() interface{ FindAllString(string, int) []string } { return ticketRe{} }

type ticketRe struct{}

func (ticketRe) FindAllString(s string, n int) []string {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool {
		return !(r == '-' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z')
	}) {
		if v, ok := models.NormalizeTicketKey(f); ok {
			out = append(out, v)
		}
	}
	return out
}

func mergeDeterministicIntent(in, det models.AskIntent) models.AskIntent {
	if det.IntentType != "" {
		in.IntentType = det.IntentType
	}
	if det.DateRange.Type != "" {
		in.DateRange.Type = det.DateRange.Type
	}
	in.Entities = append(in.Entities, det.Entities...)
	if len(det.ActionStatuses) > 0 {
		in.ActionStatuses = det.ActionStatuses
	}
	return in
}

func normalizeAskIntent(in models.AskIntent, question string, now time.Time, loc *time.Location) (models.AskIntent, error) {
	if in.IntentType == "" {
		in.IntentType = models.AskIntentGeneralContext
	}
	if !validIntent(in.IntentType) {
		return in, fmt.Errorf("unsupported intent %s", in.IntentType)
	}
	if strings.TrimSpace(in.TextQuery) == "" {
		in.TextQuery = question
	}
	if utf8.RuneCountInString(in.TextQuery) > 300 {
		in.TextQuery = string([]rune(in.TextQuery)[:300])
	}
	if in.Limit <= 0 {
		in.Limit = 20
	}
	if in.Limit > 30 {
		in.Limit = 30
	}
	in.Kinds = filterKinds(in.Kinds)
	in.ActionStatuses = filterSet(in.ActionStatuses, map[string]bool{"open": true, "done": true})
	in.PeopleNoteTypes = filterSet(in.PeopleNoteTypes, map[string]bool{"positive_signal": true, "concern": true, "growth_topic": true, "context": true, "follow_up_needed": true, "commitment": true, "decision": true, "risk": true, "blocker": true, "review_evidence": true, "feedback": true})
	in.DecisionStatuses = filterSet(in.DecisionStatuses, map[string]bool{"active": true, "superseded": true, "reversed": true})
	if len(in.People) > 5 {
		in.People = in.People[:5]
	}
	var ents []models.AskEntity
	seen := map[string]bool{}
	for _, e := range in.Entities {
		t := strings.ToLower(strings.TrimSpace(e.Type))
		v := strings.TrimSpace(e.Value)
		if !models.IsAllowedEntityType(t) || v == "" {
			continue
		}
		if t == models.EntityTypeTicket {
			var ok bool
			v, ok = models.NormalizeTicketKey(v)
			if !ok {
				continue
			}
		}
		key := t + ":" + strings.ToLower(v)
		if !seen[key] {
			seen[key] = true
			ents = append(ents, models.AskEntity{Type: t, Value: v})
		}
		if len(ents) >= 10 {
			break
		}
	}
	in.Entities = ents
	dr, err := resolveAskDateRange(in.DateRange, now, loc)
	if err != nil {
		return in, err
	}
	in.DateRange = dr
	return in, nil
}
func validIntent(v string) bool {
	for _, x := range []string{models.AskIntentGeneralContext, models.AskIntentActivity, models.AskIntentCommitments, models.AskIntentOpenActions, models.AskIntentOpenQuestions, models.AskIntentPersonContext, models.AskIntentEntityHistory, models.AskIntentDecisions, models.AskIntentLatestMention, models.AskIntentRepeatedTopics} {
		if v == x {
			return true
		}
	}
	return false
}
func filterKinds(in []models.RetrievalKind) []models.RetrievalKind {
	valid := map[models.RetrievalKind]bool{models.RetrievalKindNote: true, models.RetrievalKindAction: true, models.RetrievalKindPeopleNote: true, models.RetrievalKindDecision: true, models.RetrievalKindEntityMention: true}
	seen := map[models.RetrievalKind]bool{}
	var out []models.RetrievalKind
	for _, k := range in {
		if valid[k] && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}
func filterSet(in []string, valid map[string]bool) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.ToLower(strings.TrimSpace(v))
		if valid[v] && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func resolveAskDateRange(dr models.AskDateRange, now time.Time, loc *time.Location) (models.AskDateRange, error) {
	if loc == nil {
		loc = time.UTC
	}
	t := now.In(loc)
	midnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	typ := dr.Type
	if typ == "" {
		typ = models.AskDateUnspecified
	}
	out := models.AskDateRange{Type: typ}
	switch typ {
	case models.AskDateToday:
		out.From = &midnight
		to := midnight.AddDate(0, 0, 1)
		out.To = &to
	case models.AskDateYesterday:
		to := midnight
		from := midnight.AddDate(0, 0, -1)
		out.From = &from
		out.To = &to
	case models.AskDateCurrentWeek:
		wd := (int(t.Weekday()) + 6) % 7
		from := midnight.AddDate(0, 0, -wd)
		to := t
		out.From = &from
		out.To = &to
	case models.AskDatePreviousWeek:
		wd := (int(t.Weekday()) + 6) % 7
		to := midnight.AddDate(0, 0, -wd)
		from := to.AddDate(0, 0, -7)
		out.From = &from
		out.To = &to
	case models.AskDateLast7Days:
		from := t.AddDate(0, 0, -7)
		out.From = &from
		out.To = &t
	case models.AskDateCurrentMonth:
		from := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc)
		out.From = &from
		out.To = &t
	case models.AskDateLast30Days:
		from := t.AddDate(0, 0, -30)
		out.From = &from
		out.To = &t
	case models.AskDateAllTime, models.AskDateUnspecified:
		return out, nil
	case models.AskDateExplicit:
		out.From = dr.From
		out.To = dr.To
	default:
		out.Type = models.AskDateUnspecified
		return out, nil
	}
	if out.From != nil && out.To != nil && !out.From.Before(*out.To) {
		return out, fmt.Errorf("invalid date range")
	}
	return out, nil
}

func BuildRetrievalPlan(userID int64, intent models.AskIntent) []models.RetrievalQuery {
	lim := intent.Limit
	if lim <= 0 {
		lim = 20
	}
	base := func(k ...models.RetrievalKind) models.RetrievalQuery {
		return models.RetrievalQuery{UserID: userID, Text: intent.TextQuery, From: intent.DateRange.From, To: intent.DateRange.To, Kinds: k, Limit: lim}
	}
	var qs []models.RetrievalQuery
	for _, e := range intent.Entities {
		if e.Type == models.EntityTypeTicket {
			qs = append(qs, models.RetrievalQuery{UserID: userID, From: intent.DateRange.From, To: intent.DateRange.To, Kinds: []models.RetrievalKind{models.RetrievalKindEntityMention}, EntityType: e.Type, EntityValue: e.Value, Limit: lim}, models.RetrievalQuery{UserID: userID, Text: e.Value, From: intent.DateRange.From, To: intent.DateRange.To, Kinds: []models.RetrievalKind{models.RetrievalKindNote}, Limit: lim})
		}
	}
	switch intent.IntentType {
	case models.AskIntentActivity:
		qs = append(qs, base(models.RetrievalKindNote), base(models.RetrievalKindAction), base(models.RetrievalKindDecision), base(models.RetrievalKindPeopleNote))
	case models.AskIntentCommitments:
		for _, p := range intent.People {
			q := base(models.RetrievalKindPeopleNote)
			q.PersonName = p
			q.PeopleNoteTypes = []string{"commitment", "follow_up_needed"}
			qs = append(qs, q)
			a := base(models.RetrievalKindAction)
			a.PersonName = p
			qs = append(qs, a)
		}
	case models.AskIntentOpenActions:
		q := base(models.RetrievalKindAction)
		q.ActionStatuses = []string{"open"}
		qs = append(qs, q)
	case models.AskIntentDecisions:
		q := base(models.RetrievalKindDecision)
		q.DecisionStatuses = intent.DecisionStatuses
		qs = append(qs, q, base(models.RetrievalKindNote))
	case models.AskIntentPersonContext:
		for _, p := range intent.People {
			q := base(models.RetrievalKindPeopleNote)
			q.PersonName = p
			qs = append(qs, q)
		}
	default:
		if len(qs) == 0 {
			qs = append(qs, base(models.RetrievalKindNote, models.RetrievalKindAction, models.RetrievalKindPeopleNote, models.RetrievalKindDecision))
		}
	}
	if len(qs) == 0 {
		qs = append(qs, base(models.RetrievalKindNote))
	}
	if len(qs) > 8 {
		qs = qs[:8]
	}
	return qs
}

func selectAskCandidates(items []models.RetrievalItem) []models.AskCandidate {
	seen := map[string]bool{}
	sort.SliceStable(items, func(i, j int) bool {
		pi, pj := askPriority(items[i]), askPriority(items[j])
		if pi != pj {
			return pi < pj
		}
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	var out []models.AskCandidate
	total := 0
	for _, it := range items {
		if it.SourceNoteID <= 0 {
			continue
		}
		key := fmt.Sprintf("%s:%d", it.Kind, it.RecordID)
		if seen[key] {
			continue
		}
		seen[key] = true
		text := truncateRunes(strings.TrimSpace(firstNonBlank(it.Text, it.Context, it.Title)), MaxAskSnippetRunes)
		if total+utf8.RuneCountInString(text) > MaxAskTotalTextRunes {
			continue
		}
		total += utf8.RuneCountInString(text)
		out = append(out, models.AskCandidate{Kind: it.Kind, SourceNoteID: it.SourceNoteID, Date: it.CreatedAt.Format("2006-01-02"), Title: truncateRunes(it.Title, 160), Text: text, PersonName: it.PersonName, EntityType: it.EntityType, EntityValue: it.EntityValue, Status: it.Status})
		if len(out) >= MaxAskCandidates {
			break
		}
	}
	return out
}
func askPriority(it models.RetrievalItem) int {
	switch it.Kind {
	case models.RetrievalKindEntityMention:
		return 1
	case models.RetrievalKindDecision:
		return 2
	case models.RetrievalKindAction:
		return 3
	case models.RetrievalKindPeopleNote:
		return 4
	default:
		return 5
	}
}
func validateAskAnswer(a models.AskAnswer, c []models.AskCandidate) models.AskAnswer {
	valid := map[int64]string{}
	for _, x := range c {
		valid[x.SourceNoteID] = x.Date
	}
	for i := range a.Items {
		var ids []int64
		var dates []string
		seen := map[int64]bool{}
		for _, id := range a.Items[i].SourceNoteIDs {
			if d, ok := valid[id]; ok && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
				dates = append(dates, d)
			}
		}
		a.Items[i].SourceNoteIDs = ids
		a.Items[i].SourceDates = dates
		if a.Items[i].Confidence != "confirmed" && a.Items[i].Confidence != "inferred" && a.Items[i].Confidence != "uncertain" {
			a.Items[i].Confidence = "uncertain"
		}
	}
	if strings.TrimSpace(a.Answer) == "" {
		a.Answer = "I found related notes, but there is not enough evidence to answer fully."
		a.InsufficientData = true
	}
	return a
}
func formatAskAnswer(a models.AskAnswer, c []models.AskCandidate, lang models.ResponseLanguage) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(a.Answer))
	for _, it := range a.Items {
		if strings.TrimSpace(it.Text) != "" {
			b.WriteString("\n- " + strings.TrimSpace(it.Text))
			if len(it.SourceNoteIDs) > 0 {
				b.WriteString(" " + formatRefs(it.SourceNoteIDs, it.SourceDates))
			}
		}
	}
	if len(a.Caveats) > 0 {
		b.WriteString("\n\n")
		for _, cv := range a.Caveats {
			if strings.TrimSpace(cv) != "" {
				b.WriteString("⚠️ " + strings.TrimSpace(cv) + "\n")
			}
		}
	}
	refs := sourceRefs(a, c)
	if len(refs) > 0 {
		b.WriteString("\nДжерела:")
		for _, r := range refs {
			b.WriteString("\n- note #" + fmt.Sprint(r.id) + " — " + r.date)
		}
	}
	return strings.TrimSpace(b.String())
}

type ref struct {
	id   int64
	date string
}

func sourceRefs(a models.AskAnswer, c []models.AskCandidate) []ref {
	m := map[int64]string{}
	for _, it := range a.Items {
		for i, id := range it.SourceNoteIDs {
			d := ""
			if i < len(it.SourceDates) {
				d = it.SourceDates[i]
			}
			m[id] = d
		}
	}
	if len(m) == 0 {
		for _, x := range c {
			m[x.SourceNoteID] = x.Date
			if len(m) >= 5 {
				break
			}
		}
	}
	var out []ref
	for id, d := range m {
		out = append(out, ref{id, d})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}
func formatRefs(ids []int64, dates []string) string {
	parts := []string{}
	for i, id := range ids {
		d := ""
		if i < len(dates) {
			d = ", " + dates[i]
		}
		parts = append(parts, fmt.Sprintf("[note #%d%s]", id, d))
	}
	return strings.Join(parts, " ")
}
func citedCount(a models.AskAnswer) int {
	m := map[int64]bool{}
	for _, it := range a.Items {
		for _, id := range it.SourceNoteIDs {
			m[id] = true
		}
	}
	return len(m)
}
func askCandidateCounts(c []models.AskCandidate) map[string]int {
	m := map[string]int{}
	for _, x := range c {
		m[string(x.Kind)]++
	}
	return m
}
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
func firstNonBlank(v ...string) string {
	for _, x := range v {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}
func hashPrefix(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:])[:12] }
