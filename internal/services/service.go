package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spozhydaiev/lead-log/internal/adapters/llm"
	"github.com/spozhydaiev/lead-log/internal/adapters/store"
	"github.com/spozhydaiev/lead-log/internal/models"
	"github.com/spozhydaiev/lead-log/pkg/utils"
)

const PromptVersion = "v1"

type Service struct {
	store         *store.Store
	llm           llm.ClientLLM
	dailyLocation *time.Location
}

func New(store *store.Store, llm llm.ClientLLM, opts ...Option) *Service {
	s := &Service{store: store, llm: llm, dailyLocation: time.Local}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

type Option func(*Service)

func WithDailyLocation(loc *time.Location) Option {
	return func(s *Service) {
		if loc != nil {
			s.dailyLocation = loc
		}
	}
}

func (s *Service) EnsureUser(ctx context.Context, telegramUserID int64, username string) (int64, error) {
	return s.store.UpsertUser(ctx, telegramUserID, username)
}

func (s *Service) CaptureNote(ctx context.Context, userID int64, raw string) (string, error) {
	if _, err := s.store.SaveRawNote(ctx, userID, raw); err != nil {
		return "", err
	}
	return "Збережено в нотатки за сьогодні.", nil
}

func (s *Service) AddNote(ctx context.Context, userID int64, raw string) (string, error) {
	parsed, err := s.llm.ParseManagerNote(ctx, raw)
	if err != nil {
		return "", err
	}
	noteID, err := s.store.SaveParsedNote(ctx, userID, raw, parsed)
	if err != nil {
		return "", err
	}
	return formatParsedNote(noteID, parsed), nil
}

func (s *Service) OpenActions(ctx context.Context, userID int64) (string, error) {
	actions, err := s.store.ListOpenActions(ctx, userID, 30)
	if err != nil {
		return "", err
	}
	if len(actions) == 0 {
		return "Відкритих дій немає 🎉", nil
	}
	var b strings.Builder
	b.WriteString("Відкриті дії:\n")
	for _, a := range actions {
		person := ""
		if a.PersonName != nil {
			person = " — " + *a.PersonName
		}
		b.WriteString(fmt.Sprintf("%d. %s%s", a.ID, a.Title, person))
		if a.OutputType != "" {
			b.WriteString(" [" + a.OutputType + "]")
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

func (s *Service) Done(ctx context.Context, userID int64, arg string) (string, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(arg), 10, 64)
	if err != nil {
		return "Використання: /done <action_id>", nil
	}
	if err := s.store.MarkActionDone(ctx, userID, id); err != nil {
		return "", err
	}
	return fmt.Sprintf("Позначено дію %d як виконану.", id), nil
}

func (s *Service) People(ctx context.Context, userID int64) (string, error) {
	people, err := s.store.ListPeople(ctx, userID)
	if err != nil {
		return "", err
	}
	if len(people) == 0 {
		return "Людей ще немає.", nil
	}
	var b strings.Builder
	b.WriteString("Люди:\n")
	for _, p := range people {
		b.WriteString("- " + p.Name)
		if len(p.Aliases) > 0 {
			b.WriteString(" (аліаси: " + strings.Join(p.Aliases, ", ") + ")")
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String()), nil
}

func (s *Service) Alias(ctx context.Context, userID int64, arg string) (string, error) {
	left, right, ok := splitEqualsArg(arg)
	if !ok {
		return "Usage: /alias <alias> = <canonical_name>", nil
	}
	canonicalName, err := s.store.AddPersonAlias(ctx, userID, left, right)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Added alias %q for %q.", left, canonicalName), nil
}

func (s *Service) Merge(ctx context.Context, userID int64, arg string) (string, error) {
	left, right, ok := splitEqualsArg(arg)
	if !ok {
		return "Usage: /merge <source_person> = <target_person>", nil
	}
	targetName, err := s.store.MergePeople(ctx, userID, left, right)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Merged %q into %q.", left, targetName), nil
}

func splitEqualsArg(arg string) (string, string, bool) {
	parts := strings.SplitN(arg, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	left := strings.TrimSpace(parts[0])
	right := strings.TrimSpace(parts[1])
	return left, right, left != "" && right != ""
}

func (s *Service) Person(ctx context.Context, userID int64, name string, refresh bool) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Usage: /person <name>", nil
	}

	since := time.Now().AddDate(0, -3, 0)
	personName, source, err := s.store.PersonSummarySource(ctx, userID, name, since)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(source) == "" {
		return "No people notes found.", nil
	}

	scopeKey := "person:" + strings.ToLower(personName) + ":last_90_days"
	sourceHash := utils.HashStrings(source)

	if !refresh {
		cached, err := s.store.GetCachedAgentResponse(
			ctx,
			userID,
			"person",
			scopeKey,
			sourceHash,
			PromptVersion,
		)
		if err != nil {
			return "", err
		}
		if cached != nil {
			return cached.ResponseText + "\n\n_cached. Use /person " + name + " --refresh to regenerate._", nil
		}
	}

	response, err := s.llm.SummarizePerson(ctx, source)
	if err != nil {
		return "", err
	}

	now := time.Now()
	if err := s.store.SaveAgentResponse(ctx, models.AgentResponse{
		UserID:        userID,
		Kind:          "person",
		ScopeKey:      scopeKey,
		PeriodStart:   &since,
		PeriodEnd:     &now,
		SourceHash:    sourceHash,
		PromptVersion: PromptVersion,
		Model:         s.llm.Model(),
		ResponseText:  response,
	}); err != nil {
		return "", err
	}

	return response, nil
}

func (s *Service) Ticket(ctx context.Context, input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "Usage: /ticket <context>", nil
	}
	draft, err := s.llm.GenerateTicket(ctx, input)
	if err != nil {
		return "", err
	}
	return formatTicket(draft), nil
}

func (s *Service) Daily(ctx context.Context, userID int64, refresh bool) (string, error) {
	return s.DailyAt(ctx, userID, time.Now(), refresh)
}

func (s *Service) DailyAt(ctx context.Context, userID int64, now time.Time, refresh bool) (string, error) {
	loc := s.dailyLocation
	if loc == nil {
		loc = time.Local
	}
	localNow := now.In(loc)
	startOfDay := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc)
	endOfDay := startOfDay.AddDate(0, 0, 1)

	source, err := s.store.RecentDailySource(ctx, userID, startOfDay, endOfDay)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(source) == "" {
		return "За сьогодні нотаток немає.", nil
	}

	scopeKey := startOfDay.Format("2006-01-02")
	sourceHash := utils.HashStrings(source)

	if !refresh {
		cached, err := s.store.GetCachedAgentResponse(
			ctx,
			userID,
			"daily",
			scopeKey,
			sourceHash,
			PromptVersion,
		)
		if err != nil {
			return "", err
		}
		if cached != nil {
			if strings.TrimSpace(cached.ResponseJSON) != "" {
				var result models.DailyProcessingResult
				if err := json.Unmarshal([]byte(cached.ResponseJSON), &result); err != nil {
					return "", err
				}
				if err := s.store.PersistDailyStructured(ctx, userID, startOfDay, endOfDay, result.Structured); err != nil {
					return "", err
				}
			}
			return cached.ResponseText + "\n\n_з кешу. Використайте /daily --refresh, щоб згенерувати заново._", nil
		}
	}

	result, err := s.llm.ProcessDaily(ctx, source)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(result.SummaryText) == "" {
		result.SummaryText = result.Structured.Summary
	}
	if strings.TrimSpace(result.SummaryText) == "" {
		result.SummaryText = "Денний підсумок готовий, але модель не повернула текст дайджесту."
	}
	responseJSON, err := json.Marshal(result)
	if err != nil {
		return "", err
	}

	if err := s.store.PersistDailyStructured(ctx, userID, startOfDay, endOfDay, result.Structured); err != nil {
		return "", err
	}

	if err := s.store.SaveAgentResponse(ctx, models.AgentResponse{
		UserID:        userID,
		Kind:          "daily",
		ScopeKey:      scopeKey,
		PeriodStart:   &startOfDay,
		PeriodEnd:     &endOfDay,
		SourceHash:    sourceHash,
		PromptVersion: PromptVersion,
		Model:         s.llm.Model(),
		ResponseText:  result.SummaryText,
		ResponseJSON:  string(responseJSON),
	}); err != nil {
		return "", err
	}

	return result.SummaryText, nil
}

func (s *Service) Weekly(ctx context.Context, userID int64, refresh bool) (string, error) {
	now := time.Now()
	start := now.AddDate(0, 0, -7)

	source, err := s.store.RecentWeeklySource(ctx, userID, start)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(source) == "" {
		return "No notes for the last 7 days.", nil
	}

	year, week := now.ISOWeek()
	scopeKey := fmt.Sprintf("%d-W%02d", year, week)
	sourceHash := utils.HashStrings(source)

	if !refresh {
		cached, err := s.store.GetCachedAgentResponse(
			ctx,
			userID,
			"weekly",
			scopeKey,
			sourceHash,
			PromptVersion,
		)
		if err != nil {
			return "", err
		}
		if cached != nil {
			return cached.ResponseText + "\n\n_cached. Use /weekly --refresh to regenerate._", nil
		}
	}

	response, err := s.llm.SummarizeWeekly(ctx, source)
	if err != nil {
		return "", err
	}

	if err := s.store.SaveAgentResponse(ctx, models.AgentResponse{
		UserID:        userID,
		Kind:          "weekly",
		ScopeKey:      scopeKey,
		PeriodStart:   &start,
		PeriodEnd:     &now,
		SourceHash:    sourceHash,
		PromptVersion: PromptVersion,
		Model:         s.llm.Model(),
		ResponseText:  response,
	}); err != nil {
		return "", err
	}

	return response, nil
}

func formatParsedNote(noteID int64, p models.ParsedNote) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Saved note #%d\n\n", noteID))
	if p.Summary != "" {
		b.WriteString("Summary:\n" + p.Summary + "\n\n")
	}
	if len(p.Actions) > 0 {
		b.WriteString("Actions:\n")
		for i, a := range p.Actions {
			b.WriteString(fmt.Sprintf("%d. %s", i+1, a.Title))
			if a.LinkedPersonName != "" {
				b.WriteString(" — " + a.LinkedPersonName)
			}
			if a.OutputType != "" {
				b.WriteString(" [" + a.OutputType + "]")
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if len(p.PeopleNotes) > 0 {
		b.WriteString("People notes:\n")
		for _, pn := range p.PeopleNotes {
			b.WriteString(fmt.Sprintf("- %s: %s / %s — %s\n", pn.PersonName, pn.Type, pn.Theme, pn.Text))
		}
		b.WriteString("\n")
	}
	if len(p.TicketDrafts) > 0 {
		b.WriteString("Ticket drafts detected. Use /ticket <context> if you want a cleaner ticket draft.\n")
	}
	if len(p.SuggestedQuestions) > 0 {
		b.WriteString("Questions:\n")
		for _, q := range p.SuggestedQuestions {
			b.WriteString("- " + q + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func formatPersonContext(pc models.PersonContext) string {
	var b strings.Builder
	b.WriteString(pc.PersonName + " — last 90 days\n\n")
	if len(pc.Actions) > 0 {
		b.WriteString("Open follow-ups:\n")
		for _, a := range pc.Actions {
			b.WriteString(fmt.Sprintf("- #%d %s\n", a.ID, a.Title))
		}
		b.WriteString("\n")
	}
	if len(pc.Notes) == 0 {
		b.WriteString("No people notes found.")
		return b.String()
	}
	b.WriteString("Highlights:\n")
	for _, n := range pc.Notes {
		b.WriteString(fmt.Sprintf("- %s / %s: %s (note #%d, %s)\n", n.Type, n.Theme, n.Text, n.NoteID, n.CreatedAt.Format("2006-01-02")))
	}
	return b.String()
}

func formatTicket(t models.TicketDraft) string {
	var b strings.Builder
	b.WriteString("Ticket draft:\n\n")
	b.WriteString("Title:\n" + t.Title + "\n\n")
	if t.Context != "" {
		b.WriteString("Context:\n" + t.Context + "\n\n")
	}
	if t.Problem != "" {
		b.WriteString("Problem:\n" + t.Problem + "\n\n")
	}
	if len(t.AcceptanceCriteria) > 0 {
		b.WriteString("Acceptance criteria:\n")
		for _, ac := range t.AcceptanceCriteria {
			b.WriteString("- " + ac + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}
