package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/spozhydaiev/lead-log/internal/adapters/bot"
	"github.com/spozhydaiev/lead-log/internal/adapters/llm"
	"github.com/spozhydaiev/lead-log/internal/adapters/store"
	"github.com/spozhydaiev/lead-log/internal/config"
	svc "github.com/spozhydaiev/lead-log/internal/services"
	"github.com/spozhydaiev/lead-log/migrations"
	"github.com/spozhydaiev/lead-log/pkg/db"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()

	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db pool: %v", err)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		log.Fatalf("db migrate: %v", err)
	}

	st := store.New(pool)
	llmClient := llm.NewClient(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel)

	svc := svc.New(st, llmClient)

	telegramBot, err := bot.New(cfg, svc)
	if err != nil {
		log.Fatalf("telegram bot: %v", err)
	}

	if err := telegramBot.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("bot stopped: %v", err)
	}
}
