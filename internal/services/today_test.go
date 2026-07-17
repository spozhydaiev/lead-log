package services

import (
	"testing"
	"time"
)

func TestLocalDayBoundariesDST(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		now   time.Time
		hours float64
	}{{time.Date(2026, 3, 29, 12, 0, 0, 0, loc), 23}, {time.Date(2026, 10, 25, 12, 0, 0, 0, loc), 25}} {
		local := tc.now.In(loc)
		start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
		end := start.AddDate(0, 0, 1)
		if end.Sub(start).Hours() != tc.hours {
			t.Fatalf("day length=%v", end.Sub(start))
		}
		if !time.Date(local.Year(), local.Month(), local.Day(), 0, 1, 0, 0, loc).Before(end) {
			t.Fatal("00:01 excluded")
		}
		if !time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc).Add(-time.Minute).Before(start) {
			t.Fatal("previous 23:59 not before start")
		}
	}
}
