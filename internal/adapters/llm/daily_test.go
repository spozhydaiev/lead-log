package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spozhydaiev/lead-log/internal/models"
)

func TestParseDailyDigestJSON(t *testing.T) {
	content := `{
		"short_summary":"День має один follow-up.",
		"open_loops":[{"title":"Уточнити ETA","owner":"Олена","due_hint":null,"source_note_ids":[1]}],
		"ticket_candidates":[],
		"people_highlights":[{"person_name":"Олена","type":"commitment","theme":"delivery","text":"Пообіцяла оновити ETA.","source_note_ids":[1]}],
		"decisions":[],
		"suggested_next_steps":[],
		"unclear_items":[]
	}`
	digest, err := ParseDailyDigestJSON(content)
	if err != nil {
		t.Fatal(err)
	}
	if digest.ShortSummary == "" || len(digest.OpenLoops) != 1 || len(digest.PeopleHighlights) != 1 {
		t.Fatalf("unexpected digest: %+v", digest)
	}
}

func TestParseDailyDigestJSONSkipsPeopleHighlightMissingPersonName(t *testing.T) {
	content := `{"short_summary":"x","open_loops":[],"ticket_candidates":[],"people_highlights":[{"person_name":" ","type":"commitment","theme":"delivery","text":"x","source_note_ids":[1]}],"decisions":[{"text":"Рішення лишається валідним.","source_note_ids":[2]}],"suggested_next_steps":[],"unclear_items":[]}`
	digest, err := ParseDailyDigestJSON(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(digest.PeopleHighlights) != 0 {
		t.Fatalf("people highlights len = %d, want 0", len(digest.PeopleHighlights))
	}
	if len(digest.Decisions) != 1 {
		t.Fatalf("decisions len = %d, want 1", len(digest.Decisions))
	}
}

func TestParseDailyDigestJSONSkipsPeopleHighlightMissingText(t *testing.T) {
	content := `{"short_summary":"x","open_loops":[{"title":"Уточнити ETA","owner":null,"due_hint":null,"source_note_ids":[1]}],"ticket_candidates":[],"people_highlights":[{"person_name":"Олена","type":"commitment","theme":"delivery","text":" ","source_note_ids":[1]}],"decisions":[],"suggested_next_steps":[],"unclear_items":[]}`
	digest, err := ParseDailyDigestJSON(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(digest.PeopleHighlights) != 0 {
		t.Fatalf("people highlights len = %d, want 0", len(digest.PeopleHighlights))
	}
	if len(digest.OpenLoops) != 1 {
		t.Fatalf("open loops len = %d, want 1", len(digest.OpenLoops))
	}
}

func TestParseDailyDigestJSONSkipsInvalidHighlightMixedWithValidHighlights(t *testing.T) {
	content := `{"short_summary":"x","open_loops":[],"ticket_candidates":[],"people_highlights":[{"person_name":" ","type":"commitment","theme":"delivery","text":"x","source_note_ids":[1]},{"person_name":"Олена","type":"commitment","theme":"delivery","text":"Пообіцяла оновити ETA.","source_note_ids":[2]}],"decisions":[],"suggested_next_steps":[],"unclear_items":[]}`
	digest, err := ParseDailyDigestJSON(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(digest.PeopleHighlights) != 1 {
		t.Fatalf("people highlights len = %d, want 1", len(digest.PeopleHighlights))
	}
	if digest.PeopleHighlights[0].PersonName != "Олена" {
		t.Fatalf("person name = %q, want Олена", digest.PeopleHighlights[0].PersonName)
	}
}

func TestParseDailyDigestJSONAllHighlightsInvalidButOtherSectionsValid(t *testing.T) {
	content := `{"short_summary":"","open_loops":[],"ticket_candidates":[{"title":"Підготувати тикет","context":"Контекст","owner":null,"source_note_ids":[1]}],"people_highlights":[{"person_name":"","type":"commitment","theme":"delivery","text":"x","source_note_ids":[1]},{"person_name":"Олена","type":"commitment","theme":"delivery","text":"","source_note_ids":[2]}],"decisions":[],"suggested_next_steps":[],"unclear_items":[]}`
	digest, err := ParseDailyDigestJSON(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(digest.PeopleHighlights) != 0 {
		t.Fatalf("people highlights len = %d, want 0", len(digest.PeopleHighlights))
	}
	if len(digest.TicketCandidates) != 1 {
		t.Fatalf("ticket candidates len = %d, want 1", len(digest.TicketCandidates))
	}
}

func TestParseDailyDigestJSONRejectsUnusableDigest(t *testing.T) {
	content := `{"short_summary":" ","open_loops":[{"title":" ","owner":null,"due_hint":null,"source_note_ids":[1]}],"ticket_candidates":[],"people_highlights":[{"person_name":"","type":"commitment","theme":"delivery","text":"x","source_note_ids":[1]}],"decisions":[{"text":" ","source_note_ids":[2]}],"suggested_next_steps":[],"unclear_items":[]}`
	if _, err := ParseDailyDigestJSON(content); err == nil {
		t.Fatal("expected unusable digest error")
	}
}

func TestParseDailyDigestJSONNormalizesReportedCollaborationType(t *testing.T) {
	content := `{"short_summary":"A productive day.","people_highlights":[{"person":"Alex","person_name":"Alex","type":"collaboration","theme":"teamwork","text":"Worked together on retry policy."}]}`
	digest, err := ParseDailyDigestJSON(content)
	if err != nil {
		t.Fatal(err)
	}
	if digest.PeopleHighlights[0].Type != "collaboration" {
		t.Fatalf("type = %q, want collaboration", digest.PeopleHighlights[0].Type)
	}
	if digest.PeopleHighlights[0].Theme != "other" {
		t.Fatalf("theme = %q, want other", digest.PeopleHighlights[0].Theme)
	}
}

func TestParseDailyDigestJSONNormalizesUnknownType(t *testing.T) {
	content := `{"short_summary":"Summary","people_highlights":[{"person_name":"Alex","type":"unexpected_future_type","theme":"other","text":"Some useful context."}]}`
	digest, err := ParseDailyDigestJSON(content)
	if err != nil {
		t.Fatal(err)
	}
	if digest.ShortSummary != "Summary" {
		t.Fatalf("short summary = %q, want Summary", digest.ShortSummary)
	}
	if digest.PeopleHighlights[0].Type != fallbackDailyHighlightType {
		t.Fatalf("type = %q, want %q", digest.PeopleHighlights[0].Type, fallbackDailyHighlightType)
	}
}

func TestParseDailyDigestJSONNormalizesTypeValueInTheme(t *testing.T) {
	tests := []struct {
		name      string
		typeValue string
		theme     string
		wantType  string
	}{
		{name: "theme growth_topic with empty type", typeValue: "", theme: "growth_topic", wantType: "growth_topic"},
		{name: "theme positive_signal with empty type", typeValue: "", theme: "positive_signal", wantType: "positive_signal"},
		{name: "valid type with invalid theme", typeValue: "commitment", theme: "not_a_theme", wantType: "commitment"},
		{name: "fully valid people highlight", typeValue: "commitment", theme: "delivery", wantType: "commitment"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := dailyDigestJSON(tt.typeValue, tt.theme)
			digest, err := ParseDailyDigestJSON(content)
			if err != nil {
				t.Fatal(err)
			}
			got := digest.PeopleHighlights[0]
			if got.Type != tt.wantType {
				t.Fatalf("type = %q, want %q", got.Type, tt.wantType)
			}
			wantTheme := tt.theme
			if !allowedDailyThemes[tt.theme] {
				wantTheme = "other"
			}
			if got.Theme != wantTheme {
				t.Fatalf("theme = %q, want %q", got.Theme, wantTheme)
			}
		})
	}
}

func TestParseDailyDigestJSONMixedValidAndInvalidOptionalItems(t *testing.T) {
	content := `{"short_summary":" ","open_loops":[],"ticket_candidates":[],"people_highlights":[{"person_name":"Олена","type":"commitment","theme":"delivery","text":"Пообіцяла оновити ETA.","source_note_ids":[1]},{"person_name":"Alex","type":"unexpected_future_type","theme":"other","text":"Some useful context.","source_note_ids":[2]},{"person_name":" ","type":"commitment","theme":"delivery","text":"missing person","source_note_ids":[3]}],"decisions":[{"text":"Decision remains valid.","source_note_ids":[4]}],"suggested_next_steps":[],"unclear_items":[]}`
	digest, err := ParseDailyDigestJSON(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(digest.PeopleHighlights) != 2 {
		t.Fatalf("people highlights len = %d, want 2", len(digest.PeopleHighlights))
	}
	if digest.PeopleHighlights[0].Type != "commitment" || digest.PeopleHighlights[1].Type != fallbackDailyHighlightType {
		t.Fatalf("unexpected highlight types: %+v", digest.PeopleHighlights)
	}
	if len(digest.Decisions) != 1 {
		t.Fatalf("decisions len = %d, want 1", len(digest.Decisions))
	}
}

func dailyDigestJSON(typeValue, theme string) string {
	return `{"short_summary":"x","open_loops":[],"ticket_candidates":[],"people_highlights":[{"person_name":"Олена","type":"` + typeValue + `","theme":"` + theme + `","text":"x","source_note_ids":[1]}],"decisions":[],"suggested_next_steps":[],"unclear_items":[]}`
}

func TestProcessDailyUsesTolerantParserForCollaboration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"short_summary\":\"A productive day.\",\"people_highlights\":[{\"person_name\":\"Alex\",\"type\":\"collaboration\",\"theme\":\"teamwork\",\"text\":\"Worked together on retry policy.\"}]}"}}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", "test-model", models.LanguageEnglish)
	digest, err := client.ProcessDaily(context.Background(), "#1 note")
	if err != nil {
		t.Fatal(err)
	}
	if len(digest.PeopleHighlights) != 1 || digest.PeopleHighlights[0].Type != "collaboration" {
		t.Fatalf("unexpected daily digest: %+v", digest)
	}
}
