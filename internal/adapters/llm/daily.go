package llm

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/spozhydaiev/lead-log/internal/models"
)

var dailyHighlightTypes = []string{
	"positive_signal", "concern", "follow_up_needed", "growth_topic",
	"context", "commitment", "risk", "collaboration",
}

var allowedDailyHighlightTypes = stringSet(dailyHighlightTypes)

var dailyHighlightTypeAliases = map[string]string{
	"teamwork": "collaboration",
}

const fallbackDailyHighlightType = "context"

var dailyThemes = []string{
	"communication", "ownership", "delivery", "collaboration",
	"technical_quality", "reliability", "hiring", "release",
	"process", "other",
}

var allowedDailyThemes = stringSet(dailyThemes)

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func DailyHighlightTypesForPrompt() string {
	return strings.Join(dailyHighlightTypes, ", ")
}

func DailyThemesForPrompt() string {
	return strings.Join(dailyThemes, ", ")
}

func DailyHighlightTypesSchemaForPrompt() string {
	return strings.Join(dailyHighlightTypes, "|")
}

func DailyThemesSchemaForPrompt() string {
	return strings.Join(dailyThemes, "|")
}

func ParseDailyDigestJSON(content string) (models.DailyDigest, error) {
	return ParseDailyDigestJSONWithLogger(content, slog.Default())
}

func ParseDailyDigestJSONWithLogger(content string, logger *slog.Logger) (models.DailyDigest, error) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("JSON parse started", "operation", "daily.parse_json", "response_size", len(content))
	var digest models.DailyDigest
	if err := json.Unmarshal([]byte(content), &digest); err != nil {
		logger.Warn("JSON parse failure", "operation", "daily.parse_json", "response_size", len(content), "error", err)
		return models.DailyDigest{}, fmt.Errorf("parse daily digest json: %w", err)
	}

	digest.ShortSummary = strings.TrimSpace(digest.ShortSummary)
	digest.OpenLoops = validDailyOpenLoops(digest.OpenLoops, logger)
	digest.TicketCandidates = validDailyTicketCandidates(digest.TicketCandidates, logger)
	digest.Decisions = validDailyTextItems("decisions", digest.Decisions, logger)
	digest.SuggestedNextSteps = validDailyTextItems("suggested_next_steps", digest.SuggestedNextSteps, logger)
	digest.UnclearItems = validDailyTextItems("unclear_items", digest.UnclearItems, logger)

	digest.PeopleHighlights = validDailyPeopleHighlights(digest.PeopleHighlights, logger)

	if unusableDailyDigest(digest) {
		logger.Warn("daily digest rejected as unusable", "operation", "daily.parse_json")
		return models.DailyDigest{}, fmt.Errorf("parse daily digest json: digest has no summary or valid sections")
	}

	logger.Info("JSON parse completed", "operation", "daily.parse_json", "open_loops", len(digest.OpenLoops), "ticket_candidates", len(digest.TicketCandidates), "people_highlights", len(digest.PeopleHighlights), "decisions", len(digest.Decisions), "suggested_next_steps", len(digest.SuggestedNextSteps), "unclear_items", len(digest.UnclearItems))
	return digest, nil
}

func validDailyOpenLoops(items []models.DailyOpenLoop, logger *slog.Logger) []models.DailyOpenLoop {
	valid := make([]models.DailyOpenLoop, 0, len(items))
	for i, item := range items {
		item.Title = strings.TrimSpace(item.Title)
		if item.Owner != nil {
			owner := strings.TrimSpace(*item.Owner)
			item.Owner = &owner
		}
		if item.DueHint != nil {
			dueHint := strings.TrimSpace(*item.DueHint)
			item.DueHint = &dueHint
		}
		if item.Title == "" {
			logger.Warn("invalid optional item skipped", "operation", "daily.parse_json", "section", "open_loops", "item_index", i, "reason", "title is required")
			continue
		}
		valid = append(valid, item)
	}
	return valid
}

func validDailyTicketCandidates(items []models.DailyTicketCandidate, logger *slog.Logger) []models.DailyTicketCandidate {
	valid := make([]models.DailyTicketCandidate, 0, len(items))
	for i, item := range items {
		item.Title = strings.TrimSpace(item.Title)
		item.Context = strings.TrimSpace(item.Context)
		if item.Owner != nil {
			owner := strings.TrimSpace(*item.Owner)
			item.Owner = &owner
		}
		if item.Title == "" {
			logger.Warn("invalid optional item skipped", "operation", "daily.parse_json", "section", "ticket_candidates", "item_index", i, "reason", "title is required")
			continue
		}
		valid = append(valid, item)
	}
	return valid
}

func validDailyPeopleHighlights(items []models.DailyPeopleHighlight, logger *slog.Logger) []models.DailyPeopleHighlight {
	valid := make([]models.DailyPeopleHighlight, 0, len(items))
	normalizedTypes := 0
	unknownTypes := 0
	normalizedThemes := 0
	skipped := 0
	for i := range items {
		h := items[i]
		h.PersonName = strings.TrimSpace(h.PersonName)
		h.Text = strings.TrimSpace(h.Text)
		if h.PersonName == "" || h.Text == "" {
			skipped++
			logger.Warn("invalid optional item skipped", "operation", "daily.parse_json", "section", "people_highlights", "item_index", i, "reason", "person_name and text are required")
			continue
		}
		stats := normalizeDailyPeopleHighlight(i, &h, logger)
		if stats.typeNormalized {
			normalizedTypes++
		}
		if stats.unknownType {
			unknownTypes++
		}
		if stats.themeNormalized {
			normalizedThemes++
		}
		valid = append(valid, h)
	}
	if normalizedTypes > 0 || normalizedThemes > 0 || skipped > 0 {
		logger.Warn("daily digest accepted with normalization", "operation", "daily.parse_json", "unknown_people_highlight_types", unknownTypes, "normalized_people_highlight_types", normalizedTypes, "normalized_people_highlight_themes", normalizedThemes, "skipped_people_highlights", skipped, "unknown_optional_enums", unknownTypes+normalizedThemes)
	}
	return valid
}

func validDailyTextItems(section string, items []models.DailyTextItem, logger *slog.Logger) []models.DailyTextItem {
	valid := make([]models.DailyTextItem, 0, len(items))
	for i, item := range items {
		item.Text = strings.TrimSpace(item.Text)
		if item.Text == "" {
			logger.Warn("invalid optional item skipped", "operation", "daily.parse_json", "section", section, "item_index", i, "reason", "text is required")
			continue
		}
		valid = append(valid, item)
	}
	return valid
}

func unusableDailyDigest(d models.DailyDigest) bool {
	return d.ShortSummary == "" &&
		len(d.OpenLoops) == 0 &&
		len(d.TicketCandidates) == 0 &&
		len(d.PeopleHighlights) == 0 &&
		len(d.Decisions) == 0 &&
		len(d.SuggestedNextSteps) == 0 &&
		len(d.UnclearItems) == 0
}

type dailyPeopleHighlightNormalization struct {
	typeNormalized  bool
	unknownType     bool
	themeNormalized bool
}

func normalizeDailyPeopleHighlight(index int, h *models.DailyPeopleHighlight, logger *slog.Logger) dailyPeopleHighlightNormalization {
	originalType := h.Type
	originalTheme := h.Theme
	h.Type = strings.TrimSpace(h.Type)
	h.Theme = strings.TrimSpace(h.Theme)

	themeLooksLikeType := allowedDailyHighlightTypes[h.Theme]
	stats := dailyPeopleHighlightNormalization{}
	if alias, ok := dailyHighlightTypeAliases[h.Type]; ok {
		h.Type = alias
	}
	if !allowedDailyHighlightTypes[h.Type] && themeLooksLikeType {
		h.Type = h.Theme
	}
	if !allowedDailyHighlightTypes[h.Type] {
		h.Type = fallbackDailyHighlightType
		stats.unknownType = true
	}

	if !allowedDailyThemes[h.Theme] {
		h.Theme = "other"
	}

	stats.typeNormalized = h.Type != strings.TrimSpace(originalType)
	stats.themeNormalized = h.Theme != strings.TrimSpace(originalTheme)
	if h.Type != originalType || h.Theme != originalTheme {
		logger.Warn("invalid optional item normalized", "operation", "daily.parse_json", "section", "people_highlights", "item_index", index, "reason", "invalid type/theme normalized")
	}
	return stats
}
