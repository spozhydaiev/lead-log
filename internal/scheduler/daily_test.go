package scheduler

import (
	"context"
	"testing"
	"time"
)

func TestNextRunUsesConfiguredLocation(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		t.Fatal(err)
	}
	runAt := 18 * time.Hour

	now := time.Date(2026, 7, 10, 15, 59, 0, 0, time.UTC) // 17:59 Warsaw Friday.
	got := nextRun(now, runAt, loc, SummaryModeCurrentDay)
	want := time.Date(2026, 7, 10, 18, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("nextRun before schedule = %s, want %s", got, want)
	}

	now = time.Date(2026, 7, 10, 16, 1, 0, 0, time.UTC) // 18:01 Warsaw Friday.
	got = nextRun(now, runAt, loc, SummaryModeCurrentDay)
	want = time.Date(2026, 7, 11, 18, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("nextRun after schedule = %s, want %s", got, want)
	}
}

func TestTuesdayRunSelectsMonday(t *testing.T) {
	loc := mustWarsaw(t)
	run := time.Date(2026, 7, 14, 8, 45, 0, 0, loc) // Tuesday.
	got, weekend := sourceDateForRun(run, SummaryModePreviousWorkday, loc)
	if weekend {
		t.Fatal("Tuesday should not be skipped as weekend")
	}
	assertDate(t, got, "2026-07-13")
}

func TestMondayRunSelectsFriday(t *testing.T) {
	loc := mustWarsaw(t)
	run := time.Date(2026, 7, 13, 8, 45, 0, 0, loc) // Monday.
	got, weekend := sourceDateForRun(run, SummaryModePreviousWorkday, loc)
	if weekend {
		t.Fatal("Monday should not be skipped as weekend")
	}
	assertDate(t, got, "2026-07-10")
}

func TestSaturdayAndSundayDoNotSend(t *testing.T) {
	loc := mustWarsaw(t)
	for _, run := range []time.Time{
		time.Date(2026, 7, 11, 8, 45, 0, 0, loc),
		time.Date(2026, 7, 12, 8, 45, 0, 0, loc),
	} {
		st := &fakeStore{}
		svc := &fakeService{noteCount: 1, response: "summary"}
		sender := &fakeSender{}
		d, err := NewDailySummary(st, svc, sender, map[int64]bool{100: true}, "08:45", loc, SummaryModePreviousWorkday)
		if err != nil {
			t.Fatal(err)
		}
		d.sendForAll(context.Background(), run)
		if sender.sent != 0 {
			t.Fatalf("%s sent %d messages, want 0", run.Weekday(), sender.sent)
		}
		if svc.dailyCalls != 0 {
			t.Fatalf("%s generated %d summaries, want 0", run.Weekday(), svc.dailyCalls)
		}
	}
}

func TestZeroNotesDoNotSendTelegramMessages(t *testing.T) {
	loc := mustWarsaw(t)
	st := &fakeStore{}
	svc := &fakeService{noteCount: 0, response: "No notes today."}
	sender := &fakeSender{}
	d, err := NewDailySummary(st, svc, sender, map[int64]bool{100: true}, "08:45", loc, SummaryModePreviousWorkday)
	if err != nil {
		t.Fatal(err)
	}
	d.sendForAll(context.Background(), time.Date(2026, 7, 14, 8, 45, 0, 0, loc))
	if sender.sent != 0 {
		t.Fatalf("sent %d messages, want 0", sender.sent)
	}
	if st.recorded != 0 {
		t.Fatalf("recorded %d sends, want 0", st.recorded)
	}
}

func TestRecapIsNotSentTwiceAfterRestart(t *testing.T) {
	loc := mustWarsaw(t)
	st := &fakeStore{sent: map[string]bool{}}
	svc := &fakeService{noteCount: 1, response: "summary"}
	sender := &fakeSender{}
	d, err := NewDailySummary(st, svc, sender, map[int64]bool{100: true}, "08:45", loc, SummaryModePreviousWorkday)
	if err != nil {
		t.Fatal(err)
	}
	run := time.Date(2026, 7, 14, 8, 45, 0, 0, loc)
	d.sendForAll(context.Background(), run)
	d.sendForAll(context.Background(), run)
	if sender.sent != 1 {
		t.Fatalf("sent %d messages, want 1", sender.sent)
	}
	if st.recorded != 1 {
		t.Fatalf("recorded %d sends, want 1", st.recorded)
	}
}

func mustWarsaw(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}
func assertDate(t *testing.T, got time.Time, want string) {
	t.Helper()
	if got.Format("2006-01-02") != want {
		t.Fatalf("date = %s, want %s", got.Format("2006-01-02"), want)
	}
}

type fakeStore struct {
	sent     map[string]bool
	recorded int
}

func (f *fakeStore) HasDailySummarySend(ctx context.Context, userID int64, scopeKey string) (bool, error) {
	if f.sent == nil {
		return false, nil
	}
	return f.sent[scopeKey], nil
}
func (f *fakeStore) RecordDailySummarySend(ctx context.Context, userID int64, scopeKey string) error {
	if f.sent == nil {
		f.sent = map[string]bool{}
	}
	if !f.sent[scopeKey] {
		f.recorded++
	}
	f.sent[scopeKey] = true
	return nil
}

type fakeService struct {
	noteCount  int
	response   string
	dailyCalls int
}

func (f *fakeService) EnsureUser(ctx context.Context, telegramUserID int64, username string) (int64, error) {
	return telegramUserID + 1, nil
}
func (f *fakeService) DailyAtDate(ctx context.Context, userID int64, sourceDate time.Time, refresh bool) (string, int, error) {
	f.dailyCalls++
	return f.response, f.noteCount, nil
}

type fakeSender struct{ sent int }

func (f *fakeSender) SendMessage(chatID int64, text string) error { f.sent++; return nil }
