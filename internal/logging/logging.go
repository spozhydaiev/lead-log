package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
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

type operationIDKey struct{}

func NewOperationID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("op-%d", os.Getpid())
	}
	return hex.EncodeToString(b[:])
}

func ContextWithOperationID(ctx context.Context, id string) context.Context {
	if strings.TrimSpace(id) == "" {
		id = NewOperationID()
	}
	return context.WithValue(ctx, operationIDKey{}, id)
}

func EnsureOperationID(ctx context.Context) (context.Context, string) {
	if id := OperationID(ctx); id != "" {
		return ctx, id
	}
	id := NewOperationID()
	return ContextWithOperationID(ctx, id), id
}

func OperationID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(operationIDKey{}).(string)
	return id
}

func OperationIDAttr(ctx context.Context) []any {
	if id := OperationID(ctx); id != "" {
		return []any{"operation_id", id}
	}
	return nil
}

type SafeError interface {
	SafeMessage() string
	Code() string
}

type CodedError struct {
	code, message string
	cause         error
}

func NewCodedError(code, message string, cause error) CodedError {
	return CodedError{code: code, message: message, cause: cause}
}
func (e CodedError) Error() string {
	if e.message != "" {
		return e.message
	}
	return e.code
}
func (e CodedError) Unwrap() error { return e.cause }
func (e CodedError) SafeMessage() string {
	if e.message != "" {
		return e.message
	}
	return e.code
}
func (e CodedError) Code() string { return e.code }

func SafeErrorFields(err error) []any {
	if err == nil {
		return nil
	}
	var safe SafeError
	if errors.As(err, &safe) {
		return []any{"error_class", safe.Code(), "error", safe.SafeMessage()}
	}
	return []any{"error_class", classifyError(err)}
}

func classifyError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "daily digest") || strings.Contains(msg, "parse daily digest"):
		return "daily_digest_validation"
	case strings.Contains(msg, "context canceled"):
		return "context_canceled"
	case strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "timeout"):
		return "timeout"
	case strings.Contains(msg, "connection") || strings.Contains(msg, "connect"):
		return "connection_error"
	case strings.Contains(msg, "constraint") || strings.Contains(msg, "duplicate") || strings.Contains(msg, "sqlstate 23505"):
		return "unique_violation"
	case strings.Contains(msg, "sql") || strings.Contains(msg, "database") || strings.Contains(msg, "pg"):
		return "database_error"
	case strings.Contains(msg, "llm") || strings.Contains(msg, "http") || strings.Contains(msg, "status="):
		return "llm_error"
	default:
		return "internal_error"
	}
}

func WithSafeError(attrs []any, err error) []any { return append(attrs, SafeErrorFields(err)...) }
