package periods

import (
	"fmt"
	"time"
)

// Week is a canonical Lead Log Monday-to-Sunday calendar week.
// Start and ExclusiveEnd are local midnights in Location; EndDate is the
// inclusive Sunday date represented as local midnight for date formatting.
type Week struct {
	Start        time.Time
	EndDate      time.Time
	ExclusiveEnd time.Time
	ScopeKey     string
}

// ResolveContainingWeek returns the Monday-to-Sunday Lead Log week containing anchor.
func ResolveContainingWeek(anchor time.Time, loc *time.Location) Week {
	if loc == nil {
		loc = time.Local
	}
	local := anchor.In(loc)
	localMidnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	daysSinceMonday := (int(localMidnight.Weekday()) + 6) % 7
	weekStart := localMidnight.AddDate(0, 0, -daysSinceMonday)
	nextWeekStart := weekStart.AddDate(0, 0, 7)
	weekEndDate := weekStart.AddDate(0, 0, 6)
	y, w := weekStart.ISOWeek()
	return Week{Start: weekStart, EndDate: weekEndDate, ExclusiveEnd: nextWeekStart, ScopeKey: fmt.Sprintf("%d-W%02d", y, w)}
}

// ResolvePreviousCompletedWeek returns the last full Monday-to-Sunday week before now.
func ResolvePreviousCompletedWeek(now time.Time, loc *time.Location) Week {
	if loc == nil {
		loc = time.Local
	}
	current := ResolveContainingWeek(now, loc)
	return ResolveContainingWeek(current.Start.AddDate(0, 0, -1), loc)
}
