package models

import "fmt"

type ResponseLanguage string

const (
	LanguageEnglish   ResponseLanguage = "en"
	LanguageUkrainian ResponseLanguage = "uk"
	LanguagePolish    ResponseLanguage = "pl"
)

func ParseResponseLanguage(code string) (ResponseLanguage, error) {
	switch ResponseLanguage(code) {
	case "", LanguageEnglish:
		return LanguageEnglish, nil
	case LanguageUkrainian:
		return LanguageUkrainian, nil
	case LanguagePolish:
		return LanguagePolish, nil
	default:
		return "", fmt.Errorf("unsupported RESPONSE_LANGUAGE %q; supported values: en, uk, pl", code)
	}
}

func (l ResponseLanguage) DisplayName() string {
	switch l {
	case LanguageUkrainian:
		return "Ukrainian"
	case LanguagePolish:
		return "Polish"
	default:
		return "English"
	}
}

func (l ResponseLanguage) PromptInstruction() string {
	return fmt.Sprintf("Return all user-facing text in %s.\nDo not translate person names.\nKeep JSON field names exactly as defined in the schema.", l.DisplayName())
}

type DailyLabels struct {
	ShortSummary       string
	OpenLoops          string
	TicketCandidates   string
	PeopleHighlights   string
	Decisions          string
	SuggestedNextSteps string
	UnclearItems       string
	SourcesPrefix      string
}

func (l ResponseLanguage) DailyLabels() DailyLabels {
	switch l {
	case LanguageUkrainian:
		return DailyLabels{"Коротко", "Open loops", "Кандидати в тікети", "Нотатки про людей", "Рішення / домовленості", "Пропоновані наступні кроки", "Незрозумілі пункти", "джерела"}
	case LanguagePolish:
		return DailyLabels{"Krótko", "Otwarte sprawy", "Kandydaci na tickety", "Notatki o osobach", "Decyzje / ustalenia", "Sugerowane kolejne kroki", "Niejasne punkty", "źródła"}
	default:
		return DailyLabels{"Brief", "Open loops", "Ticket candidates", "People highlights", "Decisions / agreements", "Suggested next steps", "Unclear items", "sources"}
	}
}

type NowLabels struct {
	SavedNote   string
	Summary     string
	Actions     string
	PeopleNotes string
	Questions   string
}

func (l ResponseLanguage) NowLabels() NowLabels {
	switch l {
	case LanguageUkrainian:
		return NowLabels{"Збережено нотатку", "Підсумок", "Дії", "Нотатки про людей", "Питання"}
	case LanguagePolish:
		return NowLabels{"Zapisano notatkę", "Podsumowanie", "Działania", "Notatki o osobach", "Pytania"}
	default:
		return NowLabels{"Saved note", "Summary", "Actions", "People notes", "Questions"}
	}
}

type CommonMessages struct {
	SavedRaw           string
	NoOpenActions      string
	OpenActionsHeader  string
	DoneUsage          string
	DoneMarked         string
	NoNotesToday       string
	NoNotesLast7Days   string
	DailyCachedNotice  string
	WeeklyCachedNotice string
	EmptySendFallback  string
	UnsupportedText    string
	AccessDenied       string
	UserInitFailed     string
	HelpText           string
	NoteUsage          string
	UnknownCommand     string
	GenericError       string
}

func (l ResponseLanguage) CommonMessages() CommonMessages {
	switch l {
	case LanguageUkrainian:
		return CommonMessages{
			SavedRaw: "Збережено в нотатки за сьогодні.", NoOpenActions: "Відкритих дій немає 🎉", OpenActionsHeader: "Відкриті дії:", DoneUsage: "Використання: /done <action_id>", DoneMarked: "Позначено дію %d як виконану.", NoNotesToday: "За сьогодні нотаток немає.", NoNotesLast7Days: "Немає нотаток за останні 7 днів.", DailyCachedNotice: "_з кешу. Використайте /daily --refresh, щоб згенерувати заново._", WeeklyCachedNotice: "_з кешу. Використайте /weekly --refresh, щоб згенерувати заново._", EmptySendFallback: "Готово.", UnsupportedText: "Надішліть текстову нотатку або використайте /note <текст>.", AccessDenied: "Доступ заборонено.", UserInitFailed: "Не вдалося ініціалізувати користувача.", HelpText: ukHelp, NoteUsage: "Використання: /note <текст>", UnknownCommand: "Невідома команда. Використайте /help, щоб побачити доступні команди.", GenericError: "Щось пішло не так. Спробуйте пізніше.",
		}
	case LanguagePolish:
		return CommonMessages{
			SavedRaw: "Zapisano w dzisiejszych notatkach.", NoOpenActions: "Brak otwartych działań 🎉", OpenActionsHeader: "Otwarte działania:", DoneUsage: "Użycie: /done <action_id>", DoneMarked: "Oznaczono działanie %d jako wykonane.", NoNotesToday: "Brak notatek na dziś.", NoNotesLast7Days: "Brak notatek z ostatnich 7 dni.", DailyCachedNotice: "_z pamięci podręcznej. Użyj /daily --refresh, aby wygenerować ponownie._", WeeklyCachedNotice: "_z pamięci podręcznej. Użyj /weekly --refresh, aby wygenerować ponownie._", EmptySendFallback: "Gotowe.", UnsupportedText: "Wyślij notatkę tekstową albo użyj /note <tekst>.", AccessDenied: "Odmowa dostępu.", UserInitFailed: "Nie udało się zainicjować użytkownika.", HelpText: plHelp, NoteUsage: "Użycie: /note <tekst>", UnknownCommand: "Nieznane polecenie. Użyj /help, aby zobaczyć dostępne polecenia.", GenericError: "Coś poszło nie tak. Spróbuj ponownie później.",
		}
	default:
		return CommonMessages{
			SavedRaw: "Saved to today's notes.", NoOpenActions: "No open actions 🎉", OpenActionsHeader: "Open actions:", DoneUsage: "Usage: /done <action_id>", DoneMarked: "Marked action %d as done.", NoNotesToday: "No notes for today.", NoNotesLast7Days: "No notes for the last 7 days.", DailyCachedNotice: "_cached. Use /daily --refresh to regenerate._", WeeklyCachedNotice: "_cached. Use /weekly --refresh to regenerate._", EmptySendFallback: "Done.", UnsupportedText: "Send a text note or use /note <text>.", AccessDenied: "Access denied.", UserInitFailed: "Failed to initialize user.", HelpText: enHelp, NoteUsage: "Usage: /note <text>", UnknownCommand: "Unknown command. Use /help to see available commands.", GenericError: "Something went wrong. Please try again later.",
		}
	}
}

const enHelp = `LeadLog Bot

Commands:
/note <text> — quickly save a raw note without AI processing
/now <text> — save and immediately structure a note
/open — show open actions created only through explicit /now processing
/done <action_id> — mark an action done
/daily — daily digest for today without creating actions or people notes
/daily --refresh — regenerate the daily digest
/weekly — weekly digest for the last 7 days
/weekly --refresh — regenerate the weekly digest

Tip: you can send regular text without /note. It will be saved as a raw note for /daily.`

const ukHelp = `LeadLog Bot

Команди:
/note <текст> — швидко зберегти сиру нотатку без AI-обробки
/now <текст> — зберегти й одразу структурувати нотатку
/open — показати відкриті дії, створені лише через явну /now-обробку
/done <action_id> — позначити дію виконаною
/daily — денний дайджест за сьогодні без створення дій чи нотаток про людей
/daily --refresh — згенерувати денний дайджест заново
/weekly — тижневий дайджест за останні 7 днів
/weekly --refresh — згенерувати тижневий дайджест заново

Порада: можна надіслати звичайний текст без /note. Він збережеться як сира нотатка для /daily.`

const plHelp = `LeadLog Bot

Polecenia:
/note <tekst> — szybko zapisz surową notatkę bez przetwarzania AI
/now <tekst> — zapisz i od razu ustrukturyzuj notatkę
/open — pokaż otwarte działania utworzone tylko przez jawne /now
/done <action_id> — oznacz działanie jako wykonane
/daily — dzienny digest z dziś bez tworzenia działań ani notatek o osobach
/daily --refresh — wygeneruj dzienny digest ponownie
/weekly — tygodniowy digest z ostatnich 7 dni
/weekly --refresh — wygeneruj tygodniowy digest ponownie

Wskazówka: możesz wysłać zwykły tekst bez /note. Zostanie zapisany jako surowa notatka dla /daily.`
