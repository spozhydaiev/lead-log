package llm

import "testing"

func TestParseAgendaJSONFiltersEmptyItems(t *testing.T) {
	content := `{
		"discussion_topics":[{"title":" Доставка ","context":" уточнити ризики ","source_note_ids":[1]},{"title":" ","context":" "}],
		"open_followups":[{"text":" Перевірити action ","source_note_ids":[2]},{"text":" "}],
		"positive_signals":[],
		"risks_or_concerns_to_clarify":[],
		"growth_topics":[],
		"suggested_questions":[]
	}`
	agenda, err := ParseAgendaJSON(content)
	if err != nil {
		t.Fatalf("ParseAgendaJSON() error = %v", err)
	}
	if len(agenda.DiscussionTopics) != 1 || agenda.DiscussionTopics[0].Title != "Доставка" {
		t.Fatalf("unexpected discussion topics: %#v", agenda.DiscussionTopics)
	}
	if len(agenda.OpenFollowups) != 1 || agenda.OpenFollowups[0].Text != "Перевірити action" {
		t.Fatalf("unexpected open followups: %#v", agenda.OpenFollowups)
	}
}
