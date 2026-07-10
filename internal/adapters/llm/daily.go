package llm

import (
	"encoding/json"
	"fmt"
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
	for i, h := range digest.PeopleHighlights {
		if strings.TrimSpace(h.PersonName) == "" || strings.TrimSpace(h.Text) == "" {
			return models.DailyDigest{}, fmt.Errorf("parse daily digest json: people_highlights[%d] requires person_name and text", i)
		}
		if !allowedDailyHighlightTypes[h.Type] {
			return models.DailyDigest{}, fmt.Errorf("parse daily digest json: people_highlights[%d] has invalid type %q", i, h.Type)
		}
		if !allowedDailyThemes[h.Theme] {
			return models.DailyDigest{}, fmt.Errorf("parse daily digest json: people_highlights[%d] has invalid theme %q", i, h.Theme)
		}
	}
	return digest, nil
}
