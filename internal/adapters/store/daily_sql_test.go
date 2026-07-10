package store

import (
	"strings"
	"testing"
)

func TestDailyStructuredInsertSQLUsesIdempotencyConflict(t *testing.T) {
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
		if !strings.Contains(sql, "ON CONFLICT (idempotency_key)") || !strings.Contains(sql, "DO NOTHING") {
			t.Fatalf("%s daily insert must be idempotent via ON CONFLICT DO NOTHING: %s", name, sql)
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
