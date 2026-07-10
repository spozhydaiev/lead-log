package llm

import (
	"context"
	"github.com/spozhydaiev/lead-log/internal/models"
)

type MockClientLLM struct {
}

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

func (m *MockClientLLM) SummarizeWeekly(ctx context.Context, input string) (string, error) {
	return input, nil
}
