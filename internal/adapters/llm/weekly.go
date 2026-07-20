package llm

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/spozhydaiev/lead-log/internal/models"
)

func ParseWeeklyDigestJSON(content string) (models.WeeklyDigest, error) {
	return ParseWeeklyDigestJSONWithLogger(content, slog.Default())
}

func ParseWeeklyDigestJSONWithLogger(content string, logger *slog.Logger) (models.WeeklyDigest, error) {
	if logger == nil {
		logger = slog.Default()
	}
	var d models.WeeklyDigest
	if err := json.Unmarshal([]byte(content), &d); err != nil {
		return models.WeeklyDigest{}, fmt.Errorf("parse weekly digest json: %w", err)
	}
	d.Summary = strings.TrimSpace(d.Summary)
	d.Highlights = validWeeklyTextItems(d.Highlights)
	d.Actions = validWeeklyTextItems(d.Actions)
	d.Decisions = validWeeklyTextItems(d.Decisions)
	d.People = validWeeklyTextItems(d.People)
	d.Tickets = validWeeklyTextItems(d.Tickets)
	d.Risks = validWeeklyTextItems(d.Risks)
	d.OpenQuestions = validWeeklyTextItems(d.OpenQuestions)
	d.RepeatedTopics = validWeeklyTextItems(d.RepeatedTopics)
	if d.Summary == "" && len(d.Highlights)+len(d.Actions)+len(d.Decisions)+len(d.People)+len(d.Tickets)+len(d.Risks)+len(d.OpenQuestions)+len(d.RepeatedTopics) == 0 {
		return models.WeeklyDigest{}, fmt.Errorf("parse weekly digest json: digest has no summary or valid sections")
	}
	logger.Info("JSON parse completed", "operation", "weekly.parse_json", "highlights", len(d.Highlights), "actions", len(d.Actions))
	return d, nil
}

func validWeeklyTextItems(items []models.WeeklyTextItem) []models.WeeklyTextItem {
	out := make([]models.WeeklyTextItem, 0, len(items))
	for _, item := range items {
		item.Text = strings.TrimSpace(item.Text)
		if item.Text != "" {
			out = append(out, item)
		}
	}
	return out
}
