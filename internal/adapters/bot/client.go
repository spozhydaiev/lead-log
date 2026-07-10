package bot

import (
	"context"
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/spozhydaiev/lead-log/internal/config"
	svc "github.com/spozhydaiev/lead-log/internal/services"
	"github.com/spozhydaiev/lead-log/pkg/utils"
)

type Bot struct {
	api *tgbotapi.BotAPI
	cfg config.Config
	svc *svc.Service
}

func New(cfg config.Config, svc *svc.Service) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.TelegramBotToken)
	if err != nil {
		return nil, err
	}
	return &Bot{api: api, cfg: cfg, svc: svc}, nil
}

func (b *Bot) Run(ctx context.Context) error {
	log.Printf("Authorized as @%s", b.api.Self.UserName)
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case update := <-updates:
			if update.Message == nil || update.Message.From == nil {
				continue
			}
			go b.handleMessage(ctx, update.Message)
		}
	}
}

func (b *Bot) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
	telegramUserID := msg.From.ID
	if len(b.cfg.AllowedTelegramUserIDs) > 0 && !b.cfg.AllowedTelegramUserIDs[telegramUserID] {
		b.reply(msg.Chat.ID, "Access denied.")
		return
	}

	userID, err := b.svc.EnsureUser(ctx, telegramUserID, msg.From.UserName)
	if err != nil {
		log.Printf("ensure user: %v", err)
		b.reply(msg.Chat.ID, "Failed to initialize user.")
		return
	}

	text := strings.TrimSpace(msg.Text)
	if text == "" {
		b.reply(msg.Chat.ID, "Send text note or use /note <text>.")
		return
	}

	cmd, arg := splitCommand(text)
	var response string
	switch cmd {
	case "/start", "/help":
		response = helpText()
	case "/note":
		response, err = b.svc.CaptureNote(ctx, userID, arg)
	case "/now":
		response, err = b.svc.AddNote(ctx, userID, arg)
	case "/open":
		response, err = b.svc.OpenActions(ctx, userID)
	case "/people":
		response, err = b.svc.People(ctx, userID)
	case "/alias":
		response, err = b.svc.Alias(ctx, userID, arg)
	case "/merge":
		response, err = b.svc.Merge(ctx, userID, arg)
	case "/done":
		response, err = b.svc.Done(ctx, userID, arg)
	case "/person":
		cleanArg, refresh := utils.StripRefreshFlag(arg)
		response, err = b.svc.Person(ctx, userID, cleanArg, refresh)
	case "/agenda":
		cleanArg, refresh := utils.StripRefreshFlag(arg)
		response, err = b.svc.Agenda(ctx, userID, cleanArg, refresh)
	case "/ticket":
		response, err = b.svc.Ticket(ctx, arg)
	case "/daily":
		response, err = b.svc.Daily(ctx, userID, utils.HasRefreshFlag(arg))
	case "/weekly":
		response, err = b.svc.Weekly(ctx, userID, utils.HasRefreshFlag(arg))
	default:
		// Treat normal text as a quick note to reduce capture friction.
		response, err = b.svc.CaptureNote(ctx, userID, text)
	}
	if err != nil {
		log.Printf("handle command %s: %v", cmd, err)
		b.reply(msg.Chat.ID, "Error: "+err.Error())
		return
	}
	b.reply(msg.Chat.ID, response)
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
		log.Printf("send message: %v", err)
	}
}

func (b *Bot) send(chatID int64, text string) error {
	if strings.TrimSpace(text) == "" {
		text = "Done."
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

func helpText() string {
	return `LeadLog Bot

Команди:
/note <текст> — швидко зберегти сиру нотатку без AI-обробки
/now <текст> — зберегти й одразу структурувати нотатку
/open — показати відкриті дії
/done <action_id> — позначити дію виконаною
/people — список людей та аліасів
/alias <аліас> = <канонічне_імʼя> — додати аліас для людини
/merge <джерело> = <ціль> — безпечно обʼєднати дублікати людей
/person <імʼя> — контекст щодо людини за останні 90 днів
/agenda <імʼя> — практична agenda для 1:1 за останні 90 днів
/ticket <контекст> — згенерувати чернетку Jira-style ticket
/daily — денний дайджест за сьогодні, також структурує дії та нотатки про людей
/weekly — тижневий дайджест

Порада: можна надіслати звичайний текст без /note. Він збережеться як сира нотатка для /daily.`
}
