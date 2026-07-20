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
