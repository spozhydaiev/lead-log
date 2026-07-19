package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spozhydaiev/lead-log/internal/adapters/bot"
	"github.com/spozhydaiev/lead-log/internal/adapters/httpapi"
	"github.com/spozhydaiev/lead-log/internal/adapters/llm"
	"github.com/spozhydaiev/lead-log/internal/adapters/store"
	"github.com/spozhydaiev/lead-log/internal/config"
	"github.com/spozhydaiev/lead-log/internal/logging"
	"github.com/spozhydaiev/lead-log/internal/scheduler"
	svc "github.com/spozhydaiev/lead-log/internal/services"
	"github.com/spozhydaiev/lead-log/internal/workers"
	"github.com/spozhydaiev/lead-log/migrations"
	"github.com/spozhydaiev/lead-log/pkg/db"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()
	logger := logging.New(logging.Config{Level: cfg.LogLevel, Format: cfg.LogFormat})
	logger.Info("application starting",
		"llm_model", cfg.LLMModel,
		"llm_base_host", logging.SafeHost(cfg.LLMBaseURL),
		"daily_summary_enabled", cfg.DailySummaryEnabled,
		"daily_summary_time", cfg.DailySummaryTime,
		"daily_summary_timezone", cfg.DailySummaryTimezone,
		"daily_summary_mode", cfg.DailySummaryMode,
		"log_level", cfg.LogLevel,
		"log_format", cfg.LogFormat,
		"response_language", string(cfg.ResponseLanguage),
		"response_language_name", cfg.ResponseLanguage.DisplayName(),
		"note_enrichment_processing_timeout", cfg.NoteEnrichmentProcessingTimeout.String(),
		"note_enrichment_worker_enabled", cfg.NoteEnrichmentWorkerEnabled,
		"note_enrichment_poll_interval", cfg.NoteEnrichmentPollInterval.String(),
		"note_enrichment_batch_size", cfg.NoteEnrichmentBatchSize,
		"note_enrichment_worker_concurrency", cfg.NoteEnrichmentWorkerConcurrency,
		"note_enrichment_max_attempts", cfg.NoteEnrichmentMaxAttempts,
		"allowlist_configured", len(cfg.AllowedTelegramUserIDs) > 0,
		"allowlist_size", len(cfg.AllowedTelegramUserIDs),
		"http_enabled", cfg.HTTPEnabled,
	)

	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("database connection failed", logging.WithSafeError([]any{"operation", "db.connect"}, err)...)
		os.Exit(1)
	}
	defer pool.Close()
	logger.Info("database connection established", "component", "db", "operation", "db.connect")

	if err := db.Migrate(ctx, pool, migrations.FS, logger.With("component", "migrations")); err != nil {
		logger.Error("migration runner failed", logging.WithSafeError([]any{"component", "migrations", "operation", "db.migrate"}, err)...)
		os.Exit(1)
	}

	st := store.New(pool, logger.With("component", "store"))
	llmClient := llm.NewClient(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel, cfg.ResponseLanguage, logger.With("component", "llm"))

	service := svc.New(st, llmClient, svc.WithDailyLocation(cfg.DailySummaryLocation), svc.WithNoteEnrichmentStaleTimeout(cfg.NoteEnrichmentProcessingTimeout), svc.WithResponseLanguage(cfg.ResponseLanguage), svc.WithLogger(logger.With("component", "service")))

	var httpServerErr <-chan error
	if cfg.HTTPEnabled {
		handler := httpapi.New(service, pool, httpapi.Config{AllowedOrigins: cfg.FrontendOrigins, ResponseLanguage: string(cfg.ResponseLanguage), Timezone: cfg.DailySummaryTimezone, SessionCookieName: cfg.AuthSessionCookieName, SessionTTL: cfg.AuthSessionTTL, SessionSecure: cfg.AuthSessionSecure, PasswordMinLength: cfg.AuthPasswordMinLength, TelegramBotUsername: cfg.TelegramBotUsername, TelegramLinkTokenTTL: cfg.TelegramLinkTokenTTL}, logger.With("component", "http_api"))
		srv := httpapi.NewServer(httpapi.ServerConfig{Address: fmt.Sprintf("%s:%d", cfg.HTTPAddress, cfg.HTTPPort), ReadTimeout: cfg.HTTPReadTimeout, WriteTimeout: cfg.HTTPWriteTimeout, IdleTimeout: cfg.HTTPIdleTimeout}, handler)
		ch := make(chan error, 1)
		httpServerErr = ch
		go func() { ch <- httpapi.RunServer(ctx, srv) }()
		logger.Info("HTTP API starting", "component", "http_api", "operation", "http.start")
	}

	telegramBot, err := bot.New(cfg, service, logger.With("component", "bot"))
	if err != nil {
		logger.Error("telegram bot initialization failed", logging.WithSafeError([]any{"component", "bot", "operation", "bot.init"}, err)...)
		os.Exit(1)
	}

	if cfg.NoteEnrichmentWorkerEnabled {
		noteWorker := workers.NewNoteEnrichment(st, service, workers.NoteEnrichmentConfig{PollInterval: cfg.NoteEnrichmentPollInterval, BatchSize: cfg.NoteEnrichmentBatchSize, Concurrency: cfg.NoteEnrichmentWorkerConcurrency, MaxAttempts: cfg.NoteEnrichmentMaxAttempts, StaleTimeout: cfg.NoteEnrichmentProcessingTimeout}, logger.With("component", "note_enrichment_worker"))
		go noteWorker.Run(ctx)
	}

	if cfg.DailySummaryEnabled {
		dailyScheduler, err := scheduler.NewDailySummary(st, service, telegramBot, cfg.AllowedTelegramUserIDs, cfg.DailySummaryTime, cfg.DailySummaryLocation, scheduler.SummaryMode(cfg.DailySummaryMode), logger.With("component", "scheduler"))
		if err != nil {
			logger.Error("daily summary scheduler initialization failed", logging.WithSafeError([]any{"component", "scheduler", "operation", "scheduler.init"}, err)...)
			os.Exit(1)
		}
		go dailyScheduler.Run(ctx)
	}

	botErr := make(chan error, 1)
	go func() { botErr <- telegramBot.Run(ctx) }()
	httpFinished := false
	select {
	case err := <-botErr:
		if err != nil && ctx.Err() == nil {
			logger.Error("bot stopped", logging.WithSafeError([]any{"component", "bot", "operation", "bot.run"}, err)...)
			stop()
		}
	case err := <-httpServerErr:
		httpFinished = true
		if err != nil && ctx.Err() == nil {
			logger.Error("HTTP API stopped", logging.WithSafeError([]any{"component", "http_api", "operation", "http.run"}, err)...)
			stop()
		}
	case <-ctx.Done():
	}
	<-ctx.Done()
	// Let the HTTP server finish graceful shutdown before pool.Close runs.
	if cfg.HTTPEnabled && !httpFinished {
		if err := <-httpServerErr; err != nil {
			logger.Error("HTTP API shutdown failed", logging.WithSafeError([]any{"component", "http_api", "operation", "http.shutdown"}, err)...)
		}
	}
}
