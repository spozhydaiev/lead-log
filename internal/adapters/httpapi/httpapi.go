package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/spozhydaiev/lead-log/internal/adapters/httpapi/dto"
	"github.com/spozhydaiev/lead-log/internal/adapters/store"
	"github.com/spozhydaiev/lead-log/internal/services"
)

const maxBody = 64 << 10
const maxNoteLength = 10000

type Principal struct {
	UserID       int64
	SessionToken string
	Timezone     string
}
type contextKey int

const (
	requestIDKey contextKey = iota
	principalKey
)

type Service interface {
	Register(context.Context, services.RegisterInput, services.AuthConfig) (services.AuthSession, error)
	Login(context.Context, services.LoginInput, services.AuthConfig) (services.AuthSession, error)
	SessionByToken(context.Context, string, time.Time) (store.CurrentUser, error)
	RevokeSession(context.Context, string) error
	CurrentUser(context.Context, int64) (store.CurrentUser, error)
	TelegramStatus(context.Context, int64) (store.TelegramStatus, error)
	CreateTelegramLink(context.Context, int64, time.Duration) (string, time.Time, error)
	UnlinkTelegram(context.Context, int64) error
	CreatePendingNote(context.Context, int64, string) (store.APINote, error)
	ListNotes(context.Context, int64, int, *store.PageCursor) ([]store.APINote, error)
	ListNotesHistory(context.Context, int64, services.NotesHistoryFilter, int, *store.PageCursor) (services.NotesHistoryView, error)
	ListActions(context.Context, int64, string, int, *store.PageCursor) ([]store.APIAction, error)
	SetActionStatus(context.Context, int64, int64, string) (store.APIAction, error)
	GetToday(context.Context, int64, time.Time) (services.TodayView, error)
	GetNoteDetail(context.Context, int64, int64) (services.NoteDetailView, error)
	ListPeople(context.Context, int64, services.PeopleListFilter, int, *store.PeoplePageCursor) (services.PeopleListView, error)
	GetPersonWorkspace(context.Context, int64, int64) (services.PersonWorkspaceView, error)
}
type Pinger interface{ Ping(context.Context) error }
type Config struct {
	AllowedOrigins       []string
	ResponseLanguage     string
	Timezone             string
	ReadinessTimeout     time.Duration
	Now                  func() time.Time
	SessionCookieName    string
	SessionTTL           time.Duration
	SessionSecure        bool
	PasswordMinLength    int
	TelegramBotUsername  string
	TelegramLinkTokenTTL time.Duration
}
type API struct {
	service Service
	db      Pinger
	cfg     Config
	logger  *slog.Logger
	origins map[string]bool
	limiter *rateLimiter
}

func New(service Service, db Pinger, cfg Config, logger *slog.Logger) http.Handler {
	if cfg.ReadinessTimeout <= 0 {
		cfg.ReadinessTimeout = 2 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	a := &API{service: service, db: db, cfg: cfg, logger: logger, origins: map[string]bool{}, limiter: newRateLimiter()}
	for _, o := range cfg.AllowedOrigins {
		a.origins[o] = true
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("GET /readyz", a.ready)
	mux.HandleFunc("POST /api/v1/auth/register", a.register)
	mux.HandleFunc("POST /api/v1/auth/login", a.login)
	mux.Handle("POST /api/v1/auth/logout", a.auth(http.HandlerFunc(a.logout)))
	mux.Handle("GET /api/v1/auth/session", a.auth(http.HandlerFunc(a.session)))
	mux.Handle("POST /api/v1/integrations/telegram/link", a.auth(http.HandlerFunc(a.telegramLink)))
	mux.Handle("GET /api/v1/integrations/telegram/status", a.auth(http.HandlerFunc(a.telegramStatus)))
	mux.Handle("DELETE /api/v1/integrations/telegram", a.auth(http.HandlerFunc(a.telegramUnlink)))
	mux.Handle("GET /api/v1/me", a.auth(http.HandlerFunc(a.me)))
	mux.Handle("GET /api/v1/notes", a.auth(http.HandlerFunc(a.notes)))
	mux.Handle("GET /api/v1/notes/{id}", a.auth(http.HandlerFunc(a.noteDetail)))
	mux.Handle("GET /api/v1/today", a.auth(http.HandlerFunc(a.today)))
	mux.Handle("POST /api/v1/notes", a.auth(http.HandlerFunc(a.notes)))
	mux.Handle("GET /api/v1/people", a.auth(http.HandlerFunc(a.people)))
	mux.Handle("GET /api/v1/people/{id}", a.auth(http.HandlerFunc(a.personDetail)))
	mux.Handle("GET /api/v1/actions", a.auth(http.HandlerFunc(a.actions)))
	mux.Handle("PATCH /api/v1/actions/{id}", a.auth(http.HandlerFunc(a.patchAction)))
	return a.middleware(mux)
}

func (a *API) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if len(id) < 8 || len(id) > 128 {
			id = newID()
		}
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		r = r.WithContext(ctx)
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if origin := r.Header.Get("Origin"); origin != "" && a.origins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		if isUnsafe(r.Method) && r.Method != http.MethodOptions {
			origin := r.Header.Get("Origin")
			if origin == "" || !a.origins[origin] {
				writeError(w, r, http.StatusForbidden, "forbidden", "The request is not allowed.")
				return
			}
		}
		if r.Method == http.MethodOptions {
			if !a.origins[r.Header.Get("Origin")] {
				writeError(w, r, http.StatusForbidden, "forbidden", "The request is not allowed.")
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		rw := &responseWriter{ResponseWriter: w, status: 200}
		start := time.Now()
		defer func() {
			if recover() != nil {
				writeError(rw, r, http.StatusInternalServerError, "internal_error", "An internal error occurred.")
			}
			a.logger.Info("HTTP request", "operation", "request", "operation_id", id, "method", r.Method, "route", route(r), "status", rw.status, "response_size", rw.size, "duration_ms", time.Since(start).Milliseconds())
		}()
		next.ServeHTTP(rw, r)
	})
}

type responseWriter struct {
	http.ResponseWriter
	status, size int
}

func (w *responseWriter) WriteHeader(s int) { w.status = s; w.ResponseWriter.WriteHeader(s) }
func (w *responseWriter) Write(b []byte) (int, error) {
	n, e := w.ResponseWriter.Write(b)
	w.size += n
	return n, e
}
func route(r *http.Request) string {
	if r.Pattern != "" {
		p := strings.SplitN(r.Pattern, " ", 2)
		if len(p) == 2 {
			return p[1]
		}
	}
	return "unmatched"
}
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (a *API) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(a.cookieName())
		if err != nil || c.Value == "" {
			writeError(w, r, 401, "unauthorized", "Authentication is required.")
			return
		}
		u, err := a.service.SessionByToken(r.Context(), c.Value, a.cfg.Now())
		if err != nil {
			writeError(w, r, 401, "unauthorized", "Authentication is required.")
			return
		}
		w.Header().Set("Pragma", "no-cache")
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey, Principal{UserID: u.ID, SessionToken: c.Value, Timezone: u.Timezone})))
	})
}
func principal(r *http.Request) (Principal, bool) {
	p, ok := r.Context().Value(principalKey).(Principal)
	return p, ok && p.UserID > 0
}

type rateBucket struct {
	count int
	reset time.Time
}
type rateLimiter struct {
	mu      sync.Mutex
	secret  []byte
	buckets map[string]rateBucket
}

func newRateLimiter() *rateLimiter {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return &rateLimiter{secret: b, buckets: map[string]rateBucket{}}
}
func (l *rateLimiter) allow(scope, value string, limit int, window time.Duration, now time.Time) bool {
	if l == nil {
		return true
	}
	mac := hmac.New(sha256.New, l.secret)
	_, _ = mac.Write([]byte(scope + ":" + value))
	key := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b.reset.IsZero() || now.After(b.reset) {
		b = rateBucket{reset: now.Add(window)}
	}
	b.count++
	l.buckets[key] = b
	return b.count <= limit
}

func isUnsafe(m string) bool {
	return m == http.MethodPost || m == http.MethodPut || m == http.MethodPatch || m == http.MethodDelete
}
func (a *API) cookieName() string {
	if a.cfg.SessionCookieName != "" {
		return a.cfg.SessionCookieName
	}
	return "lead_log_session"
}
func (a *API) setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{Name: a.cookieName(), Value: token, Path: "/", Expires: expires, HttpOnly: true, Secure: a.cfg.SessionSecure, SameSite: http.SameSiteLaxMode})
}
func (a *API) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: a.cookieName(), Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(0, 0), HttpOnly: true, Secure: a.cfg.SessionSecure, SameSite: http.SameSiteLaxMode})
}
func (a *API) authCfg() services.AuthConfig {
	return services.AuthConfig{SessionTTL: a.cfg.SessionTTL, PasswordMinLength: a.cfg.PasswordMinLength}
}
func safeCurrentUser(u store.CurrentUser, st store.TelegramStatus) map[string]any {
	name := ""
	if u.DisplayName != nil {
		name = *u.DisplayName
	}
	email := ""
	if u.Email != nil {
		email = *u.Email
	}
	return map[string]any{"user": map[string]any{"display_name": name, "email": email, "timezone": u.Timezone, "response_language": u.ResponseLanguage}, "telegram": st}
}

func (a *API) register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, r, 405, "method_not_allowed", "Method not allowed.")
		return
	}
	var in services.RegisterInput
	if decode(w, r, &in) != "" {
		return
	}
	_, norm, _ := services.NormalizeEmail(in.Email)
	if norm != "" && !a.limiter.allow("register", norm, 5, time.Hour, a.cfg.Now()) {
		writeError(w, r, 429, "rate_limited", "Too many requests.")
		return
	}
	sess, err := a.service.Register(r.Context(), in, a.authCfg())
	if errors.Is(err, services.ErrDuplicateEmail) {
		writeError(w, r, 409, "conflict", "The account could not be created.")
		return
	}
	if err != nil {
		var ve services.ValidationError
		if errors.As(err, &ve) {
			validationFields(w, r, ve.Fields)
			return
		}
		validation(w, r)
		return
	}
	a.setSessionCookie(w, sess.Token, sess.ExpiresAt)
	st, _ := a.service.TelegramStatus(r.Context(), sess.User.ID)
	writeJSON(w, 201, map[string]any{"data": safeCurrentUser(sess.User, st)})
}
func (a *API) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, r, 405, "method_not_allowed", "Method not allowed.")
		return
	}
	var in services.LoginInput
	if decode(w, r, &in) != "" {
		return
	}
	_, norm, _ := services.NormalizeEmail(in.Email)
	if norm != "" && !a.limiter.allow("login", norm, 10, 15*time.Minute, a.cfg.Now()) {
		writeError(w, r, 429, "rate_limited", "Too many requests.")
		return
	}
	sess, err := a.service.Login(r.Context(), in, a.authCfg())
	if errors.Is(err, services.ErrInvalidCredentials) {
		writeError(w, r, 401, "unauthorized", "Invalid email or password.")
		return
	}
	if err != nil {
		internal(w, r)
		return
	}
	a.setSessionCookie(w, sess.Token, sess.ExpiresAt)
	st, _ := a.service.TelegramStatus(r.Context(), sess.User.ID)
	writeJSON(w, 200, map[string]any{"data": safeCurrentUser(sess.User, st)})
}
func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	_ = a.service.RevokeSession(r.Context(), p.SessionToken)
	a.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}
func (a *API) session(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	u, err := a.service.CurrentUser(r.Context(), p.UserID)
	if err != nil {
		internal(w, r)
		return
	}
	st, err := a.service.TelegramStatus(r.Context(), p.UserID)
	if err != nil {
		internal(w, r)
		return
	}
	writeJSON(w, 200, map[string]any{"data": safeCurrentUser(u, st)})
}
func (a *API) telegramLink(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	if !a.limiter.allow("telegram_link", strconv.FormatInt(p.UserID, 10), 10, time.Hour, a.cfg.Now()) {
		writeError(w, r, 429, "rate_limited", "Too many requests.")
		return
	}
	tok, exp, err := a.service.CreateTelegramLink(r.Context(), p.UserID, a.cfg.TelegramLinkTokenTTL)
	if err != nil {
		internal(w, r)
		return
	}
	bot := strings.TrimPrefix(a.cfg.TelegramBotUsername, "@")
	url := "https://t.me/" + bot + "?start=link_" + tok
	writeJSON(w, 200, map[string]any{"data": map[string]any{"url": url, "expires_at": exp}})
}
func (a *API) telegramStatus(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	st, err := a.service.TelegramStatus(r.Context(), p.UserID)
	if err != nil {
		internal(w, r)
		return
	}
	writeJSON(w, 200, map[string]any{"data": st})
}
func (a *API) telegramUnlink(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	if err := a.service.UnlinkTelegram(r.Context(), p.UserID); err != nil {
		internal(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "ok"})
}
func (a *API) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), a.cfg.ReadinessTimeout)
	defer cancel()
	if a.db == nil || a.db.Ping(ctx) != nil {
		writeJSON(w, 503, map[string]string{"status": "not_ready"})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ready"})
}
func (a *API) me(w http.ResponseWriter, r *http.Request) { a.session(w, r) }

func (a *API) notes(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	if r.Method == http.MethodPost {
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			writeError(w, r, 415, "unsupported_media_type", "Content-Type must be application/json.")
			return
		}
		var in struct {
			Text string `json:"text"`
		}
		if code := decode(w, r, &in); code != "" {
			return
		}
		in.Text = strings.TrimSpace(in.Text)
		if in.Text == "" || len([]rune(in.Text)) > maxNoteLength {
			validation(w, r)
			return
		}
		n, err := a.service.CreatePendingNote(r.Context(), p.UserID, in.Text)
		if err != nil {
			internal(w, r)
			return
		}
		writeJSON(w, 201, map[string]any{"data": map[string]any{"id": publicID("note", n.ID), "processing_status": n.ProcessingStatus, "created_at": n.CreatedAt}})
		return
	}
	limit, filter, hash, cursor, ok := notesPage(r, p.Timezone)
	if !ok {
		validation(w, r)
		return
	}
	v, err := a.service.ListNotesHistory(r.Context(), p.UserID, filter, limit+1, cursor)
	if err != nil {
		internal(w, r)
		return
	}
	next := nextCursorTodayNotes(v.Notes, limit, hash)
	hasMore := len(v.Notes) > limit
	if hasMore {
		v.Notes = v.Notes[:limit]
	}
	out := dto.NotesHistory{Items: []dto.TodayNote{}, Page: dto.NotesPage{NextCursor: next, HasMore: hasMore}}
	for _, n := range v.Notes {
		out.Items = append(out.Items, mapPreviewNote(n))
	}
	a.logger.Info("notes list completed", "component", "http_api", "operation", "notes.list", "operation_id", requestID(r), "result", "success", "result_count", len(out.Items), "has_more", hasMore, "has_status_filter", filter.Status != "", "has_date_filter", filter.FromUTC != nil || filter.ToUTC != nil, "has_search_filter", filter.Query != "")
	writeJSON(w, 200, map[string]any{"data": out})
}

func mapPreviewNote(n services.TodayNote) dto.TodayNote {
	x := dto.TodayNote{ID: publicID("note", n.ID), RawText: runePreview(n.RawText, 400), Summary: n.Summary, ProcessingStatus: n.ProcessingStatus, CreatedAt: n.CreatedAt, ProcessedAt: n.ProcessedAt, ExtractedCounts: dto.ExtractedCounts{Actions: n.Counts.Actions, People: n.Counts.People, Decisions: n.Counts.Decisions, Entities: n.Counts.Entities}, Preview: dto.NotePreview{People: []dto.LinkedPerson{}, Tickets: []string{}, Tags: []string{}}}
	seen := map[int64]bool{}
	for _, p := range n.People {
		if !seen[p.PersonID] && len(x.Preview.People) < 3 {
			x.Preview.People = append(x.Preview.People, dto.LinkedPerson{ID: publicID("person", p.PersonID), DisplayName: p.Name})
			seen[p.PersonID] = true
		}
	}
	for _, e := range n.Entities {
		if e.Type == "ticket" && len(x.Preview.Tickets) < 3 {
			x.Preview.Tickets = append(x.Preview.Tickets, e.Value)
		} else if e.Type != "ticket" && len(x.Preview.Tags) < 3 {
			x.Preview.Tags = append(x.Preview.Tags, e.Value)
		}
	}
	return x
}
func (a *API) actions(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "open"
	}
	if status != "open" && status != "done" && status != "all" {
		validation(w, r)
		return
	}
	limit, cursor, ok := page(r)
	if !ok {
		validation(w, r)
		return
	}
	items, err := a.service.ListActions(r.Context(), p.UserID, status, limit+1, cursor)
	if err != nil {
		internal(w, r)
		return
	}
	next := nextCursorActions(items, limit)
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]dto.Action, 0, len(items))
	for _, x := range items {
		out = append(out, mapAction(x))
	}
	writeJSON(w, 200, map[string]any{"data": out, "pagination": dto.Pagination{Limit: limit, NextCursor: next}})
}
func (a *API) patchAction(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	id, ok := parsePublicID(r.PathValue("id"), "action")
	if !ok {
		validation(w, r)
		return
	}
	var in struct {
		Status string `json:"status"`
	}
	if decode(w, r, &in) != "" {
		return
	}
	if in.Status != "open" && in.Status != "done" {
		validation(w, r)
		return
	}
	x, err := a.service.SetActionStatus(r.Context(), p.UserID, id, in.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, 404, "not_found", "The resource was not found.")
		return
	}
	if err != nil {
		internal(w, r)
		return
	}
	writeJSON(w, 200, map[string]any{"data": mapAction(x)})
}

func mapAction(x store.APIAction) dto.Action {
	var person *dto.LinkedPerson
	if x.PersonName != nil {
		person = &dto.LinkedPerson{DisplayName: *x.PersonName}
		if x.PersonID != nil {
			person.ID = publicID("person", *x.PersonID)
		}
	}
	var note *string
	if x.NoteID != nil {
		s := publicID("note", *x.NoteID)
		note = &s
	}
	return dto.Action{ID: publicID("action", x.ID), Title: x.Title, Status: x.Status, LinkedPerson: person, DueAt: x.DueAt, CreatedAt: x.CreatedAt, CompletedAt: x.CompletedAt, SourceNoteID: note}
}

func (a *API) today(w http.ResponseWriter, r *http.Request) {
	p, ok := principal(r)
	if !ok {
		writeError(w, r, 401, "unauthorized", "Authentication is required.")
		return
	}
	v, err := a.service.GetToday(r.Context(), p.UserID, a.cfg.Now())
	if err != nil {
		internal(w, r)
		return
	}
	out := dto.Today{Date: v.Date, Timezone: v.Timezone, Notes: []dto.TodayNote{}, OpenActions: []dto.Action{}, DailySummary: dto.DailySummary{Date: v.Date, Status: v.DailySummary.Status, Counters: dto.DailyCounters{OpenLoops: v.DailySummary.OpenLoops, Decisions: v.DailySummary.Decisions, PeopleMentioned: v.DailySummary.PeopleMentioned}, ShortSummary: v.DailySummary.ShortSummary, GeneratedAt: v.DailySummary.GeneratedAt}}
	for _, n := range v.Notes {
		out.Notes = append(out.Notes, mapPreviewNote(n))
	}
	for _, x := range v.OpenActions {
		out.OpenActions = append(out.OpenActions, mapAction(x))
	}
	writeJSON(w, 200, map[string]any{"data": out})
}

func runePreview(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
func (a *API) noteDetail(w http.ResponseWriter, r *http.Request) {
	p, ok := principal(r)
	if !ok {
		writeError(w, r, 401, "unauthorized", "Authentication is required.")
		return
	}
	id, ok := parsePublicID(r.PathValue("id"), "note")
	if !ok {
		validation(w, r)
		return
	}
	v, err := a.service.GetNoteDetail(r.Context(), p.UserID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, 404, "not_found", "The resource was not found.")
		return
	}
	if err != nil {
		internal(w, r)
		return
	}
	out := dto.NoteDetail{ID: publicID("note", v.Note.ID), RawText: v.Note.RawText, Summary: v.Note.Summary, ProcessingStatus: v.Note.ProcessingStatus, CreatedAt: v.Note.CreatedAt, ProcessedAt: v.Note.ProcessedAt, Actions: []dto.Action{}, People: []dto.PersonDetail{}, Decisions: []dto.Decision{}, Entities: []dto.Entity{}}
	for _, x := range v.Actions {
		out.Actions = append(out.Actions, mapAction(x))
	}
	idx := map[int64]int{}
	for _, x := range v.People {
		i, exists := idx[x.PersonID]
		if !exists {
			i = len(out.People)
			idx[x.PersonID] = i
			out.People = append(out.People, dto.PersonDetail{ID: publicID("person", x.PersonID), DisplayName: x.Name, Highlights: []dto.Highlight{}})
		}
		out.People[i].Highlights = append(out.People[i].Highlights, dto.Highlight{Type: x.Type, Theme: x.Theme, Text: x.Text})
	}
	for _, x := range v.Decisions {
		out.Decisions = append(out.Decisions, dto.Decision{ID: publicID("decision", x.ID), Text: x.Text, Status: x.Status, Topic: x.Topic})
	}
	seen := map[string]bool{}
	for _, x := range v.Entities {
		k := x.Type + "\x00" + x.Value
		if !seen[k] {
			out.Entities = append(out.Entities, dto.Entity{Type: x.Type, Value: x.Value})
			seen[k] = true
		}
	}
	writeJSON(w, 200, map[string]any{"data": out})
}
func publicID(prefix string, id int64) string { return fmt.Sprintf("%s_%d", prefix, id) }
func parsePublicID(raw, prefix string) (int64, bool) {
	v, err := strconv.ParseInt(strings.TrimPrefix(raw, prefix+"_"), 10, 64)
	return v, err == nil && v > 0 && strings.HasPrefix(raw, prefix+"_")
}

type notesCursor struct {
	CreatedAt  time.Time `json:"created_at"`
	ID         int64     `json:"id"`
	FilterHash string    `json:"filter_hash"`
	Version    int       `json:"v"`
}

func notesPage(r *http.Request, tz string) (int, services.NotesHistoryFilter, string, *store.PageCursor, bool) {
	q := r.URL.Query()
	limit := 20
	if raw := q.Get("limit"); raw != "" {
		v, e := strconv.Atoi(raw)
		if e != nil || v < 1 || v > 50 {
			return 0, services.NotesHistoryFilter{}, "", nil, false
		}
		limit = v
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	var f services.NotesHistoryFilter
	fromRaw, toRaw := q.Get("from"), q.Get("to")
	var fromDay, toDay time.Time
	if fromRaw != "" {
		d, ok := parseLocalDate(fromRaw, loc)
		if !ok {
			return 0, f, "", nil, false
		}
		fromDay = d
		u := d.UTC()
		f.FromUTC = &u
	}
	if toRaw != "" {
		d, ok := parseLocalDate(toRaw, loc)
		if !ok {
			return 0, f, "", nil, false
		}
		toDay = d
		end := d.AddDate(0, 0, 1).UTC()
		f.ToUTC = &end
	}
	if fromRaw != "" && toRaw != "" && fromDay.After(toDay) {
		return 0, f, "", nil, false
	}
	if st := q.Get("status"); st != "" {
		if st != store.NoteProcessingStatusPending && st != store.NoteProcessingStatusProcessing && st != store.NoteProcessingStatusProcessed && st != store.NoteProcessingStatusFailed {
			return 0, f, "", nil, false
		}
		f.Status = st
	}
	if raw := strings.TrimSpace(q.Get("query")); raw != "" {
		if len([]rune(raw)) < 2 || len([]rune(raw)) > 200 {
			return 0, f, "", nil, false
		}
		f.Query = raw
	}
	h := notesFilterHash(f)
	var c *store.PageCursor
	if raw := q.Get("cursor"); raw != "" {
		b, e := base64.RawURLEncoding.DecodeString(raw)
		if e != nil {
			return 0, f, "", nil, false
		}
		var nc notesCursor
		if json.Unmarshal(b, &nc) != nil || nc.Version != 1 || nc.ID <= 0 || nc.CreatedAt.IsZero() || nc.FilterHash != h {
			return 0, f, "", nil, false
		}
		c = &store.PageCursor{CreatedAt: nc.CreatedAt, ID: nc.ID}
	}
	return limit, f, h, c, true
}
func parseLocalDate(raw string, loc *time.Location) (time.Time, bool) {
	if len(raw) != 10 {
		return time.Time{}, false
	}
	d, err := time.ParseInLocation("2006-01-02", raw, loc)
	if err != nil || d.Format("2006-01-02") != raw {
		return time.Time{}, false
	}
	return d, true
}
func notesFilterHash(f services.NotesHistoryFilter) string {
	parts := []string{"v1"}
	if f.FromUTC != nil {
		parts = append(parts, f.FromUTC.UTC().Format(time.RFC3339Nano))
	} else {
		parts = append(parts, "")
	}
	if f.ToUTC != nil {
		parts = append(parts, f.ToUTC.UTC().Format(time.RFC3339Nano))
	} else {
		parts = append(parts, "")
	}
	parts = append(parts, f.Status, f.Query)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}
func encodeNotesCursor(t time.Time, id int64, h string) *string {
	b, _ := json.Marshal(notesCursor{CreatedAt: t, ID: id, FilterHash: h, Version: 1})
	s := base64.RawURLEncoding.EncodeToString(b)
	return &s
}
func nextCursorTodayNotes(v []services.TodayNote, l int, h string) *string {
	if len(v) <= l {
		return nil
	}
	x := v[l-1]
	return encodeNotesCursor(x.CreatedAt, x.ID, h)
}

func page(r *http.Request) (int, *store.PageCursor, bool) {
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		v, e := strconv.Atoi(raw)
		if e != nil || v < 1 || v > 100 {
			return 0, nil, false
		}
		limit = v
	}
	raw := r.URL.Query().Get("cursor")
	if raw == "" {
		return limit, nil, true
	}
	b, e := base64.RawURLEncoding.DecodeString(raw)
	if e != nil {
		return 0, nil, false
	}
	var c store.PageCursor
	if json.Unmarshal(b, &c) != nil || c.ID <= 0 || c.CreatedAt.IsZero() {
		return 0, nil, false
	}
	return limit, &c, true
}
func encodeCursor(t time.Time, id int64) *string {
	b, _ := json.Marshal(store.PageCursor{CreatedAt: t, ID: id})
	s := base64.RawURLEncoding.EncodeToString(b)
	return &s
}
func nextCursorNotes(v []store.APINote, l int) *string {
	if len(v) <= l {
		return nil
	}
	x := v[l-1]
	return encodeCursor(x.CreatedAt, x.ID)
}
func nextCursorActions(v []store.APIAction, l int) *string {
	if len(v) <= l {
		return nil
	}
	x := v[l-1]
	return encodeCursor(x.CreatedAt, x.ID)
}
func decode(w http.ResponseWriter, r *http.Request, d any) string {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(d); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeError(w, r, 413, "request_too_large", "The request is too large.")
			return "large"
		}
		validation(w, r)
		return "invalid"
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		validation(w, r)
		return "invalid"
	}
	return ""
}
func validation(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, 400, "validation_error", "The request is invalid.")
}
func validationFields(w http.ResponseWriter, r *http.Request, fields map[string]string) {
	writeErrorWithFields(w, r, 400, "validation_error", "The request is invalid.", fields)
}
func internal(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, 500, "internal_error", "An internal error occurred.")
}
func writeError(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	writeErrorWithFields(w, r, status, code, msg, nil)
}
func writeErrorWithFields(w http.ResponseWriter, r *http.Request, status int, code, msg string, fields map[string]string) {
	err := map[string]any{"code": code, "message": msg, "request_id": requestID(r)}
	if len(fields) > 0 {
		err["fields"] = fields
	}
	writeJSON(w, status, map[string]any{"error": err})
}
func requestID(r *http.Request) string { s, _ := r.Context().Value(requestIDKey).(string); return s }
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type peopleCursor struct {
	LastMentionedAt time.Time `json:"last_mentioned_at"`
	PersonID        int64     `json:"person_id"`
	FilterHash      string    `json:"filter_hash"`
	Version         int       `json:"v"`
}

func (a *API) people(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	limit, filter, hash, cursor, ok := peoplePage(r)
	if !ok {
		validation(w, r)
		return
	}
	v, err := a.service.ListPeople(r.Context(), p.UserID, filter, limit+1, cursor)
	if err != nil {
		internal(w, r)
		return
	}
	hasMore := len(v.Items) > limit
	next := nextCursorPeople(v.Items, limit, hash)
	if hasMore {
		v.Items = v.Items[:limit]
	}
	out := dto.PeopleList{Items: []dto.PeopleListItem{}, Page: dto.NotesPage{NextCursor: next, HasMore: hasMore}}
	for _, x := range v.Items {
		out.Items = append(out.Items, mapPeopleListItem(x))
	}
	a.logger.Info("people list completed", "component", "http_api", "operation", "people.list", "operation_id", requestID(r), "result", "success", "result_count", len(out.Items), "has_more", hasMore, "has_search_filter", filter.Query != "", "has_open_actions_filter", filter.HasOpenActions)
	writeJSON(w, 200, map[string]any{"data": out})
}

func (a *API) personDetail(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	id, ok := parsePublicID(r.PathValue("id"), "person")
	if !ok {
		validation(w, r)
		return
	}
	v, err := a.service.GetPersonWorkspace(r.Context(), p.UserID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, 404, "not_found", "The resource was not found.")
		return
	}
	if err != nil {
		internal(w, r)
		return
	}
	out := dto.PersonWorkspaceDetail{Person: dto.PersonProfile{ID: publicID("person", v.Person.PersonID), DisplayName: v.Person.Name, Aliases: boundedAliases(v.Person.Aliases), FirstMentionedAt: v.Person.FirstMentionedAt, LastMentionedAt: v.Person.LastMentionedAt, MentionCount: v.Person.MentionCount}, OpenActions: []dto.Action{}, RecentDecisions: []dto.Decision{}, RecentNotes: []dto.PeopleRecentNote{}, Page: dto.NotesPage{NextCursor: nil, HasMore: false}}
	for _, x := range v.OpenActions {
		out.OpenActions = append(out.OpenActions, mapAction(x))
	}
	for _, x := range v.RecentDecisions {
		out.RecentDecisions = append(out.RecentDecisions, dto.Decision{ID: publicID("decision", x.ID), Text: x.Text, Status: x.Status, Topic: x.Topic})
	}
	for _, x := range v.RecentNotes {
		out.RecentNotes = append(out.RecentNotes, mapPeopleRecentNote(x, true))
	}
	a.logger.Info("person detail completed", "component", "http_api", "operation", "people.detail", "operation_id", requestID(r), "result", "success", "open_action_count", len(out.OpenActions), "decision_count", len(out.RecentDecisions), "recent_note_count", len(out.RecentNotes))
	writeJSON(w, 200, map[string]any{"data": out})
}

func mapPeopleListItem(x store.PeopleListItem) dto.PeopleListItem {
	return dto.PeopleListItem{ID: publicID("person", x.PersonID), DisplayName: x.Name, Aliases: boundedAliases(x.Aliases), LastMentionedAt: x.LastMentionedAt, MentionCount: x.MentionCount, OpenActionCount: x.OpenActionCount, RecentNote: mapPeopleRecentNotePtr(x.RecentNote, false)}
}
func mapPeopleRecentNotePtr(x *store.PeopleRecentNote, includeRaw bool) *dto.PeopleRecentNote {
	if x == nil {
		return nil
	}
	y := mapPeopleRecentNote(*x, includeRaw)
	return &y
}
func mapPeopleRecentNote(x store.PeopleRecentNote, includeRaw bool) dto.PeopleRecentNote {
	y := dto.PeopleRecentNote{ID: publicID("note", x.ID), CreatedAt: x.CreatedAt, Summary: x.Summary, ProcessingStatus: x.ProcessingStatus, Tickets: boundedStrings(x.Tickets, 3)}
	if includeRaw {
		y.RawTextPreview = runePreview(x.RawText, 400)
	}
	return y
}
func boundedAliases(in []string) []string { return boundedStrings(in, 10) }
func boundedStrings(in []string, n int) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, s := range in {
		k := strings.ToLower(strings.TrimSpace(s))
		if k == "" || seen[k] {
			continue
		}
		out = append(out, strings.TrimSpace(s))
		seen[k] = true
		if len(out) >= n {
			break
		}
	}
	return out
}

func peoplePage(r *http.Request) (int, services.PeopleListFilter, string, *store.PeoplePageCursor, bool) {
	q := r.URL.Query()
	limit := 20
	if raw := q.Get("limit"); raw != "" {
		v, e := strconv.Atoi(raw)
		if e != nil || v < 1 || v > 50 {
			return 0, services.PeopleListFilter{}, "", nil, false
		}
		limit = v
	}
	var f services.PeopleListFilter
	if raw := strings.TrimSpace(q.Get("query")); raw != "" {
		raw = strings.Join(strings.Fields(raw), " ")
		if len([]rune(raw)) < 2 || len([]rune(raw)) > 100 {
			return 0, f, "", nil, false
		}
		f.Query = raw
	}
	if raw := q.Get("has_open_actions"); raw != "" {
		if raw != "true" && raw != "false" {
			return 0, f, "", nil, false
		}
		f.HasOpenActions = raw == "true"
	}
	h := peopleFilterHash(f)
	var c *store.PeoplePageCursor
	if raw := q.Get("cursor"); raw != "" {
		b, e := base64.RawURLEncoding.DecodeString(raw)
		if e != nil {
			return 0, f, "", nil, false
		}
		var pc peopleCursor
		if json.Unmarshal(b, &pc) != nil || pc.Version != 1 || pc.PersonID <= 0 || pc.LastMentionedAt.IsZero() || pc.FilterHash != h {
			return 0, f, "", nil, false
		}
		c = &store.PeoplePageCursor{LastMentionedAt: pc.LastMentionedAt, PersonID: pc.PersonID}
	}
	return limit, f, h, c, true
}
func peopleFilterHash(f services.PeopleListFilter) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("v1\x00%s\x00%t", f.Query, f.HasOpenActions)))
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}
func nextCursorPeople(v []store.PeopleListItem, l int, h string) *string {
	if len(v) <= l {
		return nil
	}
	x := v[l-1]
	b, _ := json.Marshal(peopleCursor{LastMentionedAt: x.LastMentionedAt, PersonID: x.PersonID, FilterHash: h, Version: 1})
	s := base64.RawURLEncoding.EncodeToString(b)
	return &s
}
