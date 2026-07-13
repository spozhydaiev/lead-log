package services

import (
	"strings"
	"testing"
	"time"

	"github.com/spozhydaiev/lead-log/internal/models"
)

func TestFormatTicketContextNoStatusAndUTF8(t *testing.T) {
	d := time.Date(2026, 7, 12, 9, 0, 0, 0, time.UTC)
	ctx := models.TicketContext{TicketKey: "CH-1234", FirstMentionAt: &d, LastMentionAt: &d, KnownStatus: "not recorded", Mentions: []models.TicketMention{{SourceNoteID: 184, Date: d, Snippet: "CH-1234 передали на перевірку QA."}}, Actions: []models.TicketAction{{ID: 31, Title: "Check CH-1234 QA feedback", Status: "open", SourceNoteID: 184, Date: d, AssociationType: models.TicketAssociationDirect}}, Decisions: []models.TicketDecision{{ID: 7, Text: "Keep the current retry policy", Status: "active", SourceNoteID: 184, Date: d, AssociationType: models.TicketAssociationPossible}}, Sources: []models.TicketSource{{NoteID: 184, Date: d}}}
	got := formatTicketContext(ctx, models.LanguageEnglish)
	for _, want := range []string{"CH-1234", "Known status: not recorded", "CH-1234 передали", "directly related", "possibly related", "note #184"} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatted ticket missing %q\n%s", want, got)
		}
	}
	if strings.Contains(got, "completed") {
		t.Fatalf("formatter inferred a ticket status unexpectedly:\n%s", got)
	}
}

func TestFormatTicketContextNoResults(t *testing.T) {
	got := formatTicketContext(models.TicketContext{TicketKey: "CH-1234"}, models.LanguageEnglish)
	if !strings.Contains(got, "I could not find CH-1234") {
		t.Fatalf("no-result response=%q", got)
	}
}

func TestTicketAssociationType(t *testing.T) {
	if associationType("Check CH-1234 QA", "CH-1234") != models.TicketAssociationDirect {
		t.Fatal("direct key was not direct")
	}
	if associationType("Check CH-12345 QA", "CH-1234") != models.TicketAssociationPossible {
		t.Fatal("unsafe boundary matched as direct")
	}
}
