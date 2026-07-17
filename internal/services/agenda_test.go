package services

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/spozhydaiev/lead-log/internal/models"
)

func TestBuildAgendaRulesCompletionPriorityAndBalance(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -90)
	recent := now.AddDate(0, 0, -2)
	overdue := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	pc := models.PersonContext{CanonicalName: "Адлет",
		OpenActions:      []models.PersonContextAction{{ID: 1, Title: "old open", Status: "open", SourceNoteID: 1, Date: old, DueAt: &overdue}, {ID: 2, Title: "future", Status: "open", SourceNoteID: 2, Date: recent, DueAt: &future}, {ID: 9, Title: "wrong status", Status: "done", SourceNoteID: 9, Date: recent}},
		CompletedActions: []models.PersonContextAction{{ID: 3, Title: "completed", Status: "done", SourceNoteID: 3, Date: recent}},
		Commitments:      []models.PersonContextItem{{Text: "commit", Type: "commitment", SourceNoteID: 4, Date: recent}, {Text: "too old", Type: "commitment", SourceNoteID: 40, Date: old}},
		FollowUps:        []models.PersonContextItem{{Text: "follow", SourceNoteID: 5, Date: recent}}, Concerns: []models.PersonContextItem{{Text: "delivery risk", SourceNoteID: 6, Date: recent}}, OpenQuestions: []models.PersonContextItem{{Text: "question?", SourceNoteID: 7, Date: recent}, {Text: " ", SourceNoteID: 8, Date: recent}},
		Decisions: []models.PersonContextDecision{{Text: "active decision", Status: "active", SourceNoteID: 10, Date: recent}, {Text: "reversed", Status: "reversed", SourceNoteID: 11, Date: recent}}, Achievements: []models.PersonContextItem{{Text: "achievement", SourceNoteID: 12, Date: recent}}, Feedback: []models.PersonContextItem{{Text: "collaborated well", Type: "collaboration", SourceNoteID: 13, Date: recent}}, PossibleMentions: []models.PersonContextItem{{Text: "raw exact mention", SourceNoteID: 14, Date: recent}},
	}
	a := BuildAgendaFromPersonContext(pc, now)
	if len(a.MustDiscuss) != 4 || a.MustDiscuss[1].Text != "old open" || a.MustDiscuss[1].Priority != models.AgendaPriorityHigh {
		t.Fatalf("must=%#v", a.MustDiscuss)
	}
	for _, x := range a.MustDiscuss {
		if x.Text == "wrong status" || x.Text == "too old" {
			t.Fatalf("closed/old item leaked: %#v", x)
		}
	}
	if len(a.PositiveNotes) != 3 || len(a.FollowUps) != 1 || len(a.OpenQuestions) != 1 || len(a.Decisions) != 1 || len(a.Context) != 1 {
		t.Fatalf("agenda sections=%#v", a)
	}
	if a.Context[0].Kind != models.AgendaItemContext {
		t.Fatal("raw fallback was classified as structured")
	}
}

func TestBuildAgendaDedupeLimitsAndUTF8Formatter(t *testing.T) {
	now := time.Now()
	pc := models.PersonContext{CanonicalName: "Żółć Адлет"}
	for i := int64(1); i <= 13; i++ {
		pc.OpenActions = append(pc.OpenActions, models.PersonContextAction{ID: i, Title: "Перевірити якість", Status: "open", SourceNoteID: i, Date: now.Add(time.Duration(-i) * time.Minute)})
	}
	pc.Commitments = []models.PersonContextItem{{Text: "Перевірити якість", SourceNoteID: 1, Date: now}}
	a := BuildAgendaFromPersonContext(pc, now)
	if len(a.MustDiscuss) != AgendaMustDiscussLimit || a.HiddenMustDiscuss != 3 {
		t.Fatalf("len/hidden=%d/%d", len(a.MustDiscuss), a.HiddenMustDiscuss)
	}
	out := FormatPersonAgenda(a, models.LanguageUkrainian)
	if !utf8.ValidString(out) || !strings.Contains(out, "І ще 3") || !strings.Contains(out, "note #1") {
		t.Fatalf("bad output: %s", out)
	}
	for _, bad := range []string{"score", "sentiment", "personality", "звільнити"} {
		if strings.Contains(strings.ToLower(out), bad) {
			t.Fatalf("judgment leaked: %s", bad)
		}
	}
}

func TestBuildAgendaEmptyAndPositiveOnly(t *testing.T) {
	now := time.Now()
	a := BuildAgendaFromPersonContext(models.PersonContext{CanonicalName: "A"}, now)
	if !agendaEmpty(a) {
		t.Fatal("expected empty")
	}
	a = BuildAgendaFromPersonContext(models.PersonContext{CanonicalName: "A", Achievements: []models.PersonContextItem{{Text: "shipped", SourceNoteID: 1, Date: now}}}, now)
	if agendaEmpty(a) || len(a.MustDiscuss) != 0 || len(a.PositiveNotes) != 1 {
		t.Fatalf("positive-only=%#v", a)
	}
}
