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

	responseLanguage, err := models.ParseResponseLanguage(envOr("RESPONSE_LANGUAGE", string(models.LanguageEnglish)))
	if err != nil {
		panic(err.Error())
	}

	return Config{
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
	}
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
