package services

import (
	"strings"
	"testing"

	"github.com/spozhydaiev/lead-log/internal/models"
)

func TestFormatAgendaOmitsEmptySectionsAndIncludesSources(t *testing.T) {
	agenda := models.Agenda{
		DiscussionTopics:   []models.AgendaDiscussionTopic{{Title: "Поточний фокус", Context: "є нотатка про пріоритети", SourceNoteIDs: []int64{3, 7}}},
		SuggestedQuestions: []models.AgendaTextItem{{Text: "Що зараз найбільше блокує рух?", SourceNoteIDs: []int64{7}}},
	}

	got := FormatAgenda("Андрій", agenda)
	for _, want := range []string{"1:1 agenda — Андрій", "Теми для обговорення", "Поточний фокус — є нотатка про пріоритети", "source notes: #3, #7", "Suggested questions"} {
		if !strings.Contains(got, want) {
			t.Fatalf("FormatAgenda() missing %q\n%s", want, got)
		}
	}
	for _, notWant := range []string{"Open follow-ups", "Positive signals to mention", "Risks / concerns to clarify", "Growth topics"} {
		if strings.Contains(got, notWant) {
			t.Fatalf("FormatAgenda() should omit empty section %q\n%s", notWant, got)
		}
	}
}
