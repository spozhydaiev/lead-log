package scheduler

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/spozhydaiev/lead-log/internal/adapters/store"
	svc "github.com/spozhydaiev/lead-log/internal/services"
)

type Sender interface {
	SendMessage(chatID int64, text string) error
}

type DailySummary struct {
	store           *store.Store
	service         *svc.Service
	sender          Sender
	telegramUserIDs []int64
	runAt           time.Duration
	location        *time.Location
	logger          *slog.Logger
}

func NewDailySummary(st *store.Store, service *svc.Service, sender Sender, allowedUsers map[int64]bool, dailyTime string, loc *time.Location, logger ...*slog.Logger) (*DailySummary, error) {
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
	l := slog.Default()
	if len(logger) > 0 && logger[0] != nil {
		l = logger[0]
	}
	return &DailySummary{
		store:           st,
		service:         service,
		sender:          sender,
		telegramUserIDs: ids,
		runAt:           time.Duration(parsed.Hour())*time.Hour + time.Duration(parsed.Minute())*time.Minute,
		location:        loc,
		logger:          l,
	}, nil
}

func (d *DailySummary) Run(ctx context.Context) {
	d.logger.Info("scheduler started", "operation", "scheduler.run", "daily_summary_time", formatRunAt(d.runAt), "timezone", d.location.String())
	for {
		now := time.Now().In(d.location)
		next := nextRun(now, d.runAt, d.location)
		d.logger.Info("next scheduled run", "operation", "scheduler.next_run", "next_run", next.Format(time.RFC3339))
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

func (d *DailySummary) sendForAll(ctx context.Context, now time.Time) {
	for _, telegramUserID := range d.telegramUserIDs {
		d.logger.Info("user processing started", "operation", "scheduler.user", "telegram_user_id", telegramUserID)
		if ctx.Err() != nil {
			return
		}
		userID, err := d.service.EnsureUser(ctx, telegramUserID, "")
		if err != nil {
			d.logger.Error("user processing failed", "operation", "scheduler.ensure_user", "telegram_user_id", telegramUserID, "error", err)
			continue
		}
		scopeKey := now.In(d.location).Format("2006-01-02")
		sent, err := d.store.HasDailySummarySend(ctx, userID, scopeKey)
		if err != nil {
			d.logger.Error("user processing failed", "operation", "scheduler.sent_check", "telegram_user_id", telegramUserID, "user_id", userID, "error", err)
			continue
		}
		if sent {
			d.logger.Info("summary skipped because already sent", "operation", "scheduler.skip", "telegram_user_id", telegramUserID, "user_id", userID, "scope_key", scopeKey)
			continue
		}
		response, err := d.service.DailyAt(ctx, userID, now, false)
		if err != nil {
			d.logger.Error("user processing failed", "operation", "scheduler.generate", "telegram_user_id", telegramUserID, "user_id", userID, "error", err)
			continue
		}
		if response == d.service.ResponseMessages().NoNotesToday {
			d.logger.Info("summary skipped because no notes", "operation", "scheduler.skip", "telegram_user_id", telegramUserID, "user_id", userID, "scope_key", scopeKey)
			continue
		}
		if err := d.sender.SendMessage(telegramUserID, response); err != nil {
			d.logger.Error("send failure", "operation", "scheduler.send", "telegram_user_id", telegramUserID, "user_id", userID, "error", err)
			continue
		}
		d.logger.Info("send success", "operation", "scheduler.send", "telegram_user_id", telegramUserID, "user_id", userID, "scope_key", scopeKey)
		d.logger.Info("user processing completed", "operation", "scheduler.user", "telegram_user_id", telegramUserID, "user_id", userID)
		if err := d.store.RecordDailySummarySend(ctx, userID, scopeKey); err != nil {
			d.logger.Error("user processing failed", "operation", "scheduler.record_send", "telegram_user_id", telegramUserID, "user_id", userID, "error", err)
		}
	}
}

func nextRun(now time.Time, runAt time.Duration, loc *time.Location) time.Time {
	local := now.In(loc)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	next := midnight.Add(runAt)
	if !next.After(local) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

func formatRunAt(d time.Duration) string {
	return time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC).Add(d).Format("15:04")
}
