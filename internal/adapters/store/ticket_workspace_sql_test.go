package store

import (
	"os"
	"strings"
	"testing"
)

func TestTicketDetailRecentNotesSQLCoalescesHistoricalNullableFields(t *testing.T) {
	b, err := os.ReadFile("store.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{"COALESCE(n.raw_text,'')", "COALESCE(n.processing_status,'pending')", "ticket_repository.list_recent_notes", "ClassDatabaseScan"} {
		if !strings.Contains(src, want) {
			t.Fatalf("ticket detail SQL/diagnostics missing %q", want)
		}
	}
}
