package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/spozhydaiev/lead-log/internal/adapters/store"
	"github.com/spozhydaiev/lead-log/internal/services"
)

type fakeService struct {
	user    store.WebUser
	notes   []store.APINote
	actions []store.APIAction
	created bool
}

func (f *fakeService) Register(context.Context, services.RegisterInput, services.AuthConfig) (services.AuthSession, error) {
	return services.AuthSession{}, nil
}
func (f *fakeService) Login(context.Context, services.LoginInput, services.AuthConfig) (services.AuthSession, error) {
	return services.AuthSession{}, nil
}
func (f *fakeService) SessionByToken(_ context.Context, token string, _ time.Time) (store.CurrentUser, error) {
	if token != "top-secret-token" {
		return store.CurrentUser{}, pgx.ErrNoRows
	}
	return store.CurrentUser{ID: f.user.ID, Timezone: "UTC", ResponseLanguage: "en"}, nil
}
func (f *fakeService) RevokeSession(context.Context, string) error { return nil }
func (f *fakeService) CurrentUser(context.Context, int64) (store.CurrentUser, error) {
	return store.CurrentUser{ID: f.user.ID, Timezone: "UTC", ResponseLanguage: "en"}, nil
}
func (f *fakeService) TelegramStatus(context.Context, int64) (store.TelegramStatus, error) {
	return store.TelegramStatus{}, nil
}
func (f *fakeService) CreateTelegramLink(context.Context, int64, time.Duration) (string, time.Time, error) {
	return "tok", time.Now().Add(time.Minute), nil
}
func (f *fakeService) UnlinkTelegram(context.Context, int64) error { return nil }
func (f *fakeService) CreatePendingNote(_ context.Context, uid int64, text string) (store.APINote, error) {
	f.created = true
	return store.APINote{ID: 9, ProcessingStatus: "pending", CreatedAt: time.Now()}, nil
}
func (f *fakeService) ListNotes(context.Context, int64, int, *store.PageCursor) ([]store.APINote, error) {
	return f.notes, nil
}
func (f *fakeService) ListActions(context.Context, int64, string, int, *store.PageCursor) ([]store.APIAction, error) {
	return f.actions, nil
}
func (f *fakeService) SetActionStatus(_ context.Context, _ int64, id int64, status string) (store.APIAction, error) {
	if id == 404 {
		return store.APIAction{}, pgx.ErrNoRows
	}
	return store.APIAction{ID: id, Status: status, CreatedAt: time.Now()}, nil
}
func (f *fakeService) GetToday(context.Context, int64, time.Time) (services.TodayView, error) {
	return services.TodayView{Date: "2026-07-17", Timezone: "Europe/Warsaw", Notes: []services.TodayNote{}, OpenActions: []store.APIAction{}, DailySummary: services.TodayDailySummary{Status: "not_available"}}, nil
}
func (f *fakeService) GetNoteDetail(context.Context, int64, int64) (services.NoteDetailView, error) {
	return services.NoteDetailView{}, pgx.ErrNoRows
}

type pinger struct {
	err   error
	calls int
}

func TestTodayAndNoteDetailRoutes(t *testing.T) {
	h := testAPI(t, &pinger{}, io.Discard)
	if w := request(h, "GET", "/api/v1/today", "", "top-secret-token"); w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"not_available"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(h, "GET", "/api/v1/today?user_id=99", "", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request(h, "GET", "/api/v1/notes/bad_1", "", "top-secret-token"); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := request(h, "GET", "/api/v1/notes/note_1", "", "top-secret-token"); w.Code != 404 {
		t.Fatal(w.Code)
	}
}
func TestRunePreviewUTF8(t *testing.T) {
	s := strings.Repeat("🙂", 401)
	got := runePreview(s, 400)
	if len([]rune(got)) != 400 || !utf8.ValidString(got) {
		t.Fatal("invalid preview")
	}
}

func (p *pinger) Ping(context.Context) error { p.calls++; return p.err }
func testAPI(t *testing.T, db *pinger, log io.Writer) http.Handler {
	t.Helper()
	return New(&fakeService{user: store.WebUser{ID: 7}}, db, Config{AllowedOrigins: []string{"http://localhost:3000"}, ResponseLanguage: "en", Timezone: "UTC", SessionCookieName: "lead_log_session"}, slog.New(slog.NewTextHandler(log, nil)))
}
func request(h http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "lead_log_session", Value: token})
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if method == "POST" || method == "PATCH" || method == "DELETE" {
		r.Header.Set("Origin", "http://localhost:3000")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestHealthAndReadiness(t *testing.T) {
	p := &pinger{}
	h := testAPI(t, p, io.Discard)
	if w := request(h, "GET", "/healthz", "", ""); w.Code != 200 || p.calls != 0 {
		t.Fatalf("health=%d calls=%d", w.Code, p.calls)
	}
	if w := request(h, "GET", "/readyz", "", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	p.err = errors.New("password=secret SQL failed")
	w := request(h, "GET", "/readyz", "", "")
	if w.Code != 503 || strings.Contains(w.Body.String(), "secret") {
		t.Fatal(w.Body.String())
	}
}
func TestAuthenticationCORSAndSafeMe(t *testing.T) {
	var logs bytes.Buffer
	h := testAPI(t, &pinger{}, &logs)
	for _, token := range []string{"", "wrong"} {
		if w := request(h, "GET", "/api/v1/me", "", token); w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/api/v1/me", nil)
	r.AddCookie(&http.Cookie{Name: "lead_log_session", Value: "top-secret-token"})
	r.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Access-Control-Allow-Origin") == "" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Header())
	}
	body := w.Body.String()
	if strings.Contains(body, "123") || strings.Contains(body, "\"id\"") {
		t.Fatal(body)
	}
	if strings.Contains(logs.String(), "top-secret-token") || strings.Contains(logs.String(), "Authorization") {
		t.Fatal(logs.String())
	}
}
func TestCreateValidationAndActionUpdate(t *testing.T) {
	h := testAPI(t, &pinger{}, io.Discard)
	if w := request(h, "POST", "/api/v1/notes", `{"text":"hello","user_id":99}`, "top-secret-token"); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := request(h, "POST", "/api/v1/notes", `{"text":"hello"}`, "top-secret-token"); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(h, "PATCH", "/api/v1/actions/action_3", `{"status":"done"}`, "top-secret-token"); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := request(h, "PATCH", "/api/v1/actions/action_404", `{"status":"done"}`, "top-secret-token"); w.Code != 404 {
		t.Fatal(w.Code)
	}
}
func TestCORSPreflightAndUnknownOrigin(t *testing.T) {
	h := testAPI(t, &pinger{}, io.Discard)
	r := httptest.NewRequest("OPTIONS", "/api/v1/notes", nil)
	r.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatal(w.Code)
	}
	r = httptest.NewRequest("OPTIONS", "/api/v1/notes", nil)
	r.Header.Set("Origin", "https://evil.test")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal(w.Code, w.Header())
	}
}
