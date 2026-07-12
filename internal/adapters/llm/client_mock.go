package llm

import (
	"context"

	"github.com/spozhydaiev/lead-log/internal/models"
)

type MockClientLLM struct{}

func NewMockClientLLM() *MockClientLLM {
	return &MockClientLLM{}
}

func (m *MockClientLLM) ParseManagerNote(ctx context.Context, raw string) (models.ParsedNote, error) {
	return models.ParsedNote{
		Summary: "summary",
		Tags:    []string{"tag1", "tag2"},
		Actions: []models.ParsedAction{
			{
				Title:            "Title",
				LinkedPersonName: "None",
				OutputType:       "output type",
			},
		},
		PeopleNotes: []models.ParsedPeopleNote{
			{
				PersonName:      "Person name",
				Type:            "engineer",
				Theme:           "hello",
				Text:            "",
				IncludeInReview: false,
			},
		},
		TicketDrafts:       nil,
		SuggestedQuestions: nil,
	}, nil
}

func (m *MockClientLLM) ProcessDaily(ctx context.Context, input string) (models.DailyDigest, error) {
	owner := "Person name"
	return models.DailyDigest{
		ShortSummary:     "Короткий підсумок дня.",
		OpenLoops:        []models.DailyOpenLoop{{Title: "Title", Owner: &owner, SourceNoteIDs: []int64{1}}},
		PeopleHighlights: []models.DailyPeopleHighlight{{PersonName: owner, Type: "context", Theme: "other", Text: "Нотатка про контекст.", SourceNoteIDs: []int64{1}}},
	}, nil
}

func (m *MockClientLLM) SummarizeWeekly(ctx context.Context, input string) (string, error) {
	return input, nil
}

func (m *MockClientLLM) PlanAskQuery(ctx context.Context, question, currentDate, timezone, language string) (models.AskIntent, error) {
	return models.AskIntent{IntentType: models.AskIntentGeneralContext, TextQuery: question, DateRange: models.AskDateRange{Type: models.AskDateUnspecified}, Limit: 20}, nil
}

func (m *MockClientLLM) GenerateAskAnswer(ctx context.Context, question string, intent models.AskIntent, candidates []models.AskCandidate, language string) (models.AskAnswer, error) {
	ids := []int64{}
	dates := []string{}
	if len(candidates) > 0 {
		ids = append(ids, candidates[0].SourceNoteID)
		dates = append(dates, candidates[0].Date)
	}
	return models.AskAnswer{Answer: "Based on your notes: " + question, Items: []models.AskAnswerItem{{Text: "See source", SourceNoteIDs: ids, SourceDates: dates, Confidence: "confirmed"}}}, nil
}

func (m *MockClientLLM) Model() string {
	return "mock"
}
