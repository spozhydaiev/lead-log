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
	"github.com/spozhydaiev/lead-log/internal/apperrors"
	"github.com/spozhydaiev/lead-log/internal/services"
)

type fakeService struct {
	user           store.WebUser
	notes          []store.APINote
	actions        []store.APIAction
	created        bool
	registeredUser store.CurrentUser
	people         services.PeopleListView
	person         services.PersonWorkspaceView
	tickets        services.TicketsListView
	ticket         services.TicketWorkspaceView
	ticketErr      error
	askResp        services.AskResponse
	askErr         error
	summaries      services.SummaryListView
	summary        services.SummaryView
	generateResp   services.SummaryGenerateResult
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
func (f *fakeService) ListPeople(context.Context, int64, services.PeopleListFilter, int, *store.PeoplePageCursor) (services.PeopleListView, error) {
	return f.people, nil
}
func (f *fakeService) GetPersonWorkspace(_ context.Context, _ int64, id int64) (services.PersonWorkspaceView, error) {
	if id == 404 {
		return services.PersonWorkspaceView{}, pgx.ErrNoRows
	}
	return f.person, nil
}
func (f *fakeService) ListTickets(context.Context, int64, services.TicketsListFilter, int, *store.TicketsPageCursor) (services.TicketsListView, error) {
	return f.tickets, nil
}

func (f *fakeService) ListSummaries(context.Context, int64, services.SummaryFilter, int, *services.SummaryCursor) (services.SummaryListView, error) {
	return f.summaries, nil
}
func (f *fakeService) GetSummary(_ context.Context, _ int64, id int64) (services.SummaryView, error) {
	if id == 404 {
		return services.SummaryView{}, pgx.ErrNoRows
	}
	if f.summary.ID != "" {
		return f.summary, nil
	}
	return services.SummaryView{}, pgx.ErrNoRows
}
func (f *fakeService) GenerateSummary(context.Context, int64, services.SummaryGenerateInput) (services.SummaryGenerateResult, error) {
	return f.generateResp, nil
}

func (f *fakeService) AskAPI(context.Context, int64, string, time.Time, string) (services.AskResponse, error) {
	if f.askErr != nil {
		return services.AskResponse{}, f.askErr
	}
	if f.askResp.Answer != "" {
		return f.askResp, nil
	}
	return services.AskResponse{Answer: "ok", Confidence: "grounded"}, nil
}

func (f *fakeService) GetTicketWorkspace(_ context.Context, _ int64, key string) (services.TicketWorkspaceView, error) {
	if key == "MISS-404" {
		return services.TicketWorkspaceView{}, pgx.ErrNoRows
	}
	if f.ticketErr != nil {
		return services.TicketWorkspaceView{}, f.ticketErr
	}
	return f.ticket, nil
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

func TestPeopleWorkspaceRoutes(t *testing.T) {
	summary := "Discussed retry ownership."
	long := strings.Repeat("🙂", 450)
	now := time.Date(2026, 7, 18, 13, 42, 0, 0, time.UTC)
	svc := &fakeService{user: store.WebUser{ID: 7}, people: services.PeopleListView{Items: []store.PeopleListItem{{PersonID: 12, Name: "Adlet", Aliases: []string{"Адлет", "adlet", "Адлет"}, LastMentionedAt: now, MentionCount: 14, OpenActionCount: 2, RecentNote: &store.PeopleRecentNote{ID: 184, CreatedAt: now, Summary: &summary, RawText: long, ProcessingStatus: "processed"}}}}, person: services.PersonWorkspaceView{Person: store.PersonProfile{PersonID: 12, Name: "Adlet", Aliases: []string{"Адлет"}, FirstMentionedAt: now.AddDate(0, -1, 0), LastMentionedAt: now, MentionCount: 14}, OpenActions: []store.APIAction{{ID: 31, Title: "Follow up", Status: "open", CreatedAt: now}}, RecentDecisions: []store.DecisionView{{ID: 7, Text: "Use retry policy", Status: "active", Topic: "architecture"}}, RecentNotes: []store.PeopleRecentNote{{ID: 184, CreatedAt: now, Summary: &summary, RawText: long, ProcessingStatus: "processed"}}}}
	h := New(svc, &pinger{}, Config{SessionCookieName: "lead_log_session"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w := request(h, "GET", "/api/v1/people?limit=20&query=Adlet&has_open_actions=true", "", "top-secret-token")
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{`"items"`, `"id":"person_12"`, `"display_name":"Adlet"`, `"aliases":["Адлет","adlet"]`, `"open_action_count":2`, `"recent_note"`, `"has_more":false`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in %s", want, body)
		}
	}
	if strings.Contains(body, "🙂") {
		t.Fatalf("list leaked raw note: %s", body)
	}
	w = request(h, "GET", "/api/v1/people/person_12", "", "top-secret-token")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
	}
	body = w.Body.String()
	for _, want := range []string{`"person"`, `"open_actions"`, `"recent_decisions"`, `"recent_notes"`, `"id":"action_31"`, `"id":"decision_7"`, `"id":"note_184"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in %s", want, body)
		}
	}
	if strings.Count(body, "🙂") > 400 || !utf8.ValidString(body) {
		t.Fatalf("detail preview invalid: %s", body)
	}
}

func TestPeopleWorkspaceValidationAndLogging(t *testing.T) {
	var logs bytes.Buffer
	h := testAPI(t, &pinger{}, &logs)
	for _, path := range []string{"/api/v1/people?limit=0", "/api/v1/people?limit=51", "/api/v1/people?query=x", "/api/v1/people?has_open_actions=yes", "/api/v1/people?cursor=bad", "/api/v1/people/bad_1"} {
		if w := request(h, "GET", path, "", "top-secret-token"); w.Code != 400 {
			t.Fatalf("%s status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
	if w := request(h, "GET", "/api/v1/people/person_404", "", "top-secret-token"); w.Code != 404 {
		t.Fatalf("detail status=%d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(logs.String(), "Adlet") || strings.Contains(logs.String(), "query=x") || strings.Contains(logs.String(), "cursor=bad") {
		t.Fatalf("private data leaked in logs: %s", logs.String())
	}
}

func TestTicketsRoutesValidationAndNoStore(t *testing.T) {
	svc := &fakeService{ticket: services.TicketWorkspaceView{Ticket: store.TicketProfile{Key: "CH-1234", FirstMentionedAt: time.Unix(1, 0), LastMentionedAt: time.Unix(2, 0), MentionCount: 1}}}
	h := New(svc, &pinger{}, Config{Now: time.Now}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if w := request(h, "GET", "/api/v1/tickets/ch-1234", "", "top-secret-token"); w.Code != 200 || !strings.Contains(w.Body.String(), `"key":"CH-1234"`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String(), w.Header().Get("Cache-Control"))
	}
	for _, path := range []string{"/api/v1/tickets/bad", "/api/v1/tickets/å-123", "/api/v1/tickets/CH-1234%27"} {
		if w := request(h, "GET", path, "", "top-secret-token"); w.Code != 400 {
			t.Fatalf("%s got %d", path, w.Code)
		}
	}
	if w := request(h, "GET", "/api/v1/tickets?query=x", "", "top-secret-token"); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := request(h, "GET", "/api/v1/tickets?cursor=bad", "", "top-secret-token"); w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestTicketDetailUnexpectedErrorLogsSanitizedDiagnosticOnce(t *testing.T) {
	var logs bytes.Buffer
	internalErr := apperrors.Wrap("ticket_repository.list_recent_notes", apperrors.ClassDatabaseScan, errors.New("scan recent ticket note: sql: Scan error on private value"))
	svc := &fakeService{ticketErr: internalErr}
	h := New(svc, &pinger{}, Config{SessionCookieName: "lead_log_session", Now: time.Now}, slog.New(slog.NewTextHandler(&logs, nil)))
	w := request(h, "GET", "/api/v1/tickets/secret-123", "", "top-secret-token")
	if w.Code != 500 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"code":"internal_error"`) || !strings.Contains(body, `"message":"An internal error occurred."`) {
		t.Fatalf("unsafe or missing client error: %s", body)
	}
	if strings.Contains(body, "Scan error") || strings.Contains(body, "ticket_repository") || strings.Contains(body, "secret-123") {
		t.Fatalf("internal error leaked to client: %s", body)
	}
	out := logs.String()
	if strings.Count(out, "level=ERROR") != 1 {
		t.Fatalf("expected one error log, got logs:\n%s", out)
	}
	for _, want := range []string{"operation=tickets.detail", "route=/api/v1/tickets/{key}", "error_class=database_scan", "failing_operation=ticket_repository.list_recent_notes", "scan recent ticket note", "level=INFO", "operation=request", "status=500"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in logs:\n%s", want, out)
		}
	}
	if strings.Contains(out, "secret-123") || strings.Contains(out, "user_id") || strings.Contains(out, "top-secret-token") {
		t.Fatalf("private data leaked in logs: %s", out)
	}
	if got := w.Header().Get("X-Request-ID"); got == "" || !strings.Contains(out, "operation_id="+got) {
		t.Fatalf("operation id not shared, header=%q logs=%s", got, out)
	}
}

func TestTicketDetailExpectedErrorsAreNotUnexpectedErrorLogs(t *testing.T) {
	var logs bytes.Buffer
	svc := &fakeService{}
	h := New(svc, &pinger{}, Config{SessionCookieName: "lead_log_session", Now: time.Now}, slog.New(slog.NewTextHandler(&logs, nil)))
	if w := request(h, "GET", "/api/v1/tickets/bad", "", "top-secret-token"); w.Code != 400 {
		t.Fatalf("400 status=%d body=%s", w.Code, w.Body.String())
	}
	if w := request(h, "GET", "/api/v1/tickets/MISS-404", "", "top-secret-token"); w.Code != 404 {
		t.Fatalf("404 status=%d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(logs.String(), "level=ERROR") {
		t.Fatalf("expected errors logged as unexpected: %s", logs.String())
	}
}

func TestTicketReturnedByListCanBeOpenedByDetail(t *testing.T) {
	now := time.Unix(2, 0)
	svc := &fakeService{
		tickets: services.TicketsListView{Items: []store.TicketListItem{{Key: "CH-1234", FirstMentionedAt: time.Unix(1, 0), LastMentionedAt: now, MentionCount: 1}}},
		ticket:  services.TicketWorkspaceView{Ticket: store.TicketProfile{Key: "CH-1234", FirstMentionedAt: time.Unix(1, 0), LastMentionedAt: now, MentionCount: 1}, OpenActions: []store.APIAction{}, RecentDecisions: []store.DecisionView{}, RecentNotes: []store.TicketRecentNote{}},
	}
	h := New(svc, &pinger{}, Config{SessionCookieName: "lead_log_session", Now: time.Now}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	list := request(h, "GET", "/api/v1/tickets", "", "top-secret-token")
	if list.Code != 200 || !strings.Contains(list.Body.String(), `"key":"CH-1234"`) {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	detail := request(h, "GET", "/api/v1/tickets/CH-1234", "", "top-secret-token")
	if detail.Code != 200 || !strings.Contains(detail.Body.String(), `"key":"CH-1234"`) || !strings.Contains(detail.Body.String(), `"open_actions":[]`) {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}
}

func TestAskEndpointValidationAndSuccess(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	svc := &fakeService{user: store.WebUser{ID: 7}, askResp: services.AskResponse{Answer: "Grounded answer", Confidence: "grounded", Sources: []services.AskSource{{Type: "note", ID: "note_184", Label: "Discussion", Excerpt: "Short excerpt"}}}}
	h := New(svc, &pinger{}, Config{SessionCookieName: "lead_log_session", Now: func() time.Time { return now }, AllowedOrigins: []string{"http://localhost:3000"}}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	w := request(h, "POST", "/api/v1/ask", `{"question":"What did I do yesterday?"}`, "top-secret-token")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"answer":"Grounded answer"`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("ask success code=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
	}
	for _, body := range []string{`{"question":""}`, `{"question":"   "}`, `{"question":"x"}`} {
		w = request(h, "POST", "/api/v1/ask", body, "top-secret-token")
		if w.Code != 400 {
			t.Fatalf("body %s: got %d", body, w.Code)
		}
	}
	w = request(h, "POST", "/api/v1/ask", `{"question":"ok"}`, "")
	if w.Code != 401 {
		t.Fatalf("unauthenticated got %d", w.Code)
	}
}

func TestSummariesAPI(t *testing.T) {
	now := time.Date(2026, 7, 19, 18, 0, 4, 0, time.UTC)
	svc := &fakeService{user: store.WebUser{ID: 7}, summaries: services.SummaryListView{Items: []services.SummaryView{{ID: "summary_123", Type: "daily", Status: "ready", Period: services.SummaryPeriod{From: "2026-07-19", To: "2026-07-19"}, Title: "Daily summary for 2026-07-19", Preview: strings.Repeat("x", 10), GeneratedAt: now}}}, summary: services.SummaryView{ID: "summary_123", Type: "daily", Status: "ready", Period: services.SummaryPeriod{From: "2026-07-19", To: "2026-07-19"}, GeneratedAt: now, Content: map[string]any{"short_summary": "ok"}, Sources: []services.SummarySource{}}, generateResp: services.SummaryGenerateResult{Generated: false, CacheHit: false, Reason: "no_source_notes", Period: services.SummaryPeriod{From: "2026-07-19", To: "2026-07-19"}}}
	h := New(svc, nil, Config{AllowedOrigins: []string{"http://localhost:3000"}, Now: func() time.Time { return now }, SessionCookieName: "lead_log_session"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w := request(h, "GET", "/api/v1/summaries?type=daily&limit=20&from=2026-07-01&to=2026-07-31&status=ready", "", "top-secret-token")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"id":"summary_123"`) {
		t.Fatalf("list failed: code=%d body=%s", w.Code, w.Body.String())
	}
	for _, path := range []string{"/api/v1/summaries?type=monthly", "/api/v1/summaries?status=failed", "/api/v1/summaries?from=2026-07-31&to=2026-07-01", "/api/v1/summaries?cursor=bad"} {
		if w := request(h, "GET", path, "", "top-secret-token"); w.Code != 400 {
			t.Fatalf("expected bad request for %s, got %d", path, w.Code)
		}
	}
	if w := request(h, "GET", "/api/v1/summaries/summary_123", "", "top-secret-token"); w.Code != 200 || !strings.Contains(w.Body.String(), `"short_summary":"ok"`) {
		t.Fatalf("detail failed: %d %s", w.Code, w.Body.String())
	}
	if w := request(h, "GET", "/api/v1/summaries/summary_404", "", "top-secret-token"); w.Code != 404 {
		t.Fatalf("missing detail got %d", w.Code)
	}
	r := httptest.NewRequest("POST", "/api/v1/summaries/generate", strings.NewReader(`{"type":"daily","anchor_date":"2026-07-19","force":false}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://localhost:3000")
	r.AddCookie(&http.Cookie{Name: "lead_log_session", Value: "top-secret-token"})
	wr := httptest.NewRecorder()
	h.ServeHTTP(wr, r)
	if wr.Code != 200 || !strings.Contains(wr.Body.String(), `"reason":"no_source_notes"`) || wr.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("generate failed: %d %s", wr.Code, wr.Body.String())
	}
}
