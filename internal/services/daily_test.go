package services

import (
	"os"
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
	for _, want := range []string{"Brief", "Open loops", "Ticket candidates", "People highlights", "Decisions / agreements", "Suggested next steps", "Unclear items", "sources: #7"} {
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

func TestDailyServiceDoesNotPersistStructuredItems(t *testing.T) {
	content, err := os.ReadFile("service.go")
	if err != nil {
		t.Fatalf("read service.go: %v", err)
	}
	dailyStart := strings.Index(string(content), "func (s *Service) DailyAt")
	weeklyStart := strings.Index(string(content), "func (s *Service) Weekly")
	if dailyStart < 0 || weeklyStart < 0 || weeklyStart <= dailyStart {
		t.Fatalf("could not locate DailyAt function body")
	}
	dailyBody := string(content)[dailyStart:weeklyStart]
	for _, forbidden := range []string{"PersistDailyStructured", "dailyDigestToParsedNote", "cachedDailyStructured"} {
		if strings.Contains(dailyBody, forbidden) {
			t.Fatalf("/daily must be read-only for actions and people_notes; found %q", forbidden)
		}
	}
	if !strings.Contains(dailyBody, "SaveAgentResponse") || !strings.Contains(dailyBody, "ResponseJSON") {
		t.Fatalf("/daily should still cache response_text and response_json")
	}
}

func TestNowStillSavesParsedStructuredItems(t *testing.T) {
	content, err := os.ReadFile("service.go")
	if err != nil {
		t.Fatalf("read service.go: %v", err)
	}
	serviceSource := string(content)
	addNoteStart := strings.Index(serviceSource, "func (s *Service) AddNote")
	openStart := strings.Index(serviceSource, "func (s *Service) OpenActions")
	if addNoteStart < 0 || openStart < 0 || openStart <= addNoteStart {
		t.Fatalf("could not locate AddNote function body")
	}
	addNoteBody := serviceSource[addNoteStart:openStart]
	for _, required := range []string{"ParseManagerNote", "SaveParsedNote"} {
		if !strings.Contains(addNoteBody, required) {
			t.Fatalf("/now should still parse and persist structured actions/people_notes; missing %q", required)
		}
	}
}

func TestDailyAtZeroNotesReturnsBeforeLLMCall(t *testing.T) {
	content, err := os.ReadFile("service.go")
	if err != nil {
		t.Fatalf("read service.go: %v", err)
	}
	dailyStart := strings.Index(string(content), "func (s *Service) DailyAtDate")
	weeklyStart := strings.Index(string(content), "func (s *Service) Weekly")
	if dailyStart < 0 || weeklyStart < 0 || weeklyStart <= dailyStart {
		t.Fatalf("could not locate DailyAtDate function body")
	}
	dailyBody := string(content)[dailyStart:weeklyStart]
	zeroNotes := strings.Index(dailyBody, "strings.TrimSpace(source) == \"\"")
	llmCall := strings.Index(dailyBody, "ProcessDaily")
	if zeroNotes < 0 || llmCall < 0 || zeroNotes > llmCall {
		t.Fatalf("DailyAtDate must return on zero notes before calling the LLM")
	}
}

func TestManualDailyStillUsesCurrentDay(t *testing.T) {
	content, err := os.ReadFile("service.go")
	if err != nil {
		t.Fatalf("read service.go: %v", err)
	}
	serviceSource := string(content)
	dailyStart := strings.Index(serviceSource, "func (s *Service) Daily(ctx")
	dailyAtStart := strings.Index(serviceSource, "func (s *Service) DailyAt(ctx")
	if dailyStart < 0 || dailyAtStart < 0 || dailyAtStart <= dailyStart {
		t.Fatalf("could not locate Daily function body")
	}
	dailyBody := serviceSource[dailyStart:dailyAtStart]
	if !strings.Contains(dailyBody, "time.Now()") || !strings.Contains(dailyBody, "s.DailyAt(ctx, userID, time.Now(), refresh)") {
		t.Fatalf("manual /daily must continue using the current day")
	}
}
