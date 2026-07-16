package scheduler

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/spozhydaiev/lead-log/internal/logging"
	"sort"
	"time"
)

type Sender interface {
	SendMessage(chatID int64, text string) error
}

type Store interface {
	HasDailySummarySend(ctx context.Context, userID int64, scopeKey string) (bool, error)
	RecordDailySummarySend(ctx context.Context, userID int64, scopeKey string) error
}

type Service interface {
	EnsureUser(ctx context.Context, telegramUserID int64, username string) (int64, error)
	DailyAtDate(ctx context.Context, userID int64, sourceDate time.Time, refresh bool) (string, int, error)
}

type SummaryMode string

const (
	SummaryModePreviousWorkday SummaryMode = "previous_workday"
	SummaryModeCurrentDay      SummaryMode = "current_day"
)

type DailySummary struct {
	store           Store
	service         Service
	sender          Sender
	telegramUserIDs []int64
	runAt           time.Duration
	location        *time.Location
	logger          *slog.Logger
	mode            SummaryMode
}

func NewDailySummary(st Store, service Service, sender Sender, allowedUsers map[int64]bool, dailyTime string, loc *time.Location, mode SummaryMode, logger ...*slog.Logger) (*DailySummary, error) {
	parsed, err := time.Parse("15:04", dailyTime)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(allowedUsers))
	for id := range allowedUsers {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if loc == nil {
		loc = time.Local
	}
	if mode == "" {
		mode = SummaryModePreviousWorkday
	}
	if mode != SummaryModePreviousWorkday && mode != SummaryModeCurrentDay {
		return nil, fmt.Errorf("unsupported daily summary mode: %s", mode)
	}
	l := slog.Default()
	if len(logger) > 0 && logger[0] != nil {
		l = logger[0]
	}
	return &DailySummary{store: st, service: service, sender: sender, telegramUserIDs: ids, runAt: time.Duration(parsed.Hour())*time.Hour + time.Duration(parsed.Minute())*time.Minute, location: loc, logger: l, mode: mode}, nil
}

func (d *DailySummary) Run(ctx context.Context) {
	d.logger.Info("scheduler started", "operation", "scheduler.run", "daily_summary_time", formatRunAt(d.runAt), "timezone", d.location.String(), "mode", string(d.mode))
	for {
		now := time.Now().In(d.location)
		next := nextRun(now, d.runAt, d.location, d.mode)
		d.logger.Info("next scheduled run", "operation", "scheduler.next_run", "next_run", next.Format(time.RFC3339), "timezone", d.location.String())
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			d.logger.Info("scheduler stopped", "operation", "scheduler.run")
			return
		case <-timer.C:
			d.logger.Info("scheduled run triggered", "operation", "scheduler.trigger", "scheduled_for", next.Format(time.RFC3339))
			d.sendForAll(ctx, next)
		}
	}
}

func (d *DailySummary) sendForAll(ctx context.Context, run time.Time) {
	sourceDate, weekend := sourceDateForRun(run, d.mode, d.location)
	if weekend {
		d.logger.Info("summary skipped weekend", "operation", "scheduler.skip", "run_date", run.In(d.location).Format("2006-01-02"), "timezone", d.location.String())
		return
	}
	scopeKey := sourceDate.Format("2006-01-02")
	d.logger.Info("selected source date", "operation", "scheduler.source_date", "source_date", scopeKey, "timezone", d.location.String(), "mode", string(d.mode))
	for _, telegramUserID := range d.telegramUserIDs {
		operationCtx, operationID := logging.EnsureOperationID(ctx)
		if operationCtx.Err() != nil {
			return
		}
		userID, err := d.service.EnsureUser(operationCtx, telegramUserID, "")
		if err != nil {
			d.logger.Error("daily summary failure", logging.WithSafeError([]any{"operation", "scheduler.ensure_user", "failure_stage", "ensure_user", "operation_id", operationID}, err)...)
			continue
		}
		sent, err := d.store.HasDailySummarySend(operationCtx, userID, scopeKey)
		if err != nil {
			d.logger.Error("daily summary failure", logging.WithSafeError([]any{"operation", "scheduler.sent_check", "failure_stage", "sent_check", "operation_id", operationID}, err)...)
			continue
		}
		if sent {
			d.logger.Info("summary skipped because already sent", "operation", "scheduler.skip", "operation_id", operationID)
			continue
		}
		response, noteCount, err := d.service.DailyAtDate(operationCtx, userID, sourceDate, false)
		d.logger.Info("daily source note count", "operation", "scheduler.note_count", "operation_id", operationID, "note_count", noteCount)
		if err != nil {
			d.logger.Error("daily summary failure", logging.WithSafeError([]any{"operation", "scheduler.generate", "failure_stage", "generate", "operation_id", operationID}, err)...)
			continue
		}
		if noteCount == 0 {
			d.logger.Info("summary skipped because no notes", "operation", "scheduler.skip", "operation_id", operationID, "note_count", noteCount)
			continue
		}
		if err := d.sender.SendMessage(telegramUserID, response); err != nil {
			d.logger.Error("daily summary failure", logging.WithSafeError([]any{"operation", "scheduler.send", "failure_stage", "send", "operation_id", operationID}, err)...)
			continue
		}
		if err := d.store.RecordDailySummarySend(operationCtx, userID, scopeKey); err != nil {
			d.logger.Error("daily summary failure", logging.WithSafeError([]any{"operation", "scheduler.record_send", "failure_stage", "record_send", "operation_id", operationID}, err)...)
			continue
		}
		d.logger.Info("summary sent", "operation", "scheduler.send", "operation_id", operationID)
	}
}

func nextRun(now time.Time, runAt time.Duration, loc *time.Location, mode SummaryMode) time.Time {
	local := now.In(loc)
	next := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc).Add(runAt)
	if !next.After(local) {
		next = next.AddDate(0, 0, 1)
	}
	if mode == SummaryModePreviousWorkday {
		for isWeekend(next) {
			next = next.AddDate(0, 0, 1)
		}
	}
	return next
}

func sourceDateForRun(run time.Time, mode SummaryMode, loc *time.Location) (time.Time, bool) {
	local := run.In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	if mode == SummaryModeCurrentDay {
		return day, false
	}
	if isWeekend(day) {
		return day, true
	}
	if day.Weekday() == time.Monday {
		return day.AddDate(0, 0, -3), false
	}
	return day.AddDate(0, 0, -1), false
}

func isWeekend(t time.Time) bool { return t.Weekday() == time.Saturday || t.Weekday() == time.Sunday }
func formatRunAt(d time.Duration) string {
	return time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC).Add(d).Format("15:04")
}
