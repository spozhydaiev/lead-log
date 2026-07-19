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
	user           store.WebUser
	notes          []store.APINote
	actions        []store.APIAction
	created        bool
	registeredUser store.CurrentUser
}

func (f *fakeService) Register(_ context.Context, in services.RegisterInput, _ services.AuthConfig) (services.AuthSession, error) {
	if in.ResponseLanguage == "bad" {
		return services.AuthSession{}, services.ValidationError{Fields: map[string]string{"response_language": "Choose a supported response language."}}
	}
	if len([]rune(in.DisplayName)) > services.MaxDisplayNameLength {
		return services.AuthSession{}, services.ValidationError{Fields: map[string]string{"display_name": "Display name is too long."}}
	}
	dn := strings.TrimSpace(in.DisplayName)
	email := strings.TrimSpace(in.Email)
	tz := in.Timezone
	if tz == "" {
		tz = "Europe/Warsaw"
	}
	lang := in.ResponseLanguage
	if lang == "" {
		lang = "en"
	}
	f.registeredUser = store.CurrentUser{ID: 7, Email: &email, DisplayName: &dn, Timezone: tz, ResponseLanguage: lang}
	return services.AuthSession{User: f.registeredUser, Token: "top-secret-token", ExpiresAt: time.Now().Add(time.Hour)}, nil
}
func (f *fakeService) Login(context.Context, services.LoginInput, services.AuthConfig) (services.AuthSession, error) {
	return services.AuthSession{}, nil
}
func (f *fakeService) SessionByToken(_ context.Context, token string, _ time.Time) (store.CurrentUser, error) {
	if token != "top-secret-token" {
		return store.CurrentUser{}, pgx.ErrNoRows
	}
	if f.registeredUser.ID != 0 {
		return f.registeredUser, nil
	}
	return store.CurrentUser{ID: f.user.ID, Timezone: "UTC", ResponseLanguage: "en"}, nil
}
func (f *fakeService) RevokeSession(context.Context, string) error { return nil }
func (f *fakeService) CurrentUser(context.Context, int64) (store.CurrentUser, error) {
	if f.registeredUser.ID != 0 {
		return f.registeredUser, nil
	}
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
func (f *fakeService) ListNotesHistory(_ context.Context, _ int64, _ services.NotesHistoryFilter, _ int, _ *store.PageCursor) (services.NotesHistoryView, error) {
	out := services.NotesHistoryView{Notes: []services.TodayNote{}}
	for _, n := range f.notes {
		out.Notes = append(out.Notes, services.TodayNote{APINote: n})
	}
	return out, nil
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

func TestNotesHistoryContract(t *testing.T) {
	summary := "Retry policy discussion."
	long := strings.Repeat("🙂", 450)
	svc := &fakeService{user: store.WebUser{ID: 7}, notes: []store.APINote{
		{ID: 2, RawText: long, Summary: &summary, ProcessingStatus: store.NoteProcessingStatusProcessed, CreatedAt: time.Date(2026, 7, 17, 11, 24, 0, 0, time.UTC)},
	}}
	h := New(svc, &pinger{}, Config{SessionCookieName: "lead_log_session"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w := request(h, "GET", "/api/v1/notes?limit=20&from=2026-07-01&to=2026-07-17&status=processed&query=retry", "", "top-secret-token")
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{`"items"`, `"page"`, `"has_more":false`, `"id":"note_2"`, `"extracted_counts"`, `"preview"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in %s", want, body)
		}
	}
	if strings.Count(body, "🙂") > 400 || !utf8.ValidString(body) {
		t.Fatalf("preview not bounded/valid: %s", body)
	}
}

func TestNotesHistoryValidation(t *testing.T) {
	h := testAPI(t, &pinger{}, io.Discard)
	for _, path := range []string{
		"/api/v1/notes?limit=0", "/api/v1/notes?limit=51", "/api/v1/notes?from=2026-02-30", "/api/v1/notes?from=2026-07-18&to=2026-07-17", "/api/v1/notes?status=done", "/api/v1/notes?query=x", "/api/v1/notes?cursor=bad",
	} {
		if w := request(h, "GET", path, "", "top-secret-token"); w.Code != 400 {
			t.Fatalf("%s status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
}

func TestRegisterContract(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"minimal registration", `{"email":"user@example.com","password":"Twelvesymbolspassword!11"}`, []string{`"timezone":"Europe/Warsaw"`, `"response_language":"en"`}},
		{"display_name only", `{"email":"user@example.com","password":"Twelvesymbolspassword!11","display_name":"Sergio"}`, []string{`"display_name":"Sergio"`}},
		{"timezone only", `{"email":"user@example.com","password":"Twelvesymbolspassword!11","timezone":"Europe/Warsaw"}`, []string{`"timezone":"Europe/Warsaw"`}},
		{"response_language only", `{"email":"user@example.com","password":"Twelvesymbolspassword!11","response_language":"en"}`, []string{`"response_language":"en"`}},
		{"all optional fields", `{"email":"user@example.com","password":"Twelvesymbolspassword!11","display_name":"Sergio","timezone":"Europe/Warsaw","response_language":"en"}`, []string{`"display_name":"Sergio"`, `"timezone":"Europe/Warsaw"`, `"response_language":"en"`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeService{user: store.WebUser{ID: 7}}
			h := New(svc, &pinger{}, Config{AllowedOrigins: []string{"http://localhost:3000"}, SessionCookieName: "lead_log_session"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			w := request(h, "POST", "/api/v1/auth/register", tc.body, "")
			if w.Code != 201 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			body := w.Body.String()
			for _, want := range tc.want {
				if !strings.Contains(body, want) {
					t.Fatalf("missing %s in %s", want, body)
				}
			}
			if strings.Contains(body, "Twelvesymbolspassword") || strings.Contains(body, "password") {
				t.Fatalf("password leaked in response: %s", body)
			}
			sw := request(h, "GET", "/api/v1/auth/session", "", "top-secret-token")
			if sw.Code != 200 {
				t.Fatalf("session status=%d body=%s", sw.Code, sw.Body.String())
			}
			for _, want := range tc.want {
				if !strings.Contains(sw.Body.String(), want) {
					t.Fatalf("session missing %s in %s", want, sw.Body.String())
				}
			}
		})
	}
}

func TestRegisterValidationContract(t *testing.T) {
	var logs bytes.Buffer
	h := testAPI(t, &pinger{}, &logs)
	if w := request(h, "POST", "/api/v1/auth/register", `{"email":"user@example.com","password":"Twelvesymbolspassword!11","unexpected":true}`, ""); w.Code != 400 {
		t.Fatalf("unknown field status=%d body=%s", w.Code, w.Body.String())
	}
	if w := request(h, "POST", "/api/v1/auth/register", `{"email":"user@example.com","password":"Twelvesymbolspassword!11","response_language":"bad"}`, ""); w.Code != 400 || !strings.Contains(w.Body.String(), `"response_language"`) || strings.Contains(w.Body.String(), "Twelvesymbolspassword") {
		t.Fatalf("invalid language response: status=%d body=%s", w.Code, w.Body.String())
	}
	longName := strings.Repeat("a", services.MaxDisplayNameLength+1)
	if w := request(h, "POST", "/api/v1/auth/register", `{"email":"user@example.com","password":"Twelvesymbolspassword!11","display_name":"`+longName+`"}`, ""); w.Code != 400 || !strings.Contains(w.Body.String(), `"display_name"`) || strings.Contains(w.Body.String(), "Twelvesymbolspassword") {
		t.Fatalf("long display response: status=%d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(logs.String(), "Twelvesymbolspassword") || strings.Contains(logs.String(), "password") {
		t.Fatalf("password leaked in logs: %s", logs.String())
	}
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
