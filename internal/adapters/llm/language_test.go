package llm

import (
	"strings"
	"testing"

	"github.com/spozhydaiev/lead-log/internal/models"
)

func TestPromptsPreserveNamesAndJSONKeys(t *testing.T) {
	prompt := dailyPrompt(models.LanguageEnglish)
	for _, want := range []string{
		"Return all user-facing text in English.",
		"Do not translate person names.",
		"Keep JSON field names exactly as defined in the schema.",
		"\"short_summary\"",
		"\"people_highlights\"",
		"\"source_note_ids\"",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("daily prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestPromptInstructionLanguages(t *testing.T) {
	cases := map[models.ResponseLanguage]string{
		models.LanguageEnglish:   "English",
		models.LanguageUkrainian: "Ukrainian",
		models.LanguagePolish:    "Polish",
	}
	for language, display := range cases {
		for name, prompt := range map[string]string{"system": systemPrompt(language), "daily": dailyPrompt(language), "weekly": weeklyPrompt(language)} {
			if !strings.Contains(prompt, "Return all user-facing text in "+display+".") {
				t.Fatalf("%s prompt for %s missing language instruction", name, language)
			}
		}
	}
}

func TestDailyPromptEnumListsMatchValidator(t *testing.T) {
	prompt := dailyPrompt(models.LanguageEnglish)
	for _, value := range dailyHighlightTypes {
		if !strings.Contains(prompt, value) {
			t.Fatalf("daily prompt missing people highlight type %q", value)
		}
	}
	for _, value := range dailyThemes {
		if !strings.Contains(prompt, value) {
			t.Fatalf("daily prompt missing people highlight theme %q", value)
		}
	}
}
