package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"crypto/pbkdf2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/spozhydaiev/lead-log/internal/adapters/store"
)

var ErrDuplicateEmail = errors.New("duplicate email")
var ErrInvalidCredentials = errors.New("invalid credentials")
var ErrTelegramConflict = errors.New("telegram account conflict")

const MaxDisplayNameLength = 100

type AuthConfig struct {
	SessionTTL        time.Duration
	PasswordMinLength int
}
type RegisterInput struct {
	Email            string `json:"email"`
	Password         string `json:"password"`
	DisplayName      string `json:"display_name,omitempty"`
	Timezone         string `json:"timezone,omitempty"`
	ResponseLanguage string `json:"response_language,omitempty"`
}

type ValidationError struct {
	Fields map[string]string
}

func (e ValidationError) Error() string { return "validation error" }

type LoginInput struct{ Email, Password string }
type AuthSession struct {
	User      store.CurrentUser
	Token     string
	ExpiresAt time.Time
}

func NormalizeEmail(raw string) (string, string, error) {
	e := strings.TrimSpace(raw)
	if e == "" {
		return "", "", fmt.Errorf("email required")
	}
	a, err := mail.ParseAddress(e)
	if err != nil || a.Address != e {
		return "", "", fmt.Errorf("invalid email")
	}
	return e, strings.ToLower(e), nil
}
func ValidatePassword(p string, min int) error {
	if min <= 0 {
		min = 12
	}
	if len(p) < min || len(p) > 128 || strings.TrimSpace(p) == "" {
		return fmt.Errorf("invalid password")
	}
	return nil
}
func validateTZ(tz string) string {
	if strings.TrimSpace(tz) == "" {
		return "Europe/Warsaw"
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return ""
	}
	return tz
}
func ValidateResponseLanguage(l string) string {
	l = strings.TrimSpace(l)
	if l == "" {
		return "en"
	}
	if l == "en" || l == "uk" {
		return l
	}
	return ""
}

func (s *Service) Register(ctx context.Context, in RegisterInput, cfg AuthConfig) (AuthSession, error) {
	_, _ = s.store.CleanupAuth(ctx, time.Now(), 100)
	email, norm, err := NormalizeEmail(in.Email)
	fields := map[string]string{}
	if err != nil {
		fields["email"] = "Enter a valid email address."
	}
	if err := ValidatePassword(in.Password, cfg.PasswordMinLength); err != nil {
		fields["password"] = "Password must be 12 to 128 characters."
	}
	displayName := strings.TrimSpace(in.DisplayName)
	if len([]rune(displayName)) > MaxDisplayNameLength {
		fields["display_name"] = "Display name is too long."
	}
	tz := validateTZ(in.Timezone)
	if tz == "" {
		fields["timezone"] = "Enter a valid IANA timezone."
	}
	lang := ValidateResponseLanguage(in.ResponseLanguage)
	if lang == "" {
		fields["response_language"] = "Choose a supported response language."
	}
	if len(fields) > 0 {
		return AuthSession{}, ValidationError{Fields: fields}
	}
	h, err := hashPassword(in.Password)
	if err != nil {
		return AuthSession{}, err
	}
	tok, th, err := NewOpaqueToken()
	if err != nil {
		return AuthSession{}, err
	}
	exp := time.Now().Add(ttl(cfg.SessionTTL))
	u, err := s.store.CreateUserWithIdentityAndSession(ctx, email, norm, h, displayName, tz, lang, th, exp)
	if isUnique(err) {
		return AuthSession{}, ErrDuplicateEmail
	}
	if err != nil {
		return AuthSession{}, err
	}
	return AuthSession{User: u, Token: tok, ExpiresAt: exp}, nil
}
func (s *Service) Login(ctx context.Context, in LoginInput, cfg AuthConfig) (AuthSession, error) {
	_, _ = s.store.CleanupAuth(ctx, time.Now(), 100)
	_, norm, err := NormalizeEmail(in.Email)
	if err != nil {
		return AuthSession{}, ErrInvalidCredentials
	}
	uid, h, err := s.store.LocalIdentityByEmail(ctx, norm)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = verifyPassword(dummyHash(), in.Password)
		return AuthSession{}, ErrInvalidCredentials
	}
	if err != nil {
		return AuthSession{}, err
	}
	if !verifyPassword(h, in.Password) {
		return AuthSession{}, ErrInvalidCredentials
	}
	tok, th, err := NewOpaqueToken()
	if err != nil {
		return AuthSession{}, err
	}
	exp := time.Now().Add(ttl(cfg.SessionTTL))
	if err := s.store.CreateSession(ctx, uid, th, exp); err != nil {
		return AuthSession{}, err
	}
	u, err := s.store.CurrentUserByID(ctx, uid)
	if err != nil {
		return AuthSession{}, err
	}
	return AuthSession{User: u, Token: tok, ExpiresAt: exp}, nil
}
func (s *Service) SessionByToken(ctx context.Context, token string, now time.Time) (store.CurrentUser, error) {
	return s.store.CurrentUserBySessionHash(ctx, HashToken(token), now)
}
func (s *Service) RevokeSession(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.store.RevokeSession(ctx, HashToken(token))
}
func (s *Service) CurrentUser(ctx context.Context, userID int64) (store.CurrentUser, error) {
	return s.store.CurrentUserByID(ctx, userID)
}
func (s *Service) TelegramStatus(ctx context.Context, userID int64) (store.TelegramStatus, error) {
	return s.store.TelegramStatus(ctx, userID)
}
func (s *Service) CreateTelegramLink(ctx context.Context, userID int64, linkTTL time.Duration) (string, time.Time, error) {
	_, _ = s.store.CleanupAuth(ctx, time.Now(), 100)
	tok, th, err := NewOpaqueToken()
	if err != nil {
		return "", time.Time{}, err
	}
	exp := time.Now().Add(linkTTL)
	if err := s.store.CreateTelegramLinkToken(ctx, userID, th, exp); err != nil {
		return "", time.Time{}, err
	}
	return tok, exp, nil
}
func (s *Service) ConsumeTelegramLink(ctx context.Context, token string, telegramUserID, chatID int64, sessionTTL time.Duration) (store.LinkConsumeResult, string, error) {
	newTok, newHash, err := NewOpaqueToken()
	if err != nil {
		return store.LinkConsumeInvalid, "", err
	}
	res, _, err := s.store.LinkTelegramByToken(ctx, HashToken(token), telegramUserID, chatID, newHash, time.Now().Add(ttl(sessionTTL)))
	if res == store.LinkConsumeConflict {
		return res, "", ErrTelegramConflict
	}
	if res != store.LinkConsumeSuccess {
		return res, "", err
	}
	return res, newTok, err
}
func (s *Service) ResolveTelegramUser(ctx context.Context, telegramUserID, chatID int64) (int64, error) {
	return s.store.ResolveTelegramUser(ctx, telegramUserID, chatID)
}
func (s *Service) UnlinkTelegram(ctx context.Context, userID int64) error {
	return s.store.UnlinkTelegram(ctx, userID)
}
func (s *Service) CleanupAuth(ctx context.Context, limit int) (int64, error) {
	return s.store.CleanupAuth(ctx, time.Now(), limit)
}
func ttl(d time.Duration) time.Duration {
	if d <= 0 {
		return 30 * 24 * time.Hour
	}
	return d
}
func NewOpaqueToken() (string, string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	t := base64.RawURLEncoding.EncodeToString(b)
	return t, HashToken(t), nil
}
func HashToken(t string) string {
	s := sha256.Sum256([]byte(t))
	return base64.RawURLEncoding.EncodeToString(s[:])
}
func hashPassword(p string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	h, err := pbkdf2.Key(sha256.New, p, salt, 600000, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("$pbkdf2-sha256$i=600000$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(h)), nil
}
func verifyPassword(encoded, p string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, p, salt, 600000, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}
func dummyHash() string {
	return "$pbkdf2-sha256$i=600000$AAAAAAAAAAAAAAAAAAAAAA$HfzNAkfEUn6L+jHZYbcokybyDFv/TH+Ile3Oq8TZHYU"
}
func isUnique(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}
