package store

import (
	"strings"
	"testing"
)

func TestDailyActionInsertSQLCastsLinkedPersonParameter(t *testing.T) {
	if !strings.Contains(dailyActionInsertSQL, "$2::bigint") {
		t.Fatalf("dailyActionInsertSQL must cast $2 as bigint everywhere it is used for linked_person_id: %s", dailyActionInsertSQL)
	}

	if strings.Contains(dailyActionInsertSQL, "COALESCE($2, 0)") {
		t.Fatalf("dailyActionInsertSQL must not compare untyped $2 with integer literal 0: %s", dailyActionInsertSQL)
	}

	if !strings.Contains(dailyActionInsertSQL, "COALESCE($2::bigint, 0::bigint)") {
		t.Fatalf("dailyActionInsertSQL must cast $2 and the fallback literal in the idempotency check: %s", dailyActionInsertSQL)
	}
}
