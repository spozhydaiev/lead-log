package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	TelegramBotToken       string
	DatabaseURL            string
	LLMBaseURL             string
	LLMAPIKey              string
	LLMModel               string
	AllowedTelegramUserIDs map[int64]bool
	DailySummaryEnabled    bool
	DailySummaryTime       string
	DailySummaryTimezone   string
	DailySummaryLocation   *time.Location
}

func Load() Config {
	_ = godotenv.Load()
	dailyTimezone := envOr("DAILY_SUMMARY_TIMEZONE", "Europe/Warsaw")
	dailyLocation, err := time.LoadLocation(dailyTimezone)
	if err != nil {
		panic("invalid env var DAILY_SUMMARY_TIMEZONE: " + err.Error())
	}
	dailyTime := envOr("DAILY_SUMMARY_TIME", "18:00")
	if _, err := time.Parse("15:04", dailyTime); err != nil {
		panic("invalid env var DAILY_SUMMARY_TIME: " + err.Error())
	}

	return Config{
		TelegramBotToken:       mustEnv("TELEGRAM_BOT_TOKEN"),
		DatabaseURL:            mustEnv("DATABASE_URL"),
		LLMBaseURL:             envOr("LLM_BASE_URL", "https://api.openai.com/v1"),
		LLMAPIKey:              mustEnv("LLM_API_KEY"),
		LLMModel:               envOr("LLM_MODEL", "gpt-4.1-mini"),
		AllowedTelegramUserIDs: parseAllowedUsers(os.Getenv("ALLOWED_TELEGRAM_USER_IDS")),
		DailySummaryEnabled:    parseBool(os.Getenv("DAILY_SUMMARY_ENABLED")),
		DailySummaryTime:       dailyTime,
		DailySummaryTimezone:   dailyTimezone,
		DailySummaryLocation:   dailyLocation,
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
