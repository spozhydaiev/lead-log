package scheduler

import (
	"testing"
	"time"
)

func TestNextRunUsesConfiguredLocation(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		t.Fatal(err)
	}
	runAt := 18 * time.Hour

	now := time.Date(2026, 7, 10, 15, 59, 0, 0, time.UTC) // 17:59 Warsaw.
	got := nextRun(now, runAt, loc)
	want := time.Date(2026, 7, 10, 18, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("nextRun before schedule = %s, want %s", got, want)
	}

	now = time.Date(2026, 7, 10, 16, 1, 0, 0, time.UTC) // 18:01 Warsaw.
	got = nextRun(now, runAt, loc)
	want = time.Date(2026, 7, 11, 18, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("nextRun after schedule = %s, want %s", got, want)
	}
}
