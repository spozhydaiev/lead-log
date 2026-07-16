package services

import (
	"strings"
	"testing"
	"time"

	"github.com/spozhydaiev/lead-log/internal/models"
)

func TestFormatPersonContextGroupsSectionsAndAvoidsPerformanceJudgment(t *testing.T) {
	d := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	pc := models.PersonContext{CanonicalName: "Адлет", LastMentionAt: &d, MentionCount: 8,
		OpenActions:      []models.PersonContextAction{{ID: 31, Title: "Перевірити QA feedback", Status: "open", SourceNoteID: 184, Date: d}},
		CompletedActions: []models.PersonContextAction{{ID: 30, Title: "Закрити follow-up", Status: "done", SourceNoteID: 183, Date: d.Add(-time.Hour)}},
		Commitments:      []models.PersonContextItem{{Text: "Підготувати оцінку нового flow до п’ятниці.", SourceNoteID: 179, Date: d.AddDate(0, 0, -3)}},
		FollowUps:        []models.PersonContextItem{{Text: "Уточнити retry policy.", SourceNoteID: 178, Date: d.AddDate(0, 0, -4)}},
		Feedback:         []models.PersonContextItem{{Text: "Отримав feedback щодо QA.", SourceNoteID: 177, Date: d.AddDate(0, 0, -5)}},
		Achievements:     []models.PersonContextItem{{Text: "Завершив міграцію.", SourceNoteID: 176, Date: d.AddDate(0, 0, -6)}},
		Concerns:         []models.PersonContextItem{{Text: "Є ризик по дедлайну.", SourceNoteID: 175, Date: d.AddDate(0, 0, -7)}},
		Decisions:        []models.PersonContextDecision{{Text: "Залишити retry limit.", Topic: "architecture", Status: "active", SourceNoteID: 170, Date: d.AddDate(0, 0, -8)}},
		OpenQuestions:    []models.PersonContextItem{{Text: "Чи потрібен окремий ліміт?", SourceNoteID: 169, Date: d.AddDate(0, 0, -8)}},
		RecentNotes:      []models.PersonContextItem{{Text: "Обговорили retry policy.", SourceNoteID: 168, Date: d.AddDate(0, 0, -9)}},
		PossibleMentions: []models.PersonContextItem{{Text: "Адлет згаданий у raw note.", SourceNoteID: 167, Date: d.AddDate(0, 0, -10)}},
		Sources:          []models.PersonContextSource{{NoteID: 184, Date: d}, {NoteID: 179, Date: d.AddDate(0, 0, -3)}},
	}
	got := formatPersonContext(pc, models.LanguageEnglish)
	for _, want := range []string{"Адлет", "Context period: last 30 days", "Last mentioned: 15 July 2026", "Open actions:", "#31 Перевірити QA feedback", "Completed actions:", "Commitments:", "Follow-ups:", "Feedback:", "Achievements:", "Concerns:", "Decisions:", "Open questions:", "Recent context:", "Possible mentions:", "Sources:", "note #184"} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatted person context missing %q\n%s", want, got)
		}
	}
	for _, forbidden := range []string{"score", "sentiment", "strong performer", "weak performer", "promote", "fire"} {
		if strings.Contains(strings.ToLower(got), forbidden) {
			t.Fatalf("formatter added performance judgment %q\n%s", forbidden, got)
		}
	}
}

func TestFormatPersonContextNoRecentContext(t *testing.T) {
	got := formatPersonContext(models.PersonContext{CanonicalName: "Adlet"}, models.LanguageEnglish)
	if got != "Adlet exists in your people list, but no recent context was found." {
		t.Fatalf("got %q", got)
	}
}

func TestPersonSourcesDeduplicatesAndBounds(t *testing.T) {
	now := time.Now()
	pc := models.PersonContext{Commitments: []models.PersonContextItem{{Text: "a", SourceNoteID: 1, Date: now.Add(-time.Hour)}, {Text: "b", SourceNoteID: 1, Date: now}}, Decisions: []models.PersonContextDecision{{Text: "c", SourceNoteID: 2, Date: now.Add(-2 * time.Hour)}}}
	got := personSources(pc, 1)
	if len(got) != 1 || got[0].NoteID != 1 || !got[0].Date.Equal(now) {
		t.Fatalf("sources=%#v", got)
	}
}
