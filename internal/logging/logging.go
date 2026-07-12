package logging

import (
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
)

const (
	DefaultLevel  = "info"
	DefaultFormat = "text"
)

type Config struct {
	Level  string
	Format string
}

func LoadConfig() Config {
	return Config{Level: normalizeLevel(os.Getenv("LOG_LEVEL")), Format: normalizeFormat(os.Getenv("LOG_FORMAT"))}
}

func New(cfg Config) *slog.Logger { return NewWithWriter(cfg, os.Stdout) }

func NewWithWriter(cfg Config, w io.Writer) *slog.Logger {
	level := slog.LevelInfo
	switch normalizeLevel(cfg.Level) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	opts := &slog.HandlerOptions{Level: level}
	if normalizeFormat(cfg.Format) == "json" {
		return slog.New(slog.NewJSONHandler(w, opts))
	}
	return slog.New(slog.NewTextHandler(w, opts))
}

func normalizeLevel(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "debug", "info", "warn", "error":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return DefaultLevel
	}
}

func normalizeFormat(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "text", "json":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return DefaultFormat
	}
}

func SafeHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return u.Hostname()
}

func HashPrefix(hash string) string {
	if len(hash) <= 8 {
		return hash
	}
	return hash[:8]
}
