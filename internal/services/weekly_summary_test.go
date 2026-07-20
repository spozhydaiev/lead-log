package services

import (
	"testing"
	"time"

	"github.com/spozhydaiev/lead-log/internal/models"
)

func TestSummaryPeriodWeeklyCanonicalMondaySunday(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Warsaw")
	s := &Service{dailyLocation: loc}
	cases := []struct {
		name            string
		anchor          time.Time
		from, to, scope string
	}{
		{"monday", time.Date(2026, 7, 20, 12, 0, 0, 0, loc), "2026-07-20", "2026-07-26", "2026-W30"},
		{"sunday", time.Date(2026, 7, 19, 12, 0, 0, 0, loc), "2026-07-13", "2026-07-19", "2026-W29"},
		{"midweek", time.Date(2026, 7, 16, 12, 0, 0, 0, loc), "2026-07-13", "2026-07-19", "2026-W29"},
		{"dst", time.Date(2026, 3, 29, 12, 0, 0, 0, loc), "2026-03-23", "2026-03-29", "2026-W13"},
	}
	for _, tc := range cases {
		start, end, scope, err := s.summaryPeriod("weekly", tc.anchor)
		if err != nil {
			t.Fatal(err)
		}
		if got := start.Format("2006-01-02"); got != tc.from {
			t.Fatalf("%s from=%s want %s", tc.name, got, tc.from)
		}
		if got := end.Add(-time.Nanosecond).Format("2006-01-02"); got != tc.to {
			t.Fatalf("%s to=%s want %s", tc.name, got, tc.to)
		}
		if scope != tc.scope {
			t.Fatalf("%s scope=%s want %s", tc.name, scope, tc.scope)
		}
		if days := int(end.Sub(start).Hours() / 24); days < 6 || days > 7 {
			t.Fatalf("%s duration=%v", tc.name, end.Sub(start))
		}
	}
}

func TestWeeklyLegacyJSONTextNormalizesContentAndPreview(t *testing.T) {
	r := models.AgentResponse{Kind: "weekly", ResponseText: `{"summary":"Readable weekly summary","highlights":null,"actions":null,"decisions":null,"people":null,"tickets":null,"risks":null,"open_questions":null,"repeated_topics":null}`}
	if got := summaryPreview(r); got != "Readable weekly summary" {
		t.Fatalf("preview=%q", got)
	}
	content := summaryContent(r)
	d, ok := content.(models.WeeklyDigest)
	if !ok {
		t.Fatalf("content type %T", content)
	}
	if d.Summary != "Readable weekly summary" || d.Highlights == nil || d.Actions == nil {
		t.Fatalf("bad digest: %#v", d)
	}
}

func TestWeeklyOrdinaryLegacyTextRemainsText(t *testing.T) {
	r := models.AgentResponse{Kind: "weekly", ResponseText: "Plain legacy summary"}
	content := summaryContent(r)
	m, ok := content.(map[string]any)
	if !ok || m["text"] != "Plain legacy summary" {
		t.Fatalf("content=%#v", content)
	}
}

func TestSummaryPeriodDTOWarsawSummerUsesLocalCalendarDates(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Warsaw")
	start := time.Date(2026, 7, 12, 22, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 19, 22, 0, 0, 0, time.UTC)
	got := summaryPeriodDTO(start, end, loc)
	if got.From != "2026-07-13" || got.To != "2026-07-19" {
		t.Fatalf("period=%#v want 2026-07-13..2026-07-19", got)
	}
}

func TestMapSummaryWeeklyTitleUsesLocalCalendarDates(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Warsaw")
	start := time.Date(2026, 7, 12, 22, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 19, 22, 0, 0, 0, time.UTC)
	r := models.AgentResponse{ID: 9, Kind: "weekly", PeriodStart: &start, PeriodEnd: &end, CreatedAt: time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)}
	v := (&Service{}).mapSummary(t.Context(), 7, r, false, loc)
	if v.Period.From != "2026-07-13" || v.Period.To != "2026-07-19" {
		t.Fatalf("period=%#v", v.Period)
	}
	want := "Weekly summary for 2026-07-13 to 2026-07-19"
	if v.Title != want {
		t.Fatalf("title=%q want %q", v.Title, want)
	}
}

func TestSummaryPeriodDTOTimezonesAndDST(t *testing.T) {
	cases := []struct {
		name       string
		tz         string
		start, end time.Time
		from, to   string
	}{
		{"warsaw winter", "Europe/Warsaw", time.Date(2026, 1, 4, 23, 0, 0, 0, time.UTC), time.Date(2026, 1, 11, 23, 0, 0, 0, time.UTC), "2026-01-05", "2026-01-11"},
		{"warsaw dst transition", "Europe/Warsaw", time.Date(2026, 3, 22, 23, 0, 0, 0, time.UTC), time.Date(2026, 3, 29, 22, 0, 0, 0, time.UTC), "2026-03-23", "2026-03-29"},
		{"utc", "UTC", time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), "2026-07-13", "2026-07-19"},
		{"west of utc", "America/New_York", time.Date(2026, 7, 13, 4, 0, 0, 0, time.UTC), time.Date(2026, 7, 20, 4, 0, 0, 0, time.UTC), "2026-07-13", "2026-07-19"},
	}
	for _, tc := range cases {
		loc, err := time.LoadLocation(tc.tz)
		if err != nil {
			t.Fatal(err)
		}
		got := summaryPeriodDTO(tc.start, tc.end, loc)
		if got.From != tc.from || got.To != tc.to {
			t.Fatalf("%s period=%#v want %s..%s", tc.name, got, tc.from, tc.to)
		}
	}
}

func TestSummaryLocationInvalidTimezoneFallsBack(t *testing.T) {
	fallback, _ := time.LoadLocation("Europe/Warsaw")
	got := (&Service{dailyLocation: fallback}).summaryLocation("not/a-zone")
	if got.String() != "Europe/Warsaw" {
		t.Fatalf("location=%s", got)
	}
}
