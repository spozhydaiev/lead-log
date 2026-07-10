package services

import (
	"strings"
	"testing"

	"github.com/spozhydaiev/lead-log/internal/models"
)

func TestFormatDailyDigest(t *testing.T) {
	owner := "Олена"
	out := FormatDailyDigest(models.DailyDigest{
		ShortSummary:       "Є кілька follow-up після релізу.",
		OpenLoops:          []models.DailyOpenLoop{{Title: "Уточнити статус релізу", Owner: &owner, DueHint: strPtr("завтра"), SourceNoteIDs: []int64{7}}},
		TicketCandidates:   []models.DailyTicketCandidate{{Title: "Додати ретраї", Context: "Падіння інтеграції", SourceNoteIDs: []int64{8}}},
		PeopleHighlights:   []models.DailyPeopleHighlight{{PersonName: "Іван", Type: "follow_up_needed", Theme: "delivery", Text: "Потрібен follow-up щодо задачі.", SourceNoteIDs: []int64{9}}},
		Decisions:          []models.DailyTextItem{{Text: "Домовились перенести запуск.", SourceNoteIDs: []int64{10}}},
		SuggestedNextSteps: []models.DailyTextItem{{Text: "Написати команді апдейт.", SourceNoteIDs: []int64{11}}},
		UnclearItems:       []models.DailyTextItem{{Text: "Немає власника для багу.", SourceNoteIDs: []int64{12}}},
	})
	for _, want := range []string{"Коротко", "Open loops", "Ticket candidates", "People highlights", "Decisions / agreements", "Suggested next steps", "Unclear items", "джерела: #7"} {
		if !strings.Contains(out, want) {
			t.Fatalf("formatted digest missing %q:\n%s", want, out)
		}
	}
}

func TestFormatDailyDigestOmitsEmptySections(t *testing.T) {
	out := FormatDailyDigest(models.DailyDigest{ShortSummary: "Тільки підсумок."})
	if strings.Contains(out, "Open loops") || strings.Contains(out, "Ticket candidates") || strings.Contains(out, "People highlights") {
		t.Fatalf("empty sections should be omitted:\n%s", out)
	}
}

func strPtr(s string) *string { return &s }

func TestDailyDigestToParsedNoteCarriesSourceNoteIDs(t *testing.T) {
	owner := "Олена"
	parsed := dailyDigestToParsedNote(models.DailyDigest{
		OpenLoops:        []models.DailyOpenLoop{{Title: "Уточнити ETA", Owner: &owner, SourceNoteIDs: []int64{3, 1}}},
		PeopleHighlights: []models.DailyPeopleHighlight{{PersonName: "Олена", Type: "commitment", Theme: "delivery", Text: "Пообіцяла оновити ETA.", SourceNoteIDs: []int64{3}}},
	})
	if got := parsed.Actions[0].SourceNoteIDs; len(got) != 2 || got[0] != 3 || got[1] != 1 {
		t.Fatalf("action source note ids were not preserved: %#v", got)
	}
	if got := parsed.PeopleNotes[0].SourceNoteIDs; len(got) != 1 || got[0] != 3 {
		t.Fatalf("people note source note ids were not preserved: %#v", got)
	}
}
