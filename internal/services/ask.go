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

	"github.com/spozhydaiev/lead-log/internal/logging"

	"github.com/spozhydaiev/lead-log/internal/domain/periods"
	"github.com/spozhydaiev/lead-log/internal/models"
)

const AskPlanningPromptVersion = "v1"
const AskAnswerPromptVersion = "v1"
const MaxAskQuestionRunes = 1500
const MaxAskCandidates = 30
const MaxAskTotalTextRunes = 6000
const MaxAskSnippetRunes = 500
const MaxAskAPISources = 12
const MaxAskAnswerRunes = 3000

var ErrInvalidAskQuestion = errors.New("invalid ask question")
var ErrAskPlanning = errors.New("ask planning failed")
var ErrAskAnswer = errors.New("ask answer failed")

type AskResponse struct {
	Answer     string      `json:"answer"`
	Sources    []AskSource `json:"sources"`
	Scope      *AskScope   `json:"scope,omitempty"`
	Confidence string      `json:"confidence"`
}

type AskSource struct {
	Type       string     `json:"type"`
	ID         string     `json:"id"`
	OccurredAt *time.Time `json:"occurred_at,omitempty"`
	Label      string     `json:"label"`
	Excerpt    string     `json:"excerpt"`
}

type AskScope struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (s *Service) Ask(ctx context.Context, userID int64, question string) (string, error) {
	started := time.Now()
	q := strings.TrimSpace(question)
	if q == "" {
		return s.language.CommonMessages().AskUsage, nil
	}
	if utf8.RuneCountInString(q) > MaxAskQuestionRunes {
		return s.language.CommonMessages().AskTooLong, nil
	}
	log := s.logger.With("operation", "ask", "operation_id", logging.OperationID(ctx), "query_length", len(q), "planning_prompt_version", AskPlanningPromptVersion, "answer_prompt_version", AskAnswerPromptVersion, "model", s.llm.Model())
	log.Info("ask started")
	planStart := time.Now()
	intent, err := s.planAskIntent(ctx, q, time.Now(), s.dailyLocation)
	if err != nil {
		log.Error("ask failed", logging.WithSafeError([]any{"failure_stage", "intent_validation"}, err)...)
		return "", fmt.Errorf("%w: %v", ErrAskPlanning, err)
	}
	log.Info("ask intent planned", "planner_duration_ms", time.Since(planStart).Milliseconds(), "intent_type", intent.IntentType, "date_range_type", intent.DateRange.Type, "requested_kinds", kindStrings(intent.Kinds))
	queries := BuildRetrievalPlan(userID, intent)
	log.Info("ask retrieval plan built", "retrieval_query_count", len(queries))
	var all []models.RetrievalItem
	for _, rq := range queries {
		items, err := s.Retrieve(ctx, rq)
		if err != nil {
			log.Error("ask failed", logging.WithSafeError([]any{"failure_stage", "retrieval"}, err)...)
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
		log.Error("ask failed", logging.WithSafeError([]any{"failure_stage", "answer_generation", "duration_ms", time.Since(answerStart).Milliseconds()}, err)...)
		return "", fmt.Errorf("%w: %v", ErrAskAnswer, err)
	}
	ans = validateAskAnswer(ans, candidates)
	log.Info("ask completed", "answer_generation_duration_ms", time.Since(answerStart).Milliseconds(), "cited_source_count", citedCount(ans), "insufficient_data", ans.InsufficientData, "total_duration_ms", time.Since(started).Milliseconds())
	return formatAskAnswer(ans, candidates, s.language), nil
}

func (s *Service) AskAPI(ctx context.Context, userID int64, question string, now time.Time, timezone string) (AskResponse, error) {
	started := time.Now()
	q := strings.TrimSpace(question)
	if utf8.RuneCountInString(q) < 2 || utf8.RuneCountInString(q) > MaxAskQuestionRunes {
		return AskResponse{}, ErrInvalidAskQuestion
	}
	loc := s.dailyLocation
	if strings.TrimSpace(timezone) != "" {
		if l, err := time.LoadLocation(timezone); err == nil {
			loc = l
		}
	}
	if now.IsZero() {
		now = time.Now()
	}
	log := s.logger.With("operation", "ask.api", "operation_id", logging.OperationID(ctx), "query_length", utf8.RuneCountInString(q), "planning_prompt_version", AskPlanningPromptVersion, "answer_prompt_version", AskAnswerPromptVersion, "model", s.llm.Model())
	log.Info("ask api started")
	intent, err := s.planAskIntent(ctx, q, now, loc)
	if err != nil {
		return AskResponse{}, err
	}
	queries := BuildRetrievalPlan(userID, intent)
	var all []models.RetrievalItem
	for _, rq := range queries {
		items, err := s.Retrieve(ctx, rq)
		if err != nil {
			return AskResponse{}, err
		}
		all = append(all, items...)
	}
	candidates := selectAskCandidates(all)
	if len(candidates) == 0 {
		return AskResponse{Answer: s.language.CommonMessages().AskNoResults, Scope: askScope(intent.DateRange, loc), Confidence: "insufficient_context"}, nil
	}
	if deterministicAskCanAnswer(q, intent) {
		return deterministicAskResponse(q, intent, candidates, s.language, loc), nil
	}
	ans, err := s.llm.GenerateAskAnswer(ctx, q, intent, candidates, string(s.language))
	if err != nil {
		log.Error("ask api failed", logging.WithSafeError([]any{"failure_stage", "answer_generation", "duration_ms", time.Since(started).Milliseconds()}, err)...)
		return AskResponse{}, fmt.Errorf("%w: %v", ErrAskAnswer, err)
	}
	ans = validateAskAnswer(ans, candidates)
	return askResponseFromAnswer(ans, candidates, intent.DateRange, s.language, loc), nil
}

func (s *Service) planAskIntent(ctx context.Context, q string, now time.Time, loc *time.Location) (models.AskIntent, error) {
	det := deterministicAskIntent(q)
	intent := det
	if det.IntentType == "" || (det.DateRange.Type == "" && len(det.Entities) == 0) {
		planned, err := s.llm.PlanAskQuery(ctx, q, now.In(loc).Format("2006-01-02"), loc.String(), string(s.language))
		if err != nil {
			return models.AskIntent{}, fmt.Errorf("%w: %v", ErrAskPlanning, err)
		}
		intent = mergeDeterministicIntent(planned, det)
	}
	return normalizeAskIntent(intent, q, now, loc)
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
		week := periods.ResolveContainingWeek(t, loc)
		from := week.Start
		to := t
		out.From = &from
		out.To = &to
	case models.AskDatePreviousWeek:
		week := periods.ResolvePreviousCompletedWeek(t, loc)
		from := week.Start
		to := week.ExclusiveEnd
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
		out = append(out, models.AskCandidate{Kind: it.Kind, RecordID: it.RecordID, SourceNoteID: it.SourceNoteID, Date: it.CreatedAt.Format("2006-01-02"), CreatedAt: it.CreatedAt, Title: truncateRunes(it.Title, 160), Text: text, PersonName: it.PersonName, EntityType: it.EntityType, EntityValue: it.EntityValue, Status: it.Status})
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

func deterministicAskCanAnswer(q string, intent models.AskIntent) bool {
	lower := strings.ToLower(q)
	return intent.IntentType == models.AskIntentLatestMention || intent.IntentType == models.AskIntentOpenActions || (intent.IntentType == models.AskIntentActivity && (strings.Contains(lower, "yesterday") || strings.Contains(lower, "today") || strings.Contains(lower, "this week")))
}

func deterministicAskResponse(q string, intent models.AskIntent, c []models.AskCandidate, lang models.ResponseLanguage, loc *time.Location) AskResponse {
	if len(c) == 0 {
		return AskResponse{Answer: lang.CommonMessages().AskNoResults, Scope: askScope(intent.DateRange, loc), Confidence: "insufficient_context"}
	}
	var b strings.Builder
	switch intent.IntentType {
	case models.AskIntentLatestMention:
		x := c[0]
		label := strings.TrimSpace(firstNonBlank(x.EntityValue, x.Title, "the item"))
		b.WriteString(fmt.Sprintf("The last matching mention I found was %s on %s.", label, x.CreatedAt.In(loc).Format("2006-01-02")))
	case models.AskIntentOpenActions:
		b.WriteString("Open actions I found:")
		for i, x := range c {
			if i >= 10 {
				break
			}
			b.WriteString("\n- " + firstNonBlank(x.Title, x.Text))
		}
	default:
		b.WriteString("I found these records for the requested period:")
		for i, x := range c {
			if i >= 10 {
				break
			}
			b.WriteString("\n- " + firstNonBlank(x.Title, x.Text))
		}
	}
	return AskResponse{Answer: truncateRunes(b.String(), MaxAskAnswerRunes), Sources: askSources(c, nil), Scope: askScope(intent.DateRange, loc), Confidence: "grounded"}
}

func askResponseFromAnswer(a models.AskAnswer, c []models.AskCandidate, dr models.AskDateRange, lang models.ResponseLanguage, loc *time.Location) AskResponse {
	answer := truncateRunes(strings.TrimSpace(a.Answer), MaxAskAnswerRunes)
	if answer == "" || a.InsufficientData {
		if answer == "" {
			answer = lang.CommonMessages().AskNoResults
		}
	}
	ids := map[int64]bool{}
	for _, it := range a.Items {
		for _, id := range it.SourceNoteIDs {
			ids[id] = true
		}
	}
	conf := "grounded"
	if a.InsufficientData {
		conf = "insufficient_context"
	}
	return AskResponse{Answer: answer, Sources: askSources(c, ids), Scope: askScope(dr, loc), Confidence: conf}
}

func askSources(c []models.AskCandidate, noteIDs map[int64]bool) []AskSource {
	out := []AskSource{}
	seen := map[string]bool{}
	for _, x := range c {
		if noteIDs != nil && !noteIDs[x.SourceNoteID] {
			continue
		}
		typ := askPublicType(x.Kind)
		idn := x.RecordID
		if typ == "note" || idn <= 0 {
			idn = x.SourceNoteID
		}
		id := fmt.Sprintf("%s_%d", typ, idn)
		if typ == "ticket" {
			id = x.EntityValue
		}
		key := typ + ":" + id
		if seen[key] {
			continue
		}
		seen[key] = true
		t := x.CreatedAt
		out = append(out, AskSource{Type: typ, ID: id, OccurredAt: &t, Label: truncateRunes(firstNonBlank(x.Title, x.EntityValue, string(x.Kind)), 120), Excerpt: truncateRunes(firstNonBlank(x.Text, x.Title), 300)})
		if len(out) >= MaxAskAPISources {
			break
		}
	}
	return out
}

func askPublicType(k models.RetrievalKind) string {
	switch k {
	case models.RetrievalKindAction:
		return "action"
	case models.RetrievalKindPeopleNote:
		return "person"
	case models.RetrievalKindDecision:
		return "decision"
	case models.RetrievalKindEntityMention:
		return "ticket"
	default:
		return "note"
	}
}
func askScope(dr models.AskDateRange, loc *time.Location) *AskScope {
	if dr.From == nil || dr.To == nil {
		return nil
	}
	return &AskScope{From: dr.From.In(loc).Format("2006-01-02"), To: dr.To.In(loc).Add(-time.Nanosecond).Format("2006-01-02")}
}
