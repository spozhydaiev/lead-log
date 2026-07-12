package llm

import "testing"

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

func TestParseDailyDigestJSONRejectsInvalidEnum(t *testing.T) {
	content := `{"short_summary":"x","open_loops":[],"ticket_candidates":[],"people_highlights":[{"person_name":"Олена","type":"score","theme":"delivery","text":"x","source_note_ids":[1]}],"decisions":[],"suggested_next_steps":[],"unclear_items":[]}`
	if _, err := ParseDailyDigestJSON(content); err == nil {
		t.Fatal("expected invalid enum error")
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

func TestParseDailyDigestJSONRejectsInvalidTypeWithValidTheme(t *testing.T) {
	content := dailyDigestJSON("score", "delivery")
	if _, err := ParseDailyDigestJSON(content); err == nil {
		t.Fatal("expected invalid type error")
	}
}

func dailyDigestJSON(typeValue, theme string) string {
	return `{"short_summary":"x","open_loops":[],"ticket_candidates":[],"people_highlights":[{"person_name":"Олена","type":"` + typeValue + `","theme":"` + theme + `","text":"x","source_note_ids":[1]}],"decisions":[],"suggested_next_steps":[],"unclear_items":[]}`
}
