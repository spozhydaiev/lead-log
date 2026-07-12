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
	if strings.TrimSpace(digest.ShortSummary) == "" {
		return models.DailyDigest{}, fmt.Errorf("parse daily digest json: short_summary is required")
	}
	for i := range digest.PeopleHighlights {
		h := &digest.PeopleHighlights[i]
		if strings.TrimSpace(h.PersonName) == "" || strings.TrimSpace(h.Text) == "" {
			return models.DailyDigest{}, fmt.Errorf("parse daily digest json: people_highlights[%d] requires person_name and text", i)
		}
		normalizeDailyPeopleHighlight(i, h)
		if !allowedDailyHighlightTypes[h.Type] {
			return models.DailyDigest{}, fmt.Errorf("parse daily digest json: people_highlights[%d] has invalid type %q", i, h.Type)
		}
		if !allowedDailyThemes[h.Theme] {
			return models.DailyDigest{}, fmt.Errorf("parse daily digest json: people_highlights[%d] has invalid theme %q", i, h.Theme)
		}
	}
	return digest, nil
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
