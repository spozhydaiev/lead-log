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

func (m *MockClientLLM) Model() string {
	return "mock"
}
