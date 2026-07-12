package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDailyStructuredPersistenceCodeRemoved(t *testing.T) {
	content, err := os.ReadFile("store.go")
	if err != nil {
		t.Fatalf("read store.go: %v", err)
	}
	for _, forbidden := range []string{"PersistDailyStructured", "dailyActionInsertSQL", "dailyPeopleNoteInsertSQL", "dailyItemIdempotencyKey"} {
		if strings.Contains(string(content), forbidden) {
			t.Fatalf("daily-created actions/people_notes persistence code should be removed, found %q", forbidden)
		}
	}
}

func TestMigrationsStillPreserveExistingIdempotencyColumnsAndIndexes(t *testing.T) {
	for _, path := range []string{"migrations/001_init.sql", "migrations/006_scope_daily_idempotency_by_user.sql"} {
		content, err := os.ReadFile(filepath.Join("..", "..", "..", path))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		sql := strings.ReplaceAll(string(content), " ", "")
		if !strings.Contains(sql, "ONactions(user_id,idempotency_key)") || !strings.Contains(sql, "WHEREidempotency_keyISNOTNULL") {
			t.Fatalf("%s must keep user-scoped partial unique index for existing actions", path)
		}
		if !strings.Contains(sql, "ONpeople_notes(user_id,idempotency_key)") || !strings.Contains(sql, "WHEREidempotency_keyISNOTNULL") {
			t.Fatalf("%s must keep user-scoped partial unique index for existing people_notes", path)
		}
	}
}
