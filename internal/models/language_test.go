package models

import (
	"strings"
	"testing"
)

func TestDefaultLanguageIsEnglish(t *testing.T) {
	language, err := ParseResponseLanguage("")
	if err != nil {
		t.Fatalf("parse default language: %v", err)
	}
	if language != LanguageEnglish || language.DisplayName() != "English" {
		t.Fatalf("default language = %q/%q, want en/English", language, language.DisplayName())
	}
}

func TestUkrainianPromptInstructionAndLabels(t *testing.T) {
	language, err := ParseResponseLanguage("uk")
	if err != nil {
		t.Fatalf("parse uk: %v", err)
	}
	if !strings.Contains(language.PromptInstruction(), "Ukrainian") {
		t.Fatalf("uk prompt instruction missing language: %q", language.PromptInstruction())
	}
	if language.DailyLabels().ShortSummary != "Коротко" || language.CommonMessages().NoNotesToday != "За сьогодні нотаток немає." {
		t.Fatalf("unexpected uk labels/messages: %#v %#v", language.DailyLabels(), language.CommonMessages())
	}
}

func TestPolishPromptInstructionAndLabels(t *testing.T) {
	language, err := ParseResponseLanguage("pl")
	if err != nil {
		t.Fatalf("parse pl: %v", err)
	}
	if !strings.Contains(language.PromptInstruction(), "Polish") {
		t.Fatalf("pl prompt instruction missing language: %q", language.PromptInstruction())
	}
	if language.DailyLabels().ShortSummary != "Krótko" || language.CommonMessages().NoNotesToday != "Brak notatek na dziś." {
		t.Fatalf("unexpected pl labels/messages: %#v %#v", language.DailyLabels(), language.CommonMessages())
	}
}

func TestUnsupportedLanguageFails(t *testing.T) {
	if _, err := ParseResponseLanguage("de"); err == nil {
		t.Fatal("expected unsupported language error")
	}
}
