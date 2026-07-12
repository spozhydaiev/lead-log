package services

import (
	"os"

	"github.com/spozhydaiev/lead-log/internal/models"
	"strings"
	"testing"
)

func TestNowUsesReusableNoteEnrichmentFlow(t *testing.T) {
	content, err := os.ReadFile("service.go")
	if err != nil {
		t.Fatalf("read service.go: %v", err)
	}
	source := string(content)
	addStart := strings.Index(source, "func (s *Service) AddNote(")
	openStart := strings.Index(source, "func (s *Service) OpenActions")
	if addStart < 0 || openStart <= addStart {
		t.Fatal("could not locate AddNote body")
	}
	addBody := source[addStart:openStart]
	for _, required := range []string{"CreateAndClaimNoteForEnrichment", "EnrichClaimedNote", "formatParsedNote"} {
		if !strings.Contains(addBody, required) {
			t.Fatalf("AddNote must use reusable enrichment flow; missing %q", required)
		}
	}
	if strings.Contains(addBody, "ParseManagerNote(ctx, raw)") {
		t.Fatal("AddNote should not call LLM directly before raw note persistence")
	}
}

func TestEnrichmentHasRetryAndExplicitReprocessAPIs(t *testing.T) {
	content, err := os.ReadFile("service.go")
	if err != nil {
		t.Fatalf("read service.go: %v", err)
	}
	source := string(content)
	for _, required := range []string{
		"const NoteEnrichmentPromptVersion = \"v2\"",
		"func (s *Service) RetryNoteEnrichment",
		"func (s *Service) ReprocessNote",
		"ClaimNoteForEnrichment",
		"SaveNoteEnrichmentResult",
		"MarkNoteEnrichmentFailed",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("enrichment service missing %q", required)
		}
	}
}

func TestFormatParsedNoteIncludesDecisionsAndEntities(t *testing.T) {
	got := formatParsedNote(7, models.ParsedNote{
		Summary:        "Done",
		Decisions:      []models.ParsedDecision{{Text: "Use PostgreSQL-backed worker"}},
		EntityMentions: []models.EntityMention{{Type: models.EntityTypeTicket, Value: "CH-1234", DisplayValue: "CH-1234"}, {Type: models.EntityTypeService, Value: "message-processor", DisplayValue: "message-processor"}},
	}, models.LanguageEnglish)
	for _, want := range []string{"Decisions:", "- Use PostgreSQL-backed worker", "Entities:", "- ticket: CH-1234", "- service: message-processor"} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatted note missing %q in %s", want, got)
		}
	}
	got = formatParsedNote(8, models.ParsedNote{Summary: "Done"}, models.LanguageUkrainian)
	if strings.Contains(got, "Рішення:") || strings.Contains(got, "Сутності:") {
		t.Fatalf("empty sections should be hidden: %s", got)
	}
}
