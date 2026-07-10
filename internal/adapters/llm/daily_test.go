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
