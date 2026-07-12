package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/spozhydaiev/lead-log/internal/adapters/llm"
	"github.com/spozhydaiev/lead-log/internal/adapters/store"
	"github.com/spozhydaiev/lead-log/internal/logging"

	"github.com/spozhydaiev/lead-log/internal/models"
	"github.com/spozhydaiev/lead-log/pkg/utils"
)

const PromptVersion = "v1"

type Service struct {
	store         *store.Store
	llm           llm.ClientLLM
	dailyLocation *time.Location
	logger        *slog.Logger
	language      models.ResponseLanguage
}

func New(store *store.Store, llm llm.ClientLLM, opts ...Option) *Service {
	s := &Service{store: store, llm: llm, dailyLocation: time.Local, logger: slog.Default(), language: models.LanguageEnglish}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

type Option func(*Service)

func WithLogger(logger *slog.Logger) Option {
	return func(s *Service) {
		if logger != nil {
			s.logger = logger
		}
	}
}

func WithResponseLanguage(language models.ResponseLanguage) Option {
	return func(s *Service) {
		if language != "" {
			s.language = language
		}
	}
}

func WithDailyLocation(loc *time.Location) Option {
	return func(s *Service) {
		if loc != nil {
			s.dailyLocation = loc
		}
	}
}

func (s *Service) EnsureUser(ctx context.Context, telegramUserID int64, username string) (int64, error) {
	return s.store.UpsertUser(ctx, telegramUserID, username)
}

func (s *Service) CaptureNote(ctx context.Context, userID int64, raw string) (string, error) {
	noteID, err := s.store.SaveRawNote(ctx, userID, raw)
	if err != nil {
		return "", fmt.Errorf("capture.save_raw_note: %w", err)
	}
	s.logger.Info("raw note saved", "operation", "capture.save_raw_note", "user_id", userID, "note_id", noteID, "note_length", len(raw))
	return s.language.CommonMessages().SavedRaw, nil
}

func (s *Service) AddNote(ctx context.Context, userID int64, raw string) (string, error) {
	s.logger.Info("immediate processing started", "operation", "now.process", "user_id", userID, "note_length", len(raw))
	s.logger.Info("LLM call started", "operation", "parse_note", "user_id", userID)
	parsed, err := s.llm.ParseManagerNote(ctx, raw)
	if err != nil {
		s.logger.Error("command failed", "operation", "parse_note", "user_id", userID, "error", err)
		return "", fmt.Errorf("now.llm_request: %w", err)
	}
	s.logger.Info("LLM call completed", "operation", "parse_note", "user_id", userID, "actions", len(parsed.Actions), "people_notes", len(parsed.PeopleNotes), "people_mentioned", countParsedPeople(parsed))
	noteID, err := s.store.SaveParsedNote(ctx, userID, raw, parsed)
	if err != nil {
		s.logger.Error("persistence failed", "operation", "now.persistence", "user_id", userID, "error", err)
		return "", fmt.Errorf("now.persistence: %w", err)
	}
	s.logger.Info("persistence completed", "operation", "now.persistence", "user_id", userID, "note_id", noteID)
	return formatParsedNote(noteID, parsed, s.language), nil
}

func (s *Service) OpenActions(ctx context.Context, userID int64) (string, error) {
	actions, err := s.store.ListOpenActions(ctx, userID, 30)
	if err != nil {
		return "", err
	}
	if len(actions) == 0 {
		return s.language.CommonMessages().NoOpenActions, nil
	}
	var b strings.Builder
	b.WriteString(s.language.CommonMessages().OpenActionsHeader + "\n")
	for _, a := range actions {
		person := ""
		if a.PersonName != nil {
			person = " — " + *a.PersonName
		}
		b.WriteString(fmt.Sprintf("%d. %s%s", a.ID, a.Title, person))
		if a.OutputType != "" {
			b.WriteString(" [" + a.OutputType + "]")
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

func (s *Service) Done(ctx context.Context, userID int64, arg string) (string, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(arg), 10, 64)
	if err != nil {
		return s.language.CommonMessages().DoneUsage, nil
	}
	if err := s.store.MarkActionDone(ctx, userID, id); err != nil {
		return "", err
	}
	return fmt.Sprintf(s.language.CommonMessages().DoneMarked, id), nil
}

func countParsedPeople(p models.ParsedNote) int {
	seen := map[string]bool{}
	for _, pn := range p.PeopleNotes {
		name := strings.TrimSpace(pn.PersonName)
		if name != "" {
			seen[name] = true
		}
	}
	for _, a := range p.Actions {
		name := strings.TrimSpace(a.LinkedPersonName)
		if name != "" {
			seen[name] = true
		}
	}
	return len(seen)
}

func countSourceNotes(source string) int {
	return strings.Count(source, "Note #")
}
func (s *Service) Daily(ctx context.Context, userID int64, refresh bool) (string, error) {
	return s.DailyAt(ctx, userID, time.Now(), refresh)
}

func (s *Service) DailyAt(ctx context.Context, userID int64, now time.Time, refresh bool) (string, error) {
	loc := s.dailyLocation
	if loc == nil {
		loc = time.Local
	}
	localNow := now.In(loc)
	startOfDay := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc)
	endOfDay := startOfDay.AddDate(0, 0, 1)
	log := s.logger.With("operation", "daily", "user_id", userID, "period_start", startOfDay.Format(time.RFC3339), "period_end", endOfDay.Format(time.RFC3339), "timezone", loc.String())
	log.Info("daily command started", "resolved_timezone", loc.String())

	source, err := s.store.RecentDailySource(ctx, userID, startOfDay, endOfDay)
	if err != nil {
		log.Error("daily failed", "failure_stage", "source loading", "operation", "daily.load_source", "error", err)
		return "", fmt.Errorf("daily.load_source: %w", err)
	}
	if strings.TrimSpace(source) == "" {
		log.Info("daily source loaded", "note_count", 0)
		return s.language.CommonMessages().NoNotesToday, nil
	}

	scopeKey := scopedCacheKey(startOfDay.Format("2006-01-02"), s.language)
	sourceHash := utils.HashStrings(source)
	log = log.With("scope_key", scopeKey, "source_hash_prefix", logging.HashPrefix(sourceHash), "note_count", countSourceNotes(source))
	log.Info("daily source loaded")

	if !refresh {
		cached, err := s.store.GetCachedAgentResponse(ctx, userID, "daily", scopeKey, sourceHash, PromptVersion)
		if err != nil {
			log.Error("daily failed", "failure_stage", "cache lookup", "operation", "daily.cache_lookup", "error", err)
			return "", fmt.Errorf("daily.cache_lookup: %w", err)
		}
		if cached != nil {
			log.Info("cache hit", "cache_hit", true)
			return cached.ResponseText + "\n\n" + s.language.CommonMessages().DailyCachedNotice, nil
		}
	}
	log.Info("cache miss", "cache_hit", false)

	log.Info("LLM call started", "operation", "daily.llm_request")
	digest, err := s.llm.ProcessDaily(ctx, source)
	if err != nil {
		log.Error("daily failed", "failure_stage", "LLM request", "operation", "daily.llm_request", "error", err)
		return "", fmt.Errorf("daily.llm_request: %w", err)
	}
	log.Info("LLM call completed", "operation", "daily.llm_request")
	responseText := FormatDailyDigestForLanguage(digest, s.language)
	if strings.TrimSpace(responseText) == "" {
		err := fmt.Errorf("empty formatted daily digest")
		log.Error("daily failed", "failure_stage", "formatting", "operation", "daily.formatting", "error", err)
		return "", err
	}
	responseJSON, err := json.Marshal(digest)
	if err != nil {
		log.Error("daily failed", "failure_stage", "JSON parsing", "operation", "daily.parse_json", "error", err)
		return "", fmt.Errorf("daily.parse_json: %w", err)
	}

	if err := s.store.SaveAgentResponse(ctx, models.AgentResponse{UserID: userID, Kind: "daily", ScopeKey: scopeKey, PeriodStart: &startOfDay, PeriodEnd: &endOfDay, SourceHash: sourceHash, PromptVersion: PromptVersion, Model: s.llm.Model(), ResponseText: responseText, ResponseJSON: string(responseJSON)}); err != nil {
		log.Error("daily failed", "failure_stage", "cache save", "operation", "daily.cache_save", "error", err)
		return "", fmt.Errorf("daily.cache_save: %w", err)
	}
	log.Info("digest cached", "operation", "daily.cache_save")
	log.Info("formatted response sent", "operation", "daily.formatting", "response_length", len(responseText))
	return responseText, nil
}

func (s *Service) Weekly(ctx context.Context, userID int64, refresh bool) (string, error) {
	now := time.Now()
	start := now.AddDate(0, 0, -7)
	log := s.logger.With("operation", "weekly", "user_id", userID, "period_start", start.Format(time.RFC3339), "period_end", now.Format(time.RFC3339))
	log.Info("weekly command started")

	source, err := s.store.RecentWeeklySource(ctx, userID, start)
	if err != nil {
		log.Error("weekly failed", "failure_stage", "source loading", "operation", "weekly.load_source", "error", err)
		return "", fmt.Errorf("weekly.load_source: %w", err)
	}
	if strings.TrimSpace(source) == "" {
		log.Info("weekly source loaded", "note_count", 0)
		return s.language.CommonMessages().NoNotesLast7Days, nil
	}

	year, week := now.ISOWeek()
	scopeKey := scopedCacheKey(fmt.Sprintf("%d-W%02d", year, week), s.language)
	sourceHash := utils.HashStrings(source)
	log = log.With("scope_key", scopeKey, "source_hash_prefix", logging.HashPrefix(sourceHash), "note_count", countSourceNotes(source))
	log.Info("weekly source loaded")

	if !refresh {
		cached, err := s.store.GetCachedAgentResponse(ctx, userID, "weekly", scopeKey, sourceHash, PromptVersion)
		if err != nil {
			log.Error("weekly failed", "failure_stage", "cache lookup", "operation", "weekly.cache_lookup", "error", err)
			return "", fmt.Errorf("weekly.cache_lookup: %w", err)
		}
		if cached != nil {
			log.Info("cache hit", "cache_hit", true)
			return cached.ResponseText + "\n\n" + s.language.CommonMessages().WeeklyCachedNotice, nil
		}
	}
	log.Info("cache miss", "cache_hit", false)
	log.Info("LLM call started", "operation", "weekly.llm_request")
	response, err := s.llm.SummarizeWeekly(ctx, source)
	if err != nil {
		log.Error("weekly failed", "failure_stage", "LLM request", "operation", "weekly.llm_request", "error", err)
		return "", fmt.Errorf("weekly.llm_request: %w", err)
	}
	log.Info("LLM call completed", "operation", "weekly.llm_request")

	if err := s.store.SaveAgentResponse(ctx, models.AgentResponse{UserID: userID, Kind: "weekly", ScopeKey: scopeKey, PeriodStart: &start, PeriodEnd: &now, SourceHash: sourceHash, PromptVersion: PromptVersion, Model: s.llm.Model(), ResponseText: response}); err != nil {
		log.Error("weekly failed", "failure_stage", "cache save", "operation", "weekly.cache_save", "error", err)
		return "", fmt.Errorf("weekly.cache_save: %w", err)
	}
	log.Info("cache save completed", "operation", "weekly.cache_save")
	return response, nil
}

func formatParsedNote(noteID int64, p models.ParsedNote, language models.ResponseLanguage) string {
	labels := language.NowLabels()
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s #%d\n\n", labels.SavedNote, noteID))
	if p.Summary != "" {
		b.WriteString(labels.Summary + ":\n" + p.Summary + "\n\n")
	}
	if len(p.Actions) > 0 {
		b.WriteString(labels.Actions + ":\n")
		for i, a := range p.Actions {
			b.WriteString(fmt.Sprintf("%d. %s", i+1, a.Title))
			if a.LinkedPersonName != "" {
				b.WriteString(" — " + a.LinkedPersonName)
			}
			if a.OutputType != "" {
				b.WriteString(" [" + a.OutputType + "]")
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if len(p.PeopleNotes) > 0 {
		b.WriteString(labels.PeopleNotes + ":\n")
		for _, pn := range p.PeopleNotes {
			b.WriteString(fmt.Sprintf("- %s: %s / %s — %s\n", pn.PersonName, pn.Type, pn.Theme, pn.Text))
		}
		b.WriteString("\n")
	}
	if len(p.SuggestedQuestions) > 0 {
		b.WriteString(labels.Questions + ":\n")
		for _, q := range p.SuggestedQuestions {
			b.WriteString("- " + q + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func scopedCacheKey(base string, language models.ResponseLanguage) string {
	return base + ":" + string(language)
}

func (s *Service) ResponseMessages() models.CommonMessages {
	return s.language.CommonMessages()
}
