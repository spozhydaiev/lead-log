package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/spozhydaiev/lead-log/internal/services"
)

func TestNextWeeklyRunMondayMorning(t *testing.T) {
	loc := mustWarsaw(t)
	got := nextWeeklyRun(time.Date(2026, 7, 19, 10, 0, 0, 0, loc), 8*time.Hour+45*time.Minute, loc)
	want := time.Date(2026, 7, 20, 8, 45, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestPreviousCompletedWeekForMondayRun(t *testing.T) {
	loc := mustWarsaw(t)
	got := previousCompletedWeek(time.Date(2026, 7, 20, 8, 45, 0, 0, loc), loc)
	assertDate(t, got.Start, "2026-07-13")
	assertDate(t, got.EndDate, "2026-07-19")
}

func TestWeeklySchedulerGeneratesOnceAndTelegramFailureDoesNotUndo(t *testing.T) {
	loc := mustWarsaw(t)
	svc := &fakeWeeklyService{resp: services.SummaryGenerateResult{Generated: true, Period: services.SummaryPeriod{From: "2026-07-13", To: "2026-07-19"}, Summary: &services.SummaryView{Title: "Weekly summary", Preview: "Readable"}}}
	sender := &failingSender{}
	w, err := NewWeeklySummary(svc, sender, map[int64]bool{100: true}, "08:45", loc)
	if err != nil {
		t.Fatal(err)
	}
	run := time.Date(2026, 7, 20, 8, 45, 0, 0, loc)
	w.sendForAll(context.Background(), run)
	svc.resp.CacheHit = true
	w.sendForAll(context.Background(), run)
	if svc.calls != 2 {
		t.Fatalf("calls=%d", svc.calls)
	}
	if sender.sent != 1 {
		t.Fatalf("send attempts=%d", sender.sent)
	}
	if svc.lastAnchor.Format("2006-01-02") != "2026-07-13" {
		t.Fatalf("anchor=%s", svc.lastAnchor.Format("2006-01-02"))
	}
}

type fakeWeeklyService struct {
	calls      int
	lastAnchor time.Time
	resp       services.SummaryGenerateResult
}

func (f *fakeWeeklyService) EnsureUser(ctx context.Context, telegramUserID int64, username string) (int64, error) {
	return telegramUserID + 1, nil
}
func (f *fakeWeeklyService) GenerateSummary(ctx context.Context, userID int64, in services.SummaryGenerateInput) (services.SummaryGenerateResult, error) {
	f.calls++
	f.lastAnchor = in.AnchorDate
	return f.resp, nil
}

type failingSender struct{ sent int }

func (f *failingSender) SendMessage(chatID int64, text string) error {
	f.sent++
	return context.Canceled
}
