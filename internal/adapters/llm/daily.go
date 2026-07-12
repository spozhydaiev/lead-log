package llm

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/spozhydaiev/lead-log/internal/models"
)

var allowedDailyHighlightTypes = map[string]bool{
	"positive_signal": true, "concern": true, "follow_up_needed": true, "growth_topic": true,
	"context": true, "commitment": true, "risk": true,
}

var allowedDailyThemes = map[string]bool{
	"communication": true, "ownership": true, "delivery": true, "collaboration": true,
	"technical_quality": true, "reliability": true, "hiring": true, "release": true,
	"process": true, "other": true,
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

	peopleHighlights, err := validDailyPeopleHighlights(digest.PeopleHighlights, logger)
	if err != nil {
		return models.DailyDigest{}, err
	}
	digest.PeopleHighlights = peopleHighlights

	if unusableDailyDigest(digest) {
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

func validDailyPeopleHighlights(items []models.DailyPeopleHighlight, logger *slog.Logger) ([]models.DailyPeopleHighlight, error) {
	valid := make([]models.DailyPeopleHighlight, 0, len(items))
	for i := range items {
		h := items[i]
		h.PersonName = strings.TrimSpace(h.PersonName)
		h.Text = strings.TrimSpace(h.Text)
		if h.PersonName == "" || h.Text == "" {
			logger.Warn("invalid optional item skipped", "operation", "daily.parse_json", "section", "people_highlights", "item_index", i, "reason", "person_name and text are required")
			continue
		}
		normalizeDailyPeopleHighlight(i, &h, logger)
		if !allowedDailyHighlightTypes[h.Type] {
			return nil, fmt.Errorf("parse daily digest json: people_highlights[%d] has invalid type %q", i, h.Type)
		}
		if !allowedDailyThemes[h.Theme] {
			return nil, fmt.Errorf("parse daily digest json: people_highlights[%d] has invalid theme %q", i, h.Theme)
		}
		valid = append(valid, h)
	}
	return valid, nil
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

func normalizeDailyPeopleHighlight(index int, h *models.DailyPeopleHighlight, logger *slog.Logger) {
	originalType := h.Type
	originalTheme := h.Theme
	h.Type = strings.TrimSpace(h.Type)
	h.Theme = strings.TrimSpace(h.Theme)

	themeLooksLikeType := allowedDailyHighlightTypes[h.Theme]
	if !allowedDailyHighlightTypes[h.Type] && themeLooksLikeType {
		h.Type = h.Theme
	}

	if !allowedDailyThemes[h.Theme] {
		h.Theme = "other"
	}

	if h.Type != originalType || h.Theme != originalTheme {
		logger.Warn("invalid optional item normalized", "operation", "daily.parse_json", "section", "people_highlights", "item_index", index, "reason", "invalid type/theme normalized")
	}
}
