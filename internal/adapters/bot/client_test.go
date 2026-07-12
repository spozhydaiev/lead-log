package bot

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/spozhydaiev/lead-log/internal/adapters/store"
	"github.com/spozhydaiev/lead-log/internal/config"
	"github.com/spozhydaiev/lead-log/internal/models"
)

func TestSplitCommand(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantCmd string
		wantArg string
	}{
		{name: "plain text", text: "remember this", wantCmd: "", wantArg: "remember this"},
		{name: "command without arg", text: "/daily", wantCmd: "/daily", wantArg: ""},
		{name: "command with arg", text: "/note follow up", wantCmd: "/note", wantArg: "follow up"},
		{name: "now command with arg", text: "/now follow up", wantCmd: "/now", wantArg: "follow up"},
		{name: "lowercases command", text: "/DAILY", wantCmd: "/daily", wantArg: ""},
		{name: "strips bot username", text: "/daily@LeadLogBot --refresh", wantCmd: "/daily", wantArg: "--refresh"},
		{name: "ask strips bot username", text: "/ask@LeadLogBot what happened", wantCmd: "/ask", wantArg: "what happened"},
		{name: "trims arg", text: "/done   123  ", wantCmd: "/done", wantArg: "123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCmd, gotArg := splitCommand(tt.text)
			if gotCmd != tt.wantCmd || gotArg != tt.wantArg {
				t.Fatalf("splitCommand(%q) = (%q, %q), want (%q, %q)", tt.text, gotCmd, gotArg, tt.wantCmd, tt.wantArg)
			}
		})
	}
}

func TestHelpTextDocumentsMVPCommands(t *testing.T) {
	help := models.LanguageEnglish.CommonMessages().HelpText
	for _, want := range []string{
		"/note <text> — quickly save a raw note without AI processing",
		"/now <text> — save and immediately structure a note",
		"/daily --refresh — regenerate the daily digest",
		"/weekly --refresh — regenerate the weekly digest",
		"/ask <question> — ask about your saved work history with source notes",
		"regular text without /note",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("help text does not contain %q\n%s", want, help)
		}
	}

	for _, removed := range []string{"/person", "/agenda", "/review", "/alias", "/merge", "/ticket"} {
		if strings.Contains(help, removed) {
			t.Fatalf("help text still exposes %q\n%s", removed, help)
		}
	}
}

func TestHandleMessageAskRoutesToAskService(t *testing.T) {
	fake := &fakeBotService{askResponse: "answer"}
	b := testBot(fake, map[int64]bool{100: true}, models.LanguageEnglish)
	var replies []string
	b.handleMessageWithReply(testContext(), testMessage(100, "/ask what did I do yesterday?"), func(chatID int64, text string) { replies = append(replies, text) })
	if fake.askCalls != 1 || fake.lastQuestion != "what did I do yesterday?" {
		t.Fatalf("Ask calls/question = %d/%q", fake.askCalls, fake.lastQuestion)
	}
	if fake.captureCalls != 0 || fake.addCalls != 0 {
		t.Fatalf("/ask must not capture notes: capture=%d add=%d", fake.captureCalls, fake.addCalls)
	}
	if len(replies) != 1 || replies[0] != "answer" {
		t.Fatalf("replies=%#v", replies)
	}
}

func TestHandleMessageAskRequiresText(t *testing.T) {
	for _, text := range []string{"/ask", "/ask    "} {
		fake := &fakeBotService{}
		b := testBot(fake, map[int64]bool{100: true}, models.LanguageEnglish)
		var replies []string
		b.handleMessageWithReply(testContext(), testMessage(100, text), func(chatID int64, text string) { replies = append(replies, text) })
		if fake.askCalls != 0 {
			t.Fatalf("Ask called for empty question")
		}
		if len(replies) != 1 || replies[0] != models.LanguageEnglish.CommonMessages().AskUsage {
			t.Fatalf("replies=%#v", replies)
		}
	}
}

func TestHandleMessageAskDuplicateSkipsLLMAndReply(t *testing.T) {
	fake := &fakeBotService{askResponse: "answer", claims: []store.TelegramUpdateClaim{{Claimed: true, Status: store.TelegramUpdateStatusProcessing, AttemptCount: 1, ProcessingStartedAt: time.Now()}, {Claimed: false, Status: store.TelegramUpdateStatusProcessed, AttemptCount: 1}}}
	b := testBot(fake, map[int64]bool{100: true}, models.LanguageEnglish)
	var replies []string
	msg := testMessage(100, "/ask@LeadLogBot question")
	b.handleMessageWithUpdateAndReply(testContext(), 558, msg, func(chatID int64, text string) { replies = append(replies, text) })
	b.handleMessageWithUpdateAndReply(testContext(), 558, msg, func(chatID int64, text string) { replies = append(replies, text) })
	if fake.askCalls != 1 {
		t.Fatalf("Ask calls=%d, want 1", fake.askCalls)
	}
	if len(replies) != 1 || replies[0] != "answer" {
		t.Fatalf("replies=%#v", replies)
	}
}

func TestHandleMessageRejectsUnauthorizedWithoutEnsuringUser(t *testing.T) {
	fake := &fakeBotService{}
	b := testBot(fake, map[int64]bool{100: true}, models.LanguageEnglish)
	var replies []string
	b.handleMessageWithReply(testContext(), testMessage(200, "/note hidden"), func(chatID int64, text string) { replies = append(replies, text) })
	if fake.ensureCalls != 0 {
		t.Fatalf("EnsureUser called %d times, want 0", fake.ensureCalls)
	}
	if len(replies) != 1 || replies[0] != models.LanguageEnglish.CommonMessages().AccessDenied {
		t.Fatalf("replies = %#v", replies)
	}
}

func TestHandleMessageNoteRequiresText(t *testing.T) {
	for _, text := range []string{"/note", "/note    "} {
		t.Run(text, func(t *testing.T) {
			fake := &fakeBotService{}
			b := testBot(fake, map[int64]bool{100: true}, models.LanguageEnglish)
			var replies []string
			b.handleMessageWithReply(testContext(), testMessage(100, text), func(chatID int64, text string) { replies = append(replies, text) })
			if fake.captureCalls != 0 {
				t.Fatalf("CaptureNote called %d times, want 0", fake.captureCalls)
			}
			if len(replies) != 1 || replies[0] != models.LanguageEnglish.CommonMessages().NoteUsage {
				t.Fatalf("replies = %#v", replies)
			}
		})
	}
}

func TestHandleMessageNoteWithTextStillCaptures(t *testing.T) {
	fake := &fakeBotService{captureResponse: "saved"}
	b := testBot(fake, map[int64]bool{100: true}, models.LanguageEnglish)
	var replies []string
	b.handleMessageWithReply(testContext(), testMessage(100, "/note follow up"), func(chatID int64, text string) { replies = append(replies, text) })
	if fake.captureCalls != 1 || fake.lastRaw != "follow up" {
		t.Fatalf("CaptureNote calls/raw = %d/%q, want 1/follow up", fake.captureCalls, fake.lastRaw)
	}
	if len(replies) != 1 || replies[0] != "saved" {
		t.Fatalf("replies = %#v", replies)
	}
}

func TestHandleMessageUnknownCommandsAreNotCaptured(t *testing.T) {
	for _, text := range []string{"/daliy", "/unknown test", "/unknown@LeadLogBot test"} {
		t.Run(text, func(t *testing.T) {
			fake := &fakeBotService{}
			b := testBot(fake, map[int64]bool{100: true}, models.LanguageEnglish)
			var replies []string
			b.handleMessageWithReply(testContext(), testMessage(100, text), func(chatID int64, text string) { replies = append(replies, text) })
			if fake.captureCalls != 0 {
				t.Fatalf("CaptureNote called %d times, want 0", fake.captureCalls)
			}
			if len(replies) != 1 || replies[0] != models.LanguageEnglish.CommonMessages().UnknownCommand {
				t.Fatalf("replies = %#v", replies)
			}
		})
	}
}

func TestHandleMessagePlainTextStillCaptures(t *testing.T) {
	fake := &fakeBotService{captureResponse: "saved"}
	b := testBot(fake, map[int64]bool{100: true}, models.LanguageEnglish)
	b.handleMessageWithReply(testContext(), testMessage(100, "follow up without slash"), func(chatID int64, text string) {})
	if fake.captureCalls != 1 || fake.lastRaw != "follow up without slash" {
		t.Fatalf("CaptureNote calls/raw = %d/%q", fake.captureCalls, fake.lastRaw)
	}
}

func TestHandleMessagePlainTextDuplicateSkipsSideEffectsAndReply(t *testing.T) {
	fake := &fakeBotService{captureResponse: "saved", claims: []store.TelegramUpdateClaim{
		{Claimed: true, Status: store.TelegramUpdateStatusProcessing, AttemptCount: 1, ProcessingStartedAt: time.Now()},
		{Claimed: false, Status: store.TelegramUpdateStatusProcessed, AttemptCount: 1},
	}}
	b := testBot(fake, map[int64]bool{100: true}, models.LanguageEnglish)
	var replies []string
	msg := testMessage(100, "follow up without slash")
	b.handleMessageWithUpdateAndReply(testContext(), 555, msg, func(chatID int64, text string) { replies = append(replies, text) })
	b.handleMessageWithUpdateAndReply(testContext(), 555, msg, func(chatID int64, text string) { replies = append(replies, text) })
	if fake.captureCalls != 1 {
		t.Fatalf("CaptureNote calls = %d, want 1", fake.captureCalls)
	}
	if len(replies) != 1 || replies[0] != "saved" {
		t.Fatalf("replies = %#v, want one saved", replies)
	}
}

func TestHandleMessageNowDuplicateSkipsLLMAndReply(t *testing.T) {
	fake := &fakeBotService{addResponse: "structured", claims: []store.TelegramUpdateClaim{
		{Claimed: true, Status: store.TelegramUpdateStatusProcessing, AttemptCount: 1, ProcessingStartedAt: time.Now()},
		{Claimed: false, Status: store.TelegramUpdateStatusProcessed, AttemptCount: 1},
	}}
	b := testBot(fake, map[int64]bool{100: true}, models.LanguageEnglish)
	var replies []string
	msg := testMessage(100, "/now follow up")
	b.handleMessageWithUpdateAndReply(testContext(), 556, msg, func(chatID int64, text string) { replies = append(replies, text) })
	b.handleMessageWithUpdateAndReply(testContext(), 556, msg, func(chatID int64, text string) { replies = append(replies, text) })
	if fake.addCalls != 1 {
		t.Fatalf("AddNote calls = %d, want 1", fake.addCalls)
	}
	if len(replies) != 1 || replies[0] != "structured" {
		t.Fatalf("replies = %#v, want one structured", replies)
	}
}

func TestHandleMessageDoneDuplicateSkipsSecondDoneAndReply(t *testing.T) {
	fake := &fakeBotService{doneResponse: "done", claims: []store.TelegramUpdateClaim{
		{Claimed: true, Status: store.TelegramUpdateStatusProcessing, AttemptCount: 1, ProcessingStartedAt: time.Now()},
		{Claimed: false, Status: store.TelegramUpdateStatusProcessed, AttemptCount: 1},
	}}
	b := testBot(fake, map[int64]bool{100: true}, models.LanguageEnglish)
	var replies []string
	msg := testMessage(100, "/done 42")
	b.handleMessageWithUpdateAndReply(testContext(), 557, msg, func(chatID int64, text string) { replies = append(replies, text) })
	b.handleMessageWithUpdateAndReply(testContext(), 557, msg, func(chatID int64, text string) { replies = append(replies, text) })
	if fake.doneCalls != 1 {
		t.Fatalf("Done calls = %d, want 1", fake.doneCalls)
	}
	if len(replies) != 1 || replies[0] != "done" {
		t.Fatalf("replies = %#v, want one done", replies)
	}
}

func TestHandleMessageServiceErrorUsesGenericReply(t *testing.T) {
	fake := &fakeBotService{captureErr: errTestService}
	b := testBot(fake, map[int64]bool{100: true}, models.LanguageEnglish)
	var replies []string
	b.handleMessageWithReply(testContext(), testMessage(100, "plain text"), func(chatID int64, text string) { replies = append(replies, text) })
	if len(replies) != 1 || replies[0] != models.LanguageEnglish.CommonMessages().GenericError {
		t.Fatalf("replies = %#v", replies)
	}
	if strings.Contains(replies[0], errTestService.Error()) || strings.Contains(replies[0], "SQL") {
		t.Fatalf("reply leaks internal error: %q", replies[0])
	}
}

var errTestService = errors.New("SQL timeout in internal function")

func testContext() context.Context { return context.Background() }

func testBot(svc service, allowed map[int64]bool, language models.ResponseLanguage) *Bot {
	return &Bot{cfg: config.Config{AllowedTelegramUserIDs: allowed, ResponseLanguage: language}, svc: svc, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func testMessage(userID int64, text string) *tgbotapi.Message {
	return &tgbotapi.Message{MessageID: 1, From: &tgbotapi.User{ID: userID, UserName: "tester"}, Chat: &tgbotapi.Chat{ID: userID}, Text: text}
}

type fakeBotService struct {
	ensureCalls     int
	captureCalls    int
	lastRaw         string
	captureResponse string
	captureErr      error
	addCalls        int
	addResponse     string
	doneCalls       int
	doneResponse    string
	claims          []store.TelegramUpdateClaim
	askCalls        int
	askResponse     string
	lastQuestion    string
}

func (f *fakeBotService) EnsureUser(ctx context.Context, telegramUserID int64, username string) (int64, error) {
	f.ensureCalls++
	return telegramUserID + 10, nil
}
func (f *fakeBotService) CaptureNote(ctx context.Context, userID int64, raw string) (string, error) {
	f.captureCalls++
	f.lastRaw = raw
	if f.captureErr != nil {
		return "", f.captureErr
	}
	if f.captureResponse != "" {
		return f.captureResponse, nil
	}
	return "saved", nil
}
func (f *fakeBotService) AddNote(ctx context.Context, userID int64, raw string) (string, error) {
	f.addCalls++
	if f.addResponse != "" {
		return f.addResponse, nil
	}
	return "", nil
}
func (f *fakeBotService) OpenActions(ctx context.Context, userID int64) (string, error) {
	return "", nil
}
func (f *fakeBotService) Done(ctx context.Context, userID int64, arg string) (string, error) {
	f.doneCalls++
	if f.doneResponse != "" {
		return f.doneResponse, nil
	}
	return "", nil
}
func (f *fakeBotService) Daily(ctx context.Context, userID int64, refresh bool) (string, error) {
	return "", nil
}
func (f *fakeBotService) Weekly(ctx context.Context, userID int64, refresh bool) (string, error) {
	return "", nil
}
func (f *fakeBotService) Ask(ctx context.Context, userID int64, question string) (string, error) {
	f.askCalls++
	f.lastQuestion = question
	if f.askResponse != "" {
		return f.askResponse, nil
	}
	return "", nil
}
func (f *fakeBotService) ClaimTelegramUpdate(ctx context.Context, meta store.TelegramUpdateMeta, staleAfter time.Duration) (store.TelegramUpdateClaim, error) {
	if len(f.claims) > 0 {
		c := f.claims[0]
		f.claims = f.claims[1:]
		return c, nil
	}
	return store.TelegramUpdateClaim{Claimed: true, Status: store.TelegramUpdateStatusProcessing, AttemptCount: 1, ProcessingStartedAt: time.Now()}, nil
}
func (f *fakeBotService) MarkTelegramUpdateProcessed(ctx context.Context, meta store.TelegramUpdateMeta, startedAt time.Time) error {
	return nil
}
func (f *fakeBotService) MarkTelegramUpdateFailed(ctx context.Context, meta store.TelegramUpdateMeta, startedAt time.Time, cause error) error {
	return nil
}
func (f *fakeBotService) CaptureNoteForTelegramUpdate(ctx context.Context, userID int64, raw string, meta store.TelegramUpdateMeta, startedAt time.Time) (string, error) {
	return f.CaptureNote(ctx, userID, raw)
}
func (f *fakeBotService) AddNoteForTelegramUpdate(ctx context.Context, userID int64, raw string, meta store.TelegramUpdateMeta, startedAt time.Time) (string, error) {
	return f.AddNote(ctx, userID, raw)
}
func (f *fakeBotService) DoneForTelegramUpdate(ctx context.Context, userID int64, arg string, meta store.TelegramUpdateMeta, startedAt time.Time) (string, error) {
	return f.Done(ctx, userID, arg)
}
