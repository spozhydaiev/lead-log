package llm

import (
	"encoding/json"
	"fmt"
	"log"
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
	var digest models.DailyDigest
	if err := json.Unmarshal([]byte(content), &digest); err != nil {
		return models.DailyDigest{}, fmt.Errorf("parse daily digest json: %w; content=%s", err, content)
	}

	digest.ShortSummary = strings.TrimSpace(digest.ShortSummary)
	digest.OpenLoops = validDailyOpenLoops(digest.OpenLoops)
	digest.TicketCandidates = validDailyTicketCandidates(digest.TicketCandidates)
	digest.Decisions = validDailyTextItems("decisions", digest.Decisions)
	digest.SuggestedNextSteps = validDailyTextItems("suggested_next_steps", digest.SuggestedNextSteps)
	digest.UnclearItems = validDailyTextItems("unclear_items", digest.UnclearItems)

	peopleHighlights, err := validDailyPeopleHighlights(digest.PeopleHighlights)
	if err != nil {
		return models.DailyDigest{}, err
	}
	digest.PeopleHighlights = peopleHighlights

	if unusableDailyDigest(digest) {
		return models.DailyDigest{}, fmt.Errorf("parse daily digest json: digest has no summary or valid sections")
	}

	return digest, nil
}

func validDailyOpenLoops(items []models.DailyOpenLoop) []models.DailyOpenLoop {
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
			log.Printf("daily digest skipped open_loops[%d]: title is required", i)
			continue
		}
		valid = append(valid, item)
	}
	return valid
}

func validDailyTicketCandidates(items []models.DailyTicketCandidate) []models.DailyTicketCandidate {
	valid := make([]models.DailyTicketCandidate, 0, len(items))
	for i, item := range items {
		item.Title = strings.TrimSpace(item.Title)
		item.Context = strings.TrimSpace(item.Context)
		if item.Owner != nil {
			owner := strings.TrimSpace(*item.Owner)
			item.Owner = &owner
		}
		if item.Title == "" {
			log.Printf("daily digest skipped ticket_candidates[%d]: title is required", i)
			continue
		}
		valid = append(valid, item)
	}
	return valid
}

func validDailyPeopleHighlights(items []models.DailyPeopleHighlight) ([]models.DailyPeopleHighlight, error) {
	valid := make([]models.DailyPeopleHighlight, 0, len(items))
	for i := range items {
		h := items[i]
		h.PersonName = strings.TrimSpace(h.PersonName)
		h.Text = strings.TrimSpace(h.Text)
		if h.PersonName == "" || h.Text == "" {
			log.Printf("daily digest skipped people_highlights[%d]: person_name and text are required", i)
			continue
		}
		normalizeDailyPeopleHighlight(i, &h)
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

func validDailyTextItems(section string, items []models.DailyTextItem) []models.DailyTextItem {
	valid := make([]models.DailyTextItem, 0, len(items))
	for i, item := range items {
		item.Text = strings.TrimSpace(item.Text)
		if item.Text == "" {
			log.Printf("daily digest skipped %s[%d]: text is required", section, i)
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

func normalizeDailyPeopleHighlight(index int, h *models.DailyPeopleHighlight) {
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
		log.Printf("daily digest normalized people_highlights[%d] type=%q->%q theme=%q->%q", index, originalType, h.Type, originalTheme, h.Theme)
	}
}
