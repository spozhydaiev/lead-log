package llm

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spozhydaiev/lead-log/internal/models"
)

func ParseAgendaJSON(content string) (models.Agenda, error) {
	var agenda models.Agenda
	if err := json.Unmarshal([]byte(content), &agenda); err != nil {
		return models.Agenda{}, fmt.Errorf("parse agenda json: %w; content=%s", err, content)
	}
	agenda.DiscussionTopics = filterAgendaDiscussionTopics(agenda.DiscussionTopics)
	agenda.OpenFollowups = filterAgendaTextItems(agenda.OpenFollowups)
	agenda.PositiveSignals = filterAgendaTextItems(agenda.PositiveSignals)
	agenda.RisksOrConcernsToClarify = filterAgendaTextItems(agenda.RisksOrConcernsToClarify)
	agenda.GrowthTopics = filterAgendaTextItems(agenda.GrowthTopics)
	agenda.SuggestedQuestions = filterAgendaTextItems(agenda.SuggestedQuestions)
	return agenda, nil
}

func filterAgendaDiscussionTopics(items []models.AgendaDiscussionTopic) []models.AgendaDiscussionTopic {
	out := make([]models.AgendaDiscussionTopic, 0, len(items))
	for _, item := range items {
		item.Title = strings.TrimSpace(item.Title)
		item.Context = strings.TrimSpace(item.Context)
		if item.Title == "" && item.Context == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func filterAgendaTextItems(items []models.AgendaTextItem) []models.AgendaTextItem {
	out := make([]models.AgendaTextItem, 0, len(items))
	for _, item := range items {
		item.Text = strings.TrimSpace(item.Text)
		if item.Text == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}
