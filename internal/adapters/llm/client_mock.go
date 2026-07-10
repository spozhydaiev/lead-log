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

func (m *MockClientLLM) GenerateTicket(ctx context.Context, input string) (models.TicketDraft, error) {
	return models.TicketDraft{}, nil
}

func (m *MockClientLLM) ProcessDaily(ctx context.Context, input string) (models.DailyProcessingResult, error) {
	parsed, err := m.ParseManagerNote(ctx, input)
	if err != nil {
		return models.DailyProcessingResult{}, err
	}
	return models.DailyProcessingResult{SummaryText: input, Structured: parsed}, nil
}

func (m *MockClientLLM) SummarizeWeekly(ctx context.Context, input string) (string, error) {
	return input, nil
}

func (m *MockClientLLM) SummarizePerson(ctx context.Context, input string) (string, error) {
	return input, nil
}

func (m *MockClientLLM) Model() string {
	return "mock"
}
