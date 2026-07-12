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
const NoteEnrichmentPromptVersion = "v2"
const DefaultNoteEnrichmentStaleTimeout = 3 * time.Minute

type Service struct {
	store                      *store.Store
	llm                        llm.ClientLLM
	dailyLocation              *time.Location
	logger                     *slog.Logger
	language                   models.ResponseLanguage
	noteEnrichmentStaleTimeout time.Duration
}

func New(store *store.Store, llm llm.ClientLLM, opts ...Option) *Service {
	s := &Service{store: store, llm: llm, dailyLocation: time.Local, logger: slog.Default(), language: models.LanguageEnglish, noteEnrichmentStaleTimeout: DefaultNoteEnrichmentStaleTimeout}
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

func WithNoteEnrichmentStaleTimeout(timeout time.Duration) Option {
	return func(s *Service) {
		if timeout > 0 {
			s.noteEnrichmentStaleTimeout = timeout
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

func (s *Service) ClaimTelegramUpdate(ctx context.Context, meta store.TelegramUpdateMeta, staleAfter time.Duration) (store.TelegramUpdateClaim, error) {
	return s.store.ClaimTelegramUpdate(ctx, meta, staleAfter)
}

func (s *Service) MarkTelegramUpdateProcessed(ctx context.Context, meta store.TelegramUpdateMeta, startedAt time.Time) error {
	return s.store.MarkTelegramUpdateProcessed(ctx, meta, startedAt)
}

func (s *Service) MarkTelegramUpdateFailed(ctx context.Context, meta store.TelegramUpdateMeta, startedAt time.Time, cause error) error {
	return s.store.MarkTelegramUpdateFailed(ctx, meta, startedAt, cause)
}

func (s *Service) CaptureNoteForTelegramUpdate(ctx context.Context, userID int64, raw string, meta store.TelegramUpdateMeta, startedAt time.Time) (string, error) {
	noteID, err := s.store.SaveRawNoteAndMarkTelegramUpdateProcessed(ctx, userID, raw, meta, startedAt)
	if err != nil {
		return "", fmt.Errorf("capture.save_raw_note: %w", err)
	}
	s.logger.Info("raw note saved", "operation", "capture.save_raw_note", "user_id", userID, "note_id", noteID, "note_length", len(raw))
	return s.language.CommonMessages().SavedRaw, nil
}

func (s *Service) AddNoteForTelegramUpdate(ctx context.Context, userID int64, raw string, meta store.TelegramUpdateMeta, startedAt time.Time) (string, error) {
	claim, err := s.store.CreateAndClaimNoteForEnrichmentAndMarkTelegramUpdateProcessed(ctx, userID, raw, meta, startedAt)
	if err != nil {
		return "", fmt.Errorf("now.create_raw_note: %w", err)
	}
	s.logger.Info("note created and claimed", "operation", "note.created_claimed", "user_id", userID, "note_id", claim.ID, "status", store.NoteProcessingStatusProcessing, "note_length", len(raw))
	result, err := s.EnrichClaimedNote(ctx, claim)
	if err != nil {
		return "", err
	}
	return formatParsedNote(result.NoteID, result.Parsed, s.language), nil
}

func (s *Service) DoneForTelegramUpdate(ctx context.Context, userID int64, arg string, meta store.TelegramUpdateMeta, startedAt time.Time) (string, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(arg), 10, 64)
	if err != nil {
		return s.language.CommonMessages().DoneUsage, s.store.MarkTelegramUpdateProcessed(ctx, meta, startedAt)
	}
	if err := s.store.MarkActionDoneAndMarkTelegramUpdateProcessed(ctx, userID, id, meta, startedAt); err != nil {
		return "", err
	}
	return fmt.Sprintf(s.language.CommonMessages().DoneMarked, id), nil
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
	claim, err := s.store.CreateAndClaimNoteForEnrichment(ctx, userID, raw)
	if err != nil {
		return "", fmt.Errorf("now.create_raw_note: %w", err)
	}
	s.logger.Info("note created and claimed", "operation", "note.created_claimed", "user_id", userID, "note_id", claim.ID, "status", store.NoteProcessingStatusProcessing, "note_length", len(raw))
	result, err := s.EnrichClaimedNote(ctx, claim)
	if err != nil {
		return "", err
	}
	return formatParsedNote(result.NoteID, result.Parsed, s.language), nil
}

type NoteEnrichmentResult struct {
	NoteID        int64
	Parsed        models.ParsedNote
	Attempt       int
	Model         string
	PromptVersion string
}

func (s *Service) EnrichNote(ctx context.Context, userID, noteID int64) (NoteEnrichmentResult, error) {
	return s.enrichNote(ctx, userID, noteID, false)
}

func (s *Service) RetryNoteEnrichment(ctx context.Context, userID, noteID int64) (NoteEnrichmentResult, error) {
	return s.enrichNote(ctx, userID, noteID, false)
}

func (s *Service) ReprocessNote(ctx context.Context, userID, noteID int64) (NoteEnrichmentResult, error) {
	s.logger.Info("reprocess started", "operation", "note.reprocess_started", "user_id", userID, "note_id", noteID, "prompt_version", NoteEnrichmentPromptVersion)
	result, err := s.enrichNote(ctx, userID, noteID, true)
	if err == nil {
		s.logger.Info("reprocess completed", "operation", "note.reprocess_completed", "user_id", userID, "note_id", noteID, "attempt", result.Attempt, "model", result.Model, "prompt_version", result.PromptVersion)
	}
	return result, err
}

func (s *Service) EnrichClaimedNote(ctx context.Context, claim store.NoteForEnrichment) (NoteEnrichmentResult, error) {
	userID, noteID := claim.UserID, claim.ID
	if claim.ProcessingStatus != store.NoteProcessingStatusProcessing {
		s.logger.Info("enrichment not claimed", "operation", "note.enrichment_already_processing", "user_id", userID, "note_id", noteID, "status", claim.ProcessingStatus, "attempt", claim.ProcessingAttempts)
		return NoteEnrichmentResult{}, fmt.Errorf("note enrichment not claimable: %s", claim.ProcessingStatus)
	}
	if claim.StaleReclaimed {
		s.logger.Info("stale processing note reclaimed", "operation", "note.enrichment_stale_reclaim", "user_id", userID, "note_id", noteID, "attempt", claim.ProcessingAttempts)
	}
	s.logger.Info("enrichment claim success", "operation", "note.enrichment_claim", "user_id", userID, "note_id", noteID, "attempt", claim.ProcessingAttempts, "prompt_version", NoteEnrichmentPromptVersion)

	model := s.llm.Model()
	started := time.Now()
	s.logger.Info("LLM processing started", "operation", "note.enrichment_llm_started", "user_id", userID, "note_id", noteID, "attempt", claim.ProcessingAttempts, "model", model, "prompt_version", NoteEnrichmentPromptVersion)
	parsed, err := s.llm.ParseManagerNote(ctx, claim.RawText)
	if err != nil {
		_ = s.store.MarkNoteEnrichmentFailed(ctx, userID, noteID, claim.ProcessingStartedAt, err)
		s.logger.Error("enrichment failed", "operation", "note.enrichment_failed", "user_id", userID, "note_id", noteID, "attempt", claim.ProcessingAttempts, "model", model, "prompt_version", NoteEnrichmentPromptVersion, "duration_ms", time.Since(started).Milliseconds(), "error", err)
		return NoteEnrichmentResult{}, fmt.Errorf("now.llm_request: %w", err)
	}
	parsed = models.AddTicketFallbackMentions(parsed, claim.RawText)
	normalizedEntities, skippedEntities := models.NormalizeEntityMentionsForNote(parsed.EntityMentions)
	normalizedDecisions, skippedDecisions := models.NormalizeDecisionsForNote(parsed.Decisions)
	parsed.Decisions = normalizedDecisions
	parsed.EntityMentions = make([]models.EntityMention, 0, len(normalizedEntities))
	entityTypes := map[string]int{}
	for _, rec := range normalizedEntities {
		entityTypes[rec.Type]++
		parsed.EntityMentions = append(parsed.EntityMentions, models.EntityMention{Type: rec.Type, Value: rec.NormalizedValue, RawValue: rec.RawValue, DisplayValue: rec.DisplayValue, Context: rec.Context})
	}
	s.logger.Info("LLM processing completed", "operation", "note.enrichment_llm_completed", "user_id", userID, "note_id", noteID, "attempt", claim.ProcessingAttempts, "model", model, "prompt_version", NoteEnrichmentPromptVersion, "duration_ms", time.Since(started).Milliseconds(), "actions", len(parsed.Actions), "people_notes", len(parsed.PeopleNotes), "people_mentioned", countParsedPeople(parsed), "parsed_decisions", len(parsed.Decisions), "parsed_entities", len(parsed.EntityMentions), "normalized_entities", len(normalizedEntities), "skipped_decisions", skippedDecisions, "skipped_entities", skippedEntities, "entity_types", entityTypes)
	if err := s.store.SaveNoteEnrichmentResult(ctx, userID, noteID, claim.ProcessingStartedAt, parsed, model, NoteEnrichmentPromptVersion); err != nil {
		_ = s.store.MarkNoteEnrichmentFailed(ctx, userID, noteID, claim.ProcessingStartedAt, err)
		s.logger.Error("enrichment failed", "operation", "note.enrichment_failed", "user_id", userID, "note_id", noteID, "attempt", claim.ProcessingAttempts, "model", model, "prompt_version", NoteEnrichmentPromptVersion, "error", err)
		return NoteEnrichmentResult{}, fmt.Errorf("now.persistence: %w", err)
	}
	s.logger.Info("persistence completed", "operation", "note.enrichment_persistence_completed", "user_id", userID, "note_id", noteID, "attempt", claim.ProcessingAttempts, "model", model, "prompt_version", NoteEnrichmentPromptVersion, "persisted_decisions", len(parsed.Decisions), "persisted_entities", len(parsed.EntityMentions))
	return NoteEnrichmentResult{NoteID: noteID, Parsed: parsed, Attempt: claim.ProcessingAttempts, Model: model, PromptVersion: NoteEnrichmentPromptVersion}, nil
}

func (s *Service) enrichNote(ctx context.Context, userID, noteID int64, allowProcessed bool) (NoteEnrichmentResult, error) {
	claim, err := s.store.ClaimNoteForEnrichment(ctx, userID, noteID, s.noteEnrichmentStaleTimeout, allowProcessed)
	if err != nil {
		return NoteEnrichmentResult{}, fmt.Errorf("note.claim_enrichment: %w", err)
	}
	return s.EnrichClaimedNote(ctx, claim)
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
	response, _, err := s.DailyAtDate(ctx, userID, now, refresh)
	return response, err
}

func (s *Service) DailyAtDate(ctx context.Context, userID int64, sourceDate time.Time, refresh bool) (string, int, error) {
	loc := s.dailyLocation
	if loc == nil {
		loc = time.Local
	}
	localNow := sourceDate.In(loc)
	startOfDay := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc)
	endOfDay := startOfDay.AddDate(0, 0, 1)
	log := s.logger.With("operation", "daily", "user_id", userID, "period_start", startOfDay.Format(time.RFC3339), "period_end", endOfDay.Format(time.RFC3339), "timezone", loc.String())
	log.Info("daily command started", "resolved_timezone", loc.String())

	source, err := s.store.RecentDailySource(ctx, userID, startOfDay, endOfDay)
	if err != nil {
		log.Error("daily failed", "failure_stage", "source loading", "operation", "daily.load_source", "error", err)
		return "", 0, fmt.Errorf("daily.load_source: %w", err)
	}
	if strings.TrimSpace(source) == "" {
		log.Info("daily source loaded", "note_count", 0)
		return s.language.CommonMessages().NoNotesToday, 0, nil
	}

	scopeKey := scopedCacheKey(startOfDay.Format("2006-01-02"), s.language)
	sourceHash := utils.HashStrings(source)
	noteCount := countSourceNotes(source)
	log = log.With("scope_key", scopeKey, "source_hash_prefix", logging.HashPrefix(sourceHash), "note_count", noteCount)
	log.Info("daily source loaded")

	if !refresh {
		cached, err := s.store.GetCachedAgentResponse(ctx, userID, "daily", scopeKey, sourceHash, PromptVersion)
		if err != nil {
			log.Error("daily failed", "failure_stage", "cache lookup", "operation", "daily.cache_lookup", "error", err)
			return "", noteCount, fmt.Errorf("daily.cache_lookup: %w", err)
		}
		if cached != nil {
			log.Info("cache hit", "cache_hit", true)
			return cached.ResponseText + "\n\n" + s.language.CommonMessages().DailyCachedNotice, noteCount, nil
		}
	}
	log.Info("cache miss", "cache_hit", false)

	log.Info("LLM call started", "operation", "daily.llm_request")
	digest, err := s.llm.ProcessDaily(ctx, source)
	if err != nil {
		log.Error("daily failed", "failure_stage", "LLM request", "operation", "daily.llm_request", "error", err)
		return "", noteCount, fmt.Errorf("daily.llm_request: %w", err)
	}
	log.Info("LLM call completed", "operation", "daily.llm_request")
	responseText := FormatDailyDigestForLanguage(digest, s.language)
	if strings.TrimSpace(responseText) == "" {
		err := fmt.Errorf("empty formatted daily digest")
		log.Error("daily failed", "failure_stage", "formatting", "operation", "daily.formatting", "error", err)
		return "", noteCount, err
	}
	responseJSON, err := json.Marshal(digest)
	if err != nil {
		log.Error("daily failed", "failure_stage", "JSON parsing", "operation", "daily.parse_json", "error", err)
		return "", noteCount, fmt.Errorf("daily.parse_json: %w", err)
	}

	if err := s.store.SaveAgentResponse(ctx, models.AgentResponse{UserID: userID, Kind: "daily", ScopeKey: scopeKey, PeriodStart: &startOfDay, PeriodEnd: &endOfDay, SourceHash: sourceHash, PromptVersion: PromptVersion, Model: s.llm.Model(), ResponseText: responseText, ResponseJSON: string(responseJSON)}); err != nil {
		log.Error("daily failed", "failure_stage", "cache save", "operation", "daily.cache_save", "error", err)
		return "", noteCount, fmt.Errorf("daily.cache_save: %w", err)
	}
	log.Info("digest cached", "operation", "daily.cache_save")
	log.Info("formatted response sent", "operation", "daily.formatting", "response_length", len(responseText))
	return responseText, noteCount, nil
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
	noteCount := countSourceNotes(source)
	log = log.With("scope_key", scopeKey, "source_hash_prefix", logging.HashPrefix(sourceHash), "note_count", noteCount)
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
	if len(p.Decisions) > 0 {
		b.WriteString(labels.Decisions + ":\n")
		for _, d := range p.Decisions {
			b.WriteString("- " + d.Text + "\n")
		}
		b.WriteString("\n")
	}
	if len(p.EntityMentions) > 0 {
		b.WriteString(labels.Entities + ":\n")
		for _, e := range p.EntityMentions {
			value := strings.TrimSpace(e.DisplayValue)
			if value == "" {
				value = strings.TrimSpace(e.Value)
			}
			if value == "" {
				continue
			}
			b.WriteString(fmt.Sprintf("- %s: %s\n", e.Type, value))
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
