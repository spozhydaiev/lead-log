package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spozhydaiev/lead-log/internal/adapters/llm"
	"github.com/spozhydaiev/lead-log/internal/adapters/store"
	"github.com/spozhydaiev/lead-log/internal/domain/periods"
	"github.com/spozhydaiev/lead-log/internal/logging"
	"github.com/spozhydaiev/lead-log/internal/models"
	"github.com/spozhydaiev/lead-log/pkg/utils"
)

var ErrNoSummarySourceNotes = errors.New("no summary source notes")
var summaryLocks sync.Map

type SummaryFilter struct {
	Type, Status string
	From, To     *time.Time
}
type SummaryCursor struct {
	PeriodEnd time.Time
	Kind      string
	ID        int64
}
type SummaryListView struct{ Items []SummaryView }
type SummaryGenerateInput struct {
	Type       string
	AnchorDate time.Time
	Force      bool
}
type SummaryGenerateResult struct {
	Generated bool          `json:"generated"`
	CacheHit  bool          `json:"cache_hit"`
	Reason    string        `json:"reason,omitempty"`
	Period    SummaryPeriod `json:"period"`
	Summary   *SummaryView  `json:"summary"`
}
type SummaryPeriod struct {
	From string `json:"from"`
	To   string `json:"to"`
}
type SummaryView struct {
	ID            string          `json:"id"`
	Type          string          `json:"type"`
	Status        string          `json:"status"`
	Title         string          `json:"title,omitempty"`
	Preview       string          `json:"preview,omitempty"`
	Period        SummaryPeriod   `json:"period"`
	GeneratedAt   time.Time       `json:"generated_at"`
	SourceChanged *bool           `json:"source_changed"`
	Content       any             `json:"content,omitempty"`
	Sources       []SummarySource `json:"sources,omitempty"`
}
type SummarySource struct {
	Type       string    `json:"type"`
	ID         string    `json:"id"`
	OccurredAt time.Time `json:"occurred_at"`
	Label      string    `json:"label"`
	Excerpt    string    `json:"excerpt"`
}

func (s *Service) ListSummaries(ctx context.Context, userID int64, f SummaryFilter, limit int, c *SummaryCursor) (SummaryListView, error) {
	rows, err := s.store.ListSummaries(ctx, userID, store.SummaryListFilter{Type: f.Type, Status: f.Status, From: f.From, To: f.To}, limit, storeSummaryCursor(c))
	if err != nil {
		return SummaryListView{}, err
	}
	out := SummaryListView{Items: []SummaryView{}}
	for _, r := range rows {
		out.Items = append(out.Items, s.mapSummary(ctx, userID, r, false))
	}
	return out, nil
}
func (s *Service) GetSummary(ctx context.Context, userID, id int64) (SummaryView, error) {
	r, err := s.store.GetSummary(ctx, userID, id)
	if err != nil {
		return SummaryView{}, err
	}
	return s.mapSummary(ctx, userID, r, true), nil
}
func (s *Service) GenerateSummary(ctx context.Context, userID int64, in SummaryGenerateInput) (SummaryGenerateResult, error) {
	if s.summaryGenerationTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.summaryGenerationTimeout)
		defer cancel()
	}
	started := time.Now()
	start, end, scope, err := s.summaryPeriod(in.Type, in.AnchorDate)
	if err != nil {
		return SummaryGenerateResult{}, err
	}
	period := SummaryPeriod{From: start.Format("2006-01-02"), To: end.Add(-time.Nanosecond).Format("2006-01-02")}
	lockKey := fmt.Sprintf("%d:%s:%s:%s", userID, in.Type, scope, s.language)
	muIface, _ := summaryLocks.LoadOrStore(lockKey, &sync.Mutex{})
	mu := muIface.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	defer summaryLocks.Delete(lockKey)
	retrievalStarted := time.Now()
	source, err := s.summarySource(ctx, userID, in.Type, start, end)
	retrievalMS := time.Since(retrievalStarted).Milliseconds()
	if err != nil {
		return SummaryGenerateResult{}, err
	}
	if strings.TrimSpace(source) == "" {
		return SummaryGenerateResult{Generated: false, CacheHit: false, Reason: "no_source_notes", Period: period}, nil
	}
	sourceHash := utils.HashStrings(source)
	scopeKey := scopedCacheKey(scope, s.language)
	if !in.Force {
		if cached, err := s.store.GetCachedAgentResponse(ctx, userID, in.Type, scopeKey, sourceHash, PromptVersion); err != nil {
			return SummaryGenerateResult{}, err
		} else if cached != nil {
			v := s.mapSummary(ctx, userID, *cached, true)
			return SummaryGenerateResult{Generated: false, CacheHit: true, Period: period, Summary: &v}, nil
		}
	}
	if in.Type == "daily" {
		d, err := s.llm.ProcessDaily(ctx, source)
		if err != nil {
			return SummaryGenerateResult{}, err
		}
		text := FormatDailyDigestForLanguage(d, s.language)
		b, _ := json.Marshal(d)
		if err := s.store.SaveAgentResponse(ctx, models.AgentResponse{UserID: userID, Kind: "daily", ScopeKey: scopeKey, PeriodStart: &start, PeriodEnd: &end, SourceHash: sourceHash, PromptVersion: PromptVersion, Model: s.llm.Model(), ResponseText: text, ResponseJSON: string(b)}); err != nil {
			return SummaryGenerateResult{}, err
		}
	} else {
		llmStarted := time.Now()
		raw, err := s.llm.SummarizeWeekly(ctx, source)
		llmMS := time.Since(llmStarted).Milliseconds()
		if err != nil {
			return SummaryGenerateResult{}, err
		}
		parseStarted := time.Now()
		digest, err := llm.ParseWeeklyDigestJSONWithLogger(raw, s.logger)
		parseMS := time.Since(parseStarted).Milliseconds()
		if err != nil {
			return SummaryGenerateResult{}, logging.NewCodedError("malformed_provider_output", "malformed weekly provider output", err)
		}
		b, _ := json.Marshal(digest)
		text := FormatWeeklyDigest(digest)
		persistStarted := time.Now()
		if err := s.store.SaveAgentResponse(ctx, models.AgentResponse{UserID: userID, Kind: "weekly", ScopeKey: scopeKey, PeriodStart: &start, PeriodEnd: &end, SourceHash: sourceHash, PromptVersion: PromptVersion, Model: s.llm.Model(), ResponseText: text, ResponseJSON: string(b)}); err != nil {
			return SummaryGenerateResult{}, err
		}
		s.logger.Info("summary generation completed", "operation", "weekly.generate", "source_count", approximateSourceCount(source), "source_byte_length", len(source), "retrieval_ms", retrievalMS, "llm_ms", llmMS, "parse_ms", parseMS, "persist_ms", time.Since(persistStarted).Milliseconds(), "duration_ms", time.Since(started).Milliseconds(), "result", "success")
	}
	cached, err := s.store.GetCachedAgentResponse(ctx, userID, in.Type, scopeKey, sourceHash, PromptVersion)
	if err != nil {
		return SummaryGenerateResult{}, err
	}
	if cached == nil {
		return SummaryGenerateResult{}, fmt.Errorf("generated summary missing")
	}
	v := s.mapSummary(ctx, userID, *cached, true)
	return SummaryGenerateResult{Generated: true, CacheHit: false, Period: period, Summary: &v}, nil
}
func storeSummaryCursor(c *SummaryCursor) *store.SummaryCursor {
	if c == nil {
		return nil
	}
	return &store.SummaryCursor{PeriodEnd: c.PeriodEnd, Kind: c.Kind, ID: c.ID}
}
func (s *Service) summaryPeriod(kind string, anchor time.Time) (time.Time, time.Time, string, error) {
	loc := s.dailyLocation
	if loc == nil {
		loc = time.Local
	}
	d := anchor.In(loc)
	if kind == "daily" {
		st := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
		return st, st.AddDate(0, 0, 1), st.Format("2006-01-02"), nil
	}
	if kind == "weekly" {
		w := periods.ResolveContainingWeek(d, loc)
		return w.Start, w.ExclusiveEnd, w.ScopeKey, nil
	}
	return time.Time{}, time.Time{}, "", fmt.Errorf("invalid summary type")
}
func (s *Service) summarySource(ctx context.Context, userID int64, kind string, start, end time.Time) (string, error) {
	if kind == "daily" {
		return s.store.RecentDailySource(ctx, userID, start, end)
	}
	if kind == "weekly" {
		return s.store.RecentSummarySource(ctx, userID, start, end)
	}
	return "", fmt.Errorf("invalid summary type")
}
func (s *Service) mapSummary(ctx context.Context, userID int64, r models.AgentResponse, detail bool) SummaryView {
	from, to := "", ""
	if r.PeriodStart != nil {
		from = r.PeriodStart.Format("2006-01-02")
	}
	if r.PeriodEnd != nil {
		to = r.PeriodEnd.Add(-time.Nanosecond).Format("2006-01-02")
	}
	v := SummaryView{ID: fmt.Sprintf("summary_%d", r.ID), Type: r.Kind, Status: "ready", Period: SummaryPeriod{From: from, To: to}, GeneratedAt: r.CreatedAt, Title: summaryTitle(r.Kind, from, to), Preview: summaryPreview(r)}
	if detail {
		v.Content = summaryContent(r)
		v.Sources = s.summarySources(ctx, userID, r)
	}
	if r.PeriodStart != nil && r.PeriodEnd != nil && r.SourceHash != "" {
		if src, err := s.summarySource(ctx, userID, r.Kind, *r.PeriodStart, *r.PeriodEnd); err == nil {
			changed := utils.HashStrings(src) != r.SourceHash || r.PromptVersion != PromptVersion
			v.SourceChanged = &changed
		}
	}
	return v
}
func summaryTitle(kind, from, to string) string {
	if kind == "weekly" {
		return "Weekly summary for " + from + " to " + to
	}
	return "Daily summary for " + from
}
func summaryContent(r models.AgentResponse) any {
	if d, ok := normalizedWeeklyDigest(r); ok {
		return d
	}
	if strings.TrimSpace(r.ResponseJSON) != "" {
		var x any
		if json.Unmarshal([]byte(r.ResponseJSON), &x) == nil {
			return x
		}
	}
	return map[string]any{"text": r.ResponseText}
}

func normalizedWeeklyDigest(r models.AgentResponse) (models.WeeklyDigest, bool) {
	if r.Kind != "weekly" {
		return models.WeeklyDigest{}, false
	}
	for _, raw := range []string{r.ResponseJSON, r.ResponseText} {
		raw = strings.TrimSpace(raw)
		if !strings.HasPrefix(raw, "{") || !strings.HasSuffix(raw, "}") {
			continue
		}
		d, err := llm.ParseWeeklyDigestJSON(raw)
		if err == nil {
			return d, true
		}
	}
	return models.WeeklyDigest{}, false
}

func summaryPreview(r models.AgentResponse) string {
	if d, ok := normalizedWeeklyDigest(r); ok && strings.TrimSpace(d.Summary) != "" {
		return runePreview(strings.ReplaceAll(d.Summary, "\n", " "), 240)
	}
	return runePreview(strings.ReplaceAll(r.ResponseText, "\n", " "), 240)
}

func FormatWeeklyDigest(d models.WeeklyDigest) string {
	if strings.TrimSpace(d.Summary) != "" {
		return d.Summary
	}
	sections := [][]models.WeeklyTextItem{d.Highlights, d.Actions, d.Decisions, d.People, d.Tickets, d.Risks, d.OpenQuestions, d.RepeatedTopics}
	for _, items := range sections {
		if len(items) > 0 {
			return items[0].Text
		}
	}
	return "Weekly summary generated."
}
func (s *Service) summarySources(ctx context.Context, userID int64, r models.AgentResponse) []SummarySource {
	ids := sourceIDs(r)
	if len(ids) == 0 {
		return []SummarySource{}
	}
	notes, _ := s.store.SourceNotesByIDs(ctx, userID, ids, 20)
	out := []SummarySource{}
	for _, n := range notes {
		label := strings.TrimSpace(n.Summary)
		if label == "" {
			label = "Note"
		}
		out = append(out, SummarySource{Type: "note", ID: fmt.Sprintf("note_%d", n.ID), OccurredAt: n.CreatedAt, Label: runePreview(label, 120), Excerpt: runePreview(n.RawText, 300)})
	}
	return out
}
func sourceIDs(r models.AgentResponse) []int64 {
	if strings.TrimSpace(r.ResponseJSON) == "" {
		return nil
	}
	var walk func(any)
	seen := map[int64]bool{}
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, v := range x {
				if k == "source_note_ids" {
					if a, ok := v.([]any); ok {
						for _, e := range a {
							if f, ok := e.(float64); ok && f > 0 {
								seen[int64(f)] = true
							}
						}
					}
				}
				walk(v)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	var x any
	if json.Unmarshal([]byte(r.ResponseJSON), &x) == nil {
		walk(x)
	}
	out := []int64{}
	for id := range seen {
		out = append(out, id)
		if len(out) >= 20 {
			break
		}
	}
	return out
}
func runePreview(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > max {
		return string(r[:max])
	}
	return string(r)
}

func approximateSourceCount(source string) int {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "\n") + 1
}
