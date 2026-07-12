package services

import (
	"strings"
	"testing"

	"github.com/spozhydaiev/lead-log/internal/models"
)

func TestLanguageScopedCacheKey(t *testing.T) {
	base := "2026-07-12"
	if scopedCacheKey(base, models.LanguageEnglish) == scopedCacheKey(base, models.LanguageUkrainian) {
		t.Fatal("cache key should include response language")
	}
	if got := scopedCacheKey(base, models.LanguagePolish); got != "2026-07-12:pl" {
		t.Fatalf("polish scoped cache key = %q", got)
	}
}

func TestFormatDailyDigestForLanguageLabels(t *testing.T) {
	digest := models.DailyDigest{ShortSummary: "Summary"}
	if got := FormatDailyDigestForLanguage(digest, models.LanguageEnglish); !strings.Contains(got, "Brief") {
		t.Fatalf("english digest missing Brief: %q", got)
	}
	if got := FormatDailyDigestForLanguage(digest, models.LanguageUkrainian); !strings.Contains(got, "Коротко") {
		t.Fatalf("ukrainian digest missing Коротко: %q", got)
	}
	if got := FormatDailyDigestForLanguage(digest, models.LanguagePolish); !strings.Contains(got, "Krótko") {
		t.Fatalf("polish digest missing Krótko: %q", got)
	}
}
