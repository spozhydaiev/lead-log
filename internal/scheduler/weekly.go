package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/spozhydaiev/lead-log/internal/domain/periods"
	"github.com/spozhydaiev/lead-log/internal/logging"
	"github.com/spozhydaiev/lead-log/internal/services"
)

type WeeklyService interface {
	EnsureUser(ctx context.Context, telegramUserID int64, username string) (int64, error)
	GenerateSummary(ctx context.Context, userID int64, in services.SummaryGenerateInput) (services.SummaryGenerateResult, error)
}

type WeeklySummary struct {
	service         WeeklyService
	sender          Sender
	telegramUserIDs []int64
	runAt           time.Duration
	location        *time.Location
	logger          *slog.Logger
}

func NewWeeklySummary(service WeeklyService, sender Sender, allowedUsers map[int64]bool, weeklyTime string, loc *time.Location, logger ...*slog.Logger) (*WeeklySummary, error) {
	parsed, err := time.Parse("15:04", weeklyTime)
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
	l := slog.Default()
	if len(logger) > 0 && logger[0] != nil {
		l = logger[0]
	}
	return &WeeklySummary{service: service, sender: sender, telegramUserIDs: ids, runAt: time.Duration(parsed.Hour())*time.Hour + time.Duration(parsed.Minute())*time.Minute, location: loc, logger: l}, nil
}

func (w *WeeklySummary) Run(ctx context.Context) {
	w.logger.Info("weekly scheduler started", "operation", "scheduler.weekly.run", "weekly_summary_time", formatRunAt(w.runAt), "timezone", w.location.String())
	for {
		now := time.Now().In(w.location)
		next := nextWeeklyRun(now, w.runAt, w.location)
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			w.sendForAll(ctx, next)
		}
	}
}

func nextWeeklyRun(now time.Time, runAt time.Duration, loc *time.Location) time.Time {
	local := now.In(loc)
	base := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc).Add(runAt)
	wd := (int(base.Weekday()) + 6) % 7
	next := base.AddDate(0, 0, -wd)
	if !next.After(local) {
		next = next.AddDate(0, 0, 7)
	}
	return next
}

func previousCompletedWeek(run time.Time, loc *time.Location) periods.Week {
	return periods.ResolvePreviousCompletedWeek(run, loc)
}

func (w *WeeklySummary) sendForAll(ctx context.Context, run time.Time) {
	week := previousCompletedWeek(run, w.location)
	for _, telegramUserID := range w.telegramUserIDs {
		opCtx, opID := logging.EnsureOperationID(ctx)
		userID, err := w.service.EnsureUser(opCtx, telegramUserID, "")
		if err != nil {
			w.logger.Error("weekly summary failure", logging.WithSafeError([]any{"operation", "scheduler.weekly.ensure_user", "operation_id", opID}, err)...)
			continue
		}
		res, err := w.service.GenerateSummary(opCtx, userID, services.SummaryGenerateInput{Type: "weekly", AnchorDate: week.Start, Force: false})
		if err != nil {
			w.logger.Error("weekly summary failure", logging.WithSafeError([]any{"operation", "scheduler.weekly.generate", "operation_id", opID}, err)...)
			continue
		}
		if res.Reason == "no_source_notes" {
			w.logger.Info("weekly skipped because no notes", "operation", "scheduler.weekly.skip", "operation_id", opID)
			continue
		}
		if res.CacheHit {
			w.logger.Info("weekly skipped because already generated", "operation", "scheduler.weekly.skip", "operation_id", opID)
			continue
		}
		if w.sender != nil && res.Summary != nil {
			if err := w.sender.SendMessage(telegramUserID, fmt.Sprintf("%s\n\n%s", res.Summary.Title, res.Summary.Preview)); err != nil {
				w.logger.Error("weekly telegram delivery failed", logging.WithSafeError([]any{"operation", "scheduler.weekly.send", "operation_id", opID}, err)...)
			}
		}
		w.logger.Info("weekly summary generated", "operation", "scheduler.weekly.generated", "operation_id", opID)
	}
}
