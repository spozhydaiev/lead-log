package store

import (
	"os"
	"strings"
	"testing"
)

func TestNoteEnrichmentLifecycleMigrationAddsMetadata(t *testing.T) {
	content, err := os.ReadFile("../../../migrations/009_note_enrichment_lifecycle.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := strings.ToLower(string(content))
	for _, required := range []string{
		"processing_status",
		"processing_started_at",
		"processed_at",
		"processing_failed_at",
		"processing_attempts",
		"processing_error",
		"processing_model",
		"processing_prompt_version",
		"idx_notes_processing_lookup",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("note enrichment migration missing %q", required)
		}
	}
	if !strings.Contains(sql, "summary is not null") || !strings.Contains(sql, "then 'processed'") || !strings.Contains(sql, "else 'pending'") {
		t.Fatal("migration must mark summarized existing notes processed and raw notes pending")
	}
}

func TestEnrichmentPersistenceUsesNoteScopedReplaceStrategy(t *testing.T) {
	content, err := os.ReadFile("store.go")
	if err != nil {
		t.Fatalf("read store.go: %v", err)
	}
	source := strings.ToLower(string(content))
	for _, required := range []string{
		"func (s *store) claimnoteforenrichment",
		"delete from actions where user_id=$1 and note_id=$2",
		"delete from people_notes where user_id=$1 and note_id=$2",
		"processing_status='processing' and processing_started_at=$3",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("enrichment persistence/claim is missing %q", required)
		}
	}
}
