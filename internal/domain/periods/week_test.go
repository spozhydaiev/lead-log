package periods

import (
	"testing"
	"time"
)

func TestResolveContainingWeekEveryDay(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Warsaw")
	for day := 13; day <= 19; day++ {
		got := ResolveContainingWeek(time.Date(2026, 7, day, 12, 0, 0, 0, loc), loc)
		assertWeek(t, got, "2026-07-13", "2026-07-19", "2026-07-20")
	}
	got := ResolveContainingWeek(time.Date(2026, 7, 20, 12, 0, 0, 0, loc), loc)
	assertWeek(t, got, "2026-07-20", "2026-07-26", "2026-07-27")
}

func TestResolvePreviousCompletedWeek(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Warsaw")
	got := ResolvePreviousCompletedWeek(time.Date(2026, 7, 20, 8, 45, 0, 0, loc), loc)
	assertWeek(t, got, "2026-07-13", "2026-07-19", "2026-07-20")
}

func TestResolveContainingWeekTimezonesAndDST(t *testing.T) {
	for _, name := range []string{"UTC", "Europe/Warsaw"} {
		loc, _ := time.LoadLocation(name)
		got := ResolveContainingWeek(time.Date(2026, 7, 16, 12, 0, 0, 0, loc), loc)
		assertWeek(t, got, "2026-07-13", "2026-07-19", "2026-07-20")
	}
	warsaw, _ := time.LoadLocation("Europe/Warsaw")
	got := ResolveContainingWeek(time.Date(2026, 3, 29, 12, 0, 0, 0, warsaw), warsaw)
	assertWeek(t, got, "2026-03-23", "2026-03-29", "2026-03-30")
}

func assertWeek(t *testing.T, got Week, from, to, exclusive string) {
	t.Helper()
	if got.Start.Format("2006-01-02") != from || got.EndDate.Format("2006-01-02") != to || got.ExclusiveEnd.Format("2006-01-02") != exclusive {
		t.Fatalf("got %s..%s exclusive %s", got.Start.Format("2006-01-02"), got.EndDate.Format("2006-01-02"), got.ExclusiveEnd.Format("2006-01-02"))
	}
	if got.EndDate != got.Start.AddDate(0, 0, 6) {
		t.Fatalf("inclusive end is not six calendar days after start")
	}
	if got.ExclusiveEnd != got.Start.AddDate(0, 0, 7) {
		t.Fatalf("exclusive end is not seven calendar days after start")
	}
}
