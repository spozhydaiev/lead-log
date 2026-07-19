package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"github.com/spozhydaiev/lead-log/internal/models"
)

type Config struct {
	TelegramBotToken                string
	DatabaseURL                     string
	LLMBaseURL                      string
	LLMAPIKey                       string
	LLMModel                        string
	AllowedTelegramUserIDs          map[int64]bool
	DailySummaryEnabled             bool
	DailySummaryTime                string
	DailySummaryTimezone            string
	DailySummaryLocation            *time.Location
	DailySummaryMode                string
	LogLevel                        string
	LogFormat                       string
	ResponseLanguage                models.ResponseLanguage
	TelegramUpdateProcessingTimeout time.Duration
	NoteEnrichmentProcessingTimeout time.Duration
	NoteEnrichmentWorkerEnabled     bool
	NoteEnrichmentPollInterval      time.Duration
	NoteEnrichmentBatchSize         int
	NoteEnrichmentWorkerConcurrency int
	NoteEnrichmentMaxAttempts       int
	HTTPEnabled                     bool
	HTTPAddress                     string
	HTTPPort                        int
	HTTPReadTimeout                 time.Duration
	HTTPWriteTimeout                time.Duration
	HTTPIdleTimeout                 time.Duration
	FrontendOrigins                 []string
	AuthSessionCookieName           string
	AuthSessionTTL                  time.Duration
	AuthSessionSecure               bool
	AuthPasswordMinLength           int
	TelegramBotUsername             string
	TelegramLinkTokenTTL            time.Duration
}

func Load() Config {
	_ = godotenv.Load()
	dailyTimezone := envOr("DAILY_SUMMARY_TIMEZONE", "Europe/Warsaw")
	dailyLocation, err := time.LoadLocation(dailyTimezone)
	if err != nil {
		panic("invalid env var DAILY_SUMMARY_TIMEZONE: " + err.Error())
	}
	dailyTime := envOr("DAILY_SUMMARY_TIME", "08:45")
	if _, err := time.Parse("15:04", dailyTime); err != nil {
		panic("invalid env var DAILY_SUMMARY_TIME: " + err.Error())
	}
	dailyMode := envOr("DAILY_SUMMARY_MODE", "previous_workday")
	if dailyMode != "previous_workday" && dailyMode != "current_day" {
		panic("invalid env var DAILY_SUMMARY_MODE: must be previous_workday or current_day")
	}

	telegramUpdateProcessingTimeout, err := time.ParseDuration(envOr("TELEGRAM_UPDATE_PROCESSING_TIMEOUT", "2m"))
	if err != nil || telegramUpdateProcessingTimeout <= 45*time.Second {
		panic("invalid env var TELEGRAM_UPDATE_PROCESSING_TIMEOUT: must be a duration greater than 45s")
	}

	noteEnrichmentProcessingTimeout, err := time.ParseDuration(envOr("NOTE_ENRICHMENT_PROCESSING_TIMEOUT", "3m"))
	if err != nil || noteEnrichmentProcessingTimeout <= telegramUpdateProcessingTimeout {
		panic("invalid env var NOTE_ENRICHMENT_PROCESSING_TIMEOUT: must be a duration greater than TELEGRAM_UPDATE_PROCESSING_TIMEOUT")
	}

	noteEnrichmentPollInterval, err := time.ParseDuration(envOr("NOTE_ENRICHMENT_POLL_INTERVAL", "5s"))
	if err != nil || noteEnrichmentPollInterval <= 0 {
		panic("invalid env var NOTE_ENRICHMENT_POLL_INTERVAL: must be a positive duration")
	}
	noteEnrichmentBatchSize := parsePositiveInt("NOTE_ENRICHMENT_BATCH_SIZE", 10)
	noteEnrichmentWorkerConcurrency := parsePositiveInt("NOTE_ENRICHMENT_WORKER_CONCURRENCY", 1)
	noteEnrichmentMaxAttempts := parsePositiveInt("NOTE_ENRICHMENT_MAX_ATTEMPTS", 3)

	responseLanguage, err := models.ParseResponseLanguage(envOr("RESPONSE_LANGUAGE", string(models.LanguageEnglish)))
	if err != nil {
		panic(err.Error())
	}

	cfg := Config{
		TelegramBotToken:                mustEnv("TELEGRAM_BOT_TOKEN"),
		DatabaseURL:                     mustEnv("DATABASE_URL"),
		LLMBaseURL:                      envOr("LLM_BASE_URL", "https://api.openai.com/v1"),
		LLMAPIKey:                       mustEnv("LLM_API_KEY"),
		LLMModel:                        envOr("LLM_MODEL", "gpt-4.1-mini"),
		AllowedTelegramUserIDs:          mustAllowedUsers(os.Getenv("ALLOWED_TELEGRAM_USER_IDS")),
		DailySummaryEnabled:             parseBool(os.Getenv("DAILY_SUMMARY_ENABLED")),
		DailySummaryTime:                dailyTime,
		DailySummaryTimezone:            dailyTimezone,
		DailySummaryLocation:            dailyLocation,
		DailySummaryMode:                dailyMode,
		LogLevel:                        envOr("LOG_LEVEL", "info"),
		LogFormat:                       envOr("LOG_FORMAT", "text"),
		ResponseLanguage:                responseLanguage,
		TelegramUpdateProcessingTimeout: telegramUpdateProcessingTimeout,
		NoteEnrichmentProcessingTimeout: noteEnrichmentProcessingTimeout,
		NoteEnrichmentWorkerEnabled:     envOr("NOTE_ENRICHMENT_WORKER_ENABLED", "true") != "false",
		NoteEnrichmentPollInterval:      noteEnrichmentPollInterval,
		NoteEnrichmentBatchSize:         noteEnrichmentBatchSize,
		NoteEnrichmentWorkerConcurrency: noteEnrichmentWorkerConcurrency,
		NoteEnrichmentMaxAttempts:       noteEnrichmentMaxAttempts,
		HTTPEnabled:                     parseBool(envOr("HTTP_ENABLED", "true")),
		HTTPAddress:                     envOr("HTTP_ADDRESS", "0.0.0.0"),
		HTTPPort:                        parsePositiveInt("HTTP_PORT", 8080),
		HTTPReadTimeout:                 parseDuration("HTTP_READ_TIMEOUT", "10s"),
		HTTPWriteTimeout:                parseDuration("HTTP_WRITE_TIMEOUT", "30s"),
		HTTPIdleTimeout:                 parseDuration("HTTP_IDLE_TIMEOUT", "60s"),
		FrontendOrigins:                 parseCSV(envOr("FRONTEND_ORIGINS", os.Getenv("HTTP_ALLOWED_ORIGINS"))),
		AuthSessionCookieName:           envOr("AUTH_SESSION_COOKIE_NAME", "lead_log_session"),
		AuthSessionTTL:                  parseDuration("AUTH_SESSION_TTL", "720h"),
		AuthSessionSecure:               envOr("AUTH_SESSION_SECURE", "true") != "false",
		AuthPasswordMinLength:           parsePositiveInt("AUTH_PASSWORD_MIN_LENGTH", 12),
		TelegramBotUsername:             envOr("TELEGRAM_BOT_USERNAME", "LeadLogBot"),
		TelegramLinkTokenTTL:            parseDuration("TELEGRAM_LINK_TOKEN_TTL", "10m"),
	}
	if cfg.HTTPEnabled {
		for _, origin := range cfg.FrontendOrigins {
			if origin == "*" {
				panic("invalid env var FRONTEND_ORIGINS: wildcard is not allowed")
			}
		}
	}
	return cfg
}

func parseDuration(key, fallback string) time.Duration {
	d, err := time.ParseDuration(envOr(key, fallback))
	if err != nil || d <= 0 {
		panic("invalid env var " + key + ": must be a positive duration")
	}
	return d
}

func parseCSV(raw string) []string {
	var out []string
	for _, v := range strings.Split(raw, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func mustEnv(key string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		panic("missing env var: " + key)
	}
	return v
}

func envOr(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

func mustAllowedUsers(raw string) map[int64]bool {
	users := parseAllowedUsers(raw)
	if len(users) == 0 {
		panic("missing env var: ALLOWED_TELEGRAM_USER_IDS")
	}
	return users
}

func IsTelegramUserAllowed(allowed map[int64]bool, telegramUserID int64) bool {
	return len(allowed) > 0 && allowed[telegramUserID]
}

func parseAllowedUsers(raw string) map[int64]bool {
	result := map[int64]bool{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err == nil {
			result[id] = true
		}
	}
	return result
}

func parseBool(raw string) bool {
	v := strings.ToLower(strings.TrimSpace(raw))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func parsePositiveInt(key string, fallback int) int {
	raw := envOr(key, strconv.Itoa(fallback))
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		panic("invalid env var " + key + ": must be a positive integer")
	}
	return v
}
