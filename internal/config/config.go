package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	TelegramBotToken       string
	DatabaseURL            string
	LLMBaseURL             string
	LLMAPIKey              string
	LLMModel               string
	AllowedTelegramUserIDs map[int64]bool
}

func Load() Config {
	_ = godotenv.Load()
	return Config{
		TelegramBotToken:       mustEnv("TELEGRAM_BOT_TOKEN"),
		DatabaseURL:            mustEnv("DATABASE_URL"),
		LLMBaseURL:             envOr("LLM_BASE_URL", "https://api.openai.com/v1"),
		LLMAPIKey:              mustEnv("LLM_API_KEY"),
		LLMModel:               envOr("LLM_MODEL", "gpt-4.1-mini"),
		AllowedTelegramUserIDs: parseAllowedUsers(os.Getenv("ALLOWED_TELEGRAM_USER_IDS")),
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
