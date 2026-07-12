package llm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spozhydaiev/lead-log/internal/models"
)

func TestParsedNoteExtendedSchemaParsing(t *testing.T) {
	raw := `{"summary":"s","tags":["x"],"actions":[{"title":"Follow up","linked_person_name":"Ann","output_type":"message"}],"people_notes":[{"person_name":"Ann","type":"context","theme":"delivery","text":"Ann waits for QA","include_in_review":true}],"decisions":[{"text":"Use PostgreSQL-backed worker","linked_person_name":"CTO","topic":"architecture"}],"entity_mentions":[{"type":"ticket","value":"ch-1234","context":"QA"},{"type":"service","value":"message-processor"}],"suggested_questions":["q?"]}`
	var p models.ParsedNote
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	if p.Summary != "s" || len(p.Actions) != 1 || len(p.PeopleNotes) != 1 || len(p.Decisions) != 1 || len(p.EntityMentions) != 2 {
		t.Fatalf("unexpected parsed note: %#v", p)
	}
}

func TestSystemPromptMentionsDecisionsEntitiesAndTicketRules(t *testing.T) {
	p := systemPrompt(models.LanguageEnglish)
	for _, want := range []string{"\"decisions\"", "\"entity_mentions\"", "ticket|project|service|component|repository|document|other", "Do not invent decisions", "never invent IDs"} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
}
