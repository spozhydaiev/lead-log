package services

import (
	"os"
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
		"const NoteEnrichmentPromptVersion = \"v1\"",
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
