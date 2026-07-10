package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDailyStructuredInsertSQLPreventsDuplicatesBeforeInsert(t *testing.T) {
	for name, sql := range map[string]string{
		"action":      dailyActionInsertSQL,
		"people_note": dailyPeopleNoteInsertSQL,
	} {
		if !strings.Contains(sql, "idempotency_key") {
			t.Fatalf("%s daily insert must persist an idempotency_key: %s", name, sql)
		}
		if !strings.Contains(sql, "source_note_ids") {
			t.Fatalf("%s daily insert must persist source_note_ids: %s", name, sql)
		}
		if !strings.Contains(sql, "ON CONFLICT (user_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING") {
			t.Fatalf("%s daily insert must be idempotent via scoped ON CONFLICT DO NOTHING: %s", name, sql)
		}
		if strings.Contains(strings.ToUpper(sql), "DELETE") {
			t.Fatalf("%s daily insert must not rely on delete cleanup: %s", name, sql)
		}
	}
}

func TestDailyItemIdempotencyKeyUsesSourceNotesBeforeText(t *testing.T) {
	first := dailyItemIdempotencyKey(1, "action", "2026-07-10", []int64{2, 1}, "Олена", "reminder", "Уточнити ETA")
	changedText := dailyItemIdempotencyKey(1, "action", "2026-07-10", []int64{1, 2}, "олена", "reminder", "Уточнити ETA завтра")
	if first != changedText {
		t.Fatalf("source-backed action key should ignore slight text changes when source notes and person match: %q != %q", first, changedText)
	}

	withoutSource := dailyItemIdempotencyKey(1, "action", "2026-07-10", nil, "Олена", "reminder", "  Уточнити   ETA!!! ")
	withoutSourceNormalized := dailyItemIdempotencyKey(1, "action", "2026-07-10", nil, "Олена", "reminder", "уточнити eta")
	if withoutSource != withoutSourceNormalized {
		t.Fatalf("fallback key should normalize item text: %q != %q", withoutSource, withoutSourceNormalized)
	}
}

func TestRepeatedDailyAndRefreshReuseSameItemKeys(t *testing.T) {
	actionFirst := dailyItemIdempotencyKey(42, "action", "2026-07-10", []int64{11, 10}, "Олена", "reminder", "Уточнити ETA")
	actionAgain := dailyItemIdempotencyKey(42, "action", "2026-07-10", []int64{10, 11}, "олена", "reminder", "Уточнити ETA")
	if actionFirst != actionAgain {
		t.Fatalf("repeated /daily and /daily --refresh should target the same action row")
	}

	peopleNoteFirst := dailyItemIdempotencyKey(42, "people_note", "2026-07-10", []int64{10}, "Іван", "", "Needs quieter rollout context")
	peopleNoteAgain := dailyItemIdempotencyKey(42, "people_note", "2026-07-10", []int64{10}, "іван", "", "Needs quieter rollout context")
	if peopleNoteFirst != peopleNoteAgain {
		t.Fatalf("repeated /daily and /daily --refresh should target the same people_note row")
	}
}

func TestDoneActionCannotBeRecreatedOpenByRefreshSQL(t *testing.T) {
	if !strings.Contains(dailyActionInsertSQL, "ON CONFLICT (user_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING") {
		t.Fatalf("daily action insert must do nothing on existing idempotency key so done rows are not recreated as open: %s", dailyActionInsertSQL)
	}
	if strings.Contains(dailyActionInsertSQL, "status") || strings.Contains(strings.ToUpper(dailyActionInsertSQL), "DO UPDATE") {
		t.Fatalf("daily action refresh must not reset an existing done action to open: %s", dailyActionInsertSQL)
	}
}

func TestMigrationsCreateUserScopedDailyIdempotencyIndexes(t *testing.T) {
	for _, path := range []string{"migrations/001_init.sql", "migrations/006_scope_daily_idempotency_by_user.sql"} {
		content, err := os.ReadFile(filepath.Join("..", "..", "..", path))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		sql := strings.ReplaceAll(string(content), " ", "")
		if !strings.Contains(sql, "ONactions(user_id,idempotency_key)") || !strings.Contains(sql, "WHEREidempotency_keyISNOTNULL") {
			t.Fatalf("%s must create user-scoped partial unique index for actions", path)
		}
		if !strings.Contains(sql, "ONpeople_notes(user_id,idempotency_key)") || !strings.Contains(sql, "WHEREidempotency_keyISNOTNULL") {
			t.Fatalf("%s must create user-scoped partial unique index for people_notes", path)
		}
	}
}
