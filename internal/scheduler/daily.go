package scheduler

import (
	"context"
	"log"
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
}

func NewDailySummary(st *store.Store, service *svc.Service, sender Sender, allowedUsers map[int64]bool, dailyTime string, loc *time.Location) (*DailySummary, error) {
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
	return &DailySummary{
		store:           st,
		service:         service,
		sender:          sender,
		telegramUserIDs: ids,
		runAt:           time.Duration(parsed.Hour())*time.Hour + time.Duration(parsed.Minute())*time.Minute,
		location:        loc,
	}, nil
}

func (d *DailySummary) Run(ctx context.Context) {
	log.Printf("daily summary scheduler started at %s in %s", formatRunAt(d.runAt), d.location.String())
	for {
		now := time.Now().In(d.location)
		next := nextRun(now, d.runAt, d.location)
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			log.Printf("daily summary scheduler stopped")
			return
		case <-timer.C:
			d.sendForAll(ctx, next)
		}
	}
}

func (d *DailySummary) sendForAll(ctx context.Context, now time.Time) {
	for _, telegramUserID := range d.telegramUserIDs {
		if ctx.Err() != nil {
			return
		}
		userID, err := d.service.EnsureUser(ctx, telegramUserID, "")
		if err != nil {
			log.Printf("daily summary ensure user %d: %v", telegramUserID, err)
			continue
		}
		scopeKey := now.In(d.location).Format("2006-01-02")
		sent, err := d.store.HasDailySummarySend(ctx, userID, scopeKey)
		if err != nil {
			log.Printf("daily summary sent check user %d: %v", telegramUserID, err)
			continue
		}
		if sent {
			continue
		}
		response, err := d.service.DailyAt(ctx, userID, now, false)
		if err != nil {
			log.Printf("daily summary generate user %d: %v", telegramUserID, err)
			continue
		}
		if err := d.sender.SendMessage(telegramUserID, response); err != nil {
			log.Printf("daily summary send user %d: %v", telegramUserID, err)
			continue
		}
		if err := d.store.RecordDailySummarySend(ctx, userID, scopeKey); err != nil {
			log.Printf("daily summary record send user %d: %v", telegramUserID, err)
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
