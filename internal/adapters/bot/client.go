package bot

import (
	"context"
	"log/slog"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/spozhydaiev/lead-log/internal/config"
	svc "github.com/spozhydaiev/lead-log/internal/services"
	"github.com/spozhydaiev/lead-log/pkg/utils"
)

type Bot struct {
	api    *tgbotapi.BotAPI
	cfg    config.Config
	svc    service
	logger *slog.Logger
}

type service interface {
	EnsureUser(ctx context.Context, telegramUserID int64, username string) (int64, error)
	CaptureNote(ctx context.Context, userID int64, raw string) (string, error)
	AddNote(ctx context.Context, userID int64, raw string) (string, error)
	OpenActions(ctx context.Context, userID int64) (string, error)
	Done(ctx context.Context, userID int64, arg string) (string, error)
	Daily(ctx context.Context, userID int64, refresh bool) (string, error)
	Weekly(ctx context.Context, userID int64, refresh bool) (string, error)
}

func New(cfg config.Config, svc *svc.Service, logger ...*slog.Logger) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.TelegramBotToken)
	if err != nil {
		return nil, err
	}
	l := slog.Default()
	if len(logger) > 0 && logger[0] != nil {
		l = logger[0]
	}
	return &Bot{api: api, cfg: cfg, svc: svc, logger: l}, nil
}

func (b *Bot) Run(ctx context.Context) error {
	b.logger.Info("bot polling started", "operation", "bot.polling", "bot_username", b.api.Self.UserName)
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			b.logger.Info("bot polling stopped", "operation", "bot.polling")
			return ctx.Err()
		case update, ok := <-updates:
			if !ok {
				b.logger.Warn("Telegram update channel closed", "operation", "bot.polling")
				return nil
			}
			if update.Message == nil || update.Message.From == nil {
				continue
			}
			go b.handleMessage(ctx, update.Message)
		}
	}
}

func (b *Bot) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
	b.handleMessageWithReply(ctx, msg, b.reply)
}

func (b *Bot) handleMessageWithReply(ctx context.Context, msg *tgbotapi.Message, reply func(chatID int64, text string)) {
	telegramUserID := msg.From.ID
	if !config.IsTelegramUserAllowed(b.cfg.AllowedTelegramUserIDs, telegramUserID) {
		reply(msg.Chat.ID, b.cfg.ResponseLanguage.CommonMessages().AccessDenied)
		return
	}

	userID, err := b.svc.EnsureUser(ctx, telegramUserID, msg.From.UserName)
	if err != nil {
		b.logger.Error("command failed", "operation", "bot.ensure_user", "telegram_user_id", telegramUserID, "error", err)
		reply(msg.Chat.ID, b.cfg.ResponseLanguage.CommonMessages().UserInitFailed)
		return
	}

	text := strings.TrimSpace(msg.Text)
	if text == "" {
		b.logger.Info("incoming message", "operation", "bot.handle_message", "telegram_user_id", telegramUserID, "user_id", userID, "message_type", "unsupported")
		reply(msg.Chat.ID, b.cfg.ResponseLanguage.CommonMessages().UnsupportedText)
		return
	}

	cmd, arg := splitCommand(text)
	messageType := "plain_text"
	if cmd != "" {
		messageType = "command"
	}
	commandName := cmd
	if commandName == "" {
		commandName = "plain_text"
	}
	b.logger.Info("incoming message", "operation", "bot.handle_message", "telegram_user_id", telegramUserID, "user_id", userID, "message_type", messageType, "command", commandName, "note_length", len(arg))
	var response string
	switch cmd {
	case "/start", "/help":
		response = b.cfg.ResponseLanguage.CommonMessages().HelpText
	case "/note":
		if strings.TrimSpace(arg) == "" {
			response = b.cfg.ResponseLanguage.CommonMessages().NoteUsage
			break
		}
		response, err = b.svc.CaptureNote(ctx, userID, arg)
	case "/now":
		response, err = b.svc.AddNote(ctx, userID, arg)
	case "/open":
		response, err = b.svc.OpenActions(ctx, userID)
	case "/done":
		response, err = b.svc.Done(ctx, userID, arg)
	case "/daily":
		response, err = b.svc.Daily(ctx, userID, utils.HasRefreshFlag(arg))
	case "/weekly":
		response, err = b.svc.Weekly(ctx, userID, utils.HasRefreshFlag(arg))
	default:
		if cmd != "" {
			response = b.cfg.ResponseLanguage.CommonMessages().UnknownCommand
			break
		}
		// Treat normal text as a quick note to reduce capture friction.
		response, err = b.svc.CaptureNote(ctx, userID, text)
	}
	if err != nil {
		b.logger.Error("command failed", "operation", "bot.handle_message", "telegram_user_id", telegramUserID, "user_id", userID, "command", commandName, "error", err)
		reply(msg.Chat.ID, b.cfg.ResponseLanguage.CommonMessages().GenericError)
		return
	}
	reply(msg.Chat.ID, response)
	b.logger.Info("command completed", "operation", "bot.handle_message", "telegram_user_id", telegramUserID, "user_id", userID, "command", commandName)
}

func splitCommand(text string) (string, string) {
	if !strings.HasPrefix(text, "/") {
		return "", text
	}
	parts := strings.SplitN(text, " ", 2)
	cmd := strings.ToLower(parts[0])
	if i := strings.Index(cmd, "@"); i >= 0 {
		cmd = cmd[:i]
	}
	arg := ""
	if len(parts) > 1 {
		arg = strings.TrimSpace(parts[1])
	}
	return cmd, arg
}

func (b *Bot) SendMessage(chatID int64, text string) error {
	return b.send(chatID, text)
}

func (b *Bot) reply(chatID int64, text string) {
	if err := b.send(chatID, text); err != nil {
		b.logger.Error("message send failed", "operation", "bot.send_message", "chat_id", chatID, "error", err)
	}
}

func (b *Bot) send(chatID int64, text string) error {
	if strings.TrimSpace(text) == "" {
		text = b.cfg.ResponseLanguage.CommonMessages().EmptySendFallback
	}
	for _, chunk := range chunks(text, 3500) {
		msg := tgbotapi.NewMessage(chatID, chunk)
		msg.DisableWebPagePreview = true
		if _, err := b.api.Send(msg); err != nil {
			return err
		}
	}
	return nil
}

func chunks(s string, max int) []string {
	var out []string
	for len(s) > max {
		cut := strings.LastIndex(s[:max], "\n")
		if cut < 500 {
			cut = max
		}
		out = append(out, s[:cut])
		s = strings.TrimSpace(s[cut:])
	}
	out = append(out, s)
	return out
}
