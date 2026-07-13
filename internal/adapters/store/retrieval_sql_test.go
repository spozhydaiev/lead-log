package store

import (
	"os"
	"strings"
	"testing"
)

func TestRetrievalQueriesAreUserScopedAndLimited(t *testing.T) {
	content, err := os.ReadFile("retrieval.go")
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(content))
	for _, required := range []string{
		"where n.user_id=$1",
		"where em.user_id=$1",
		"where a.user_id=$1",
		"where pn.user_id=$1",
		"where d.user_id=$1",
		"limit $",
		"similarity(",
		"to_tsvector('simple'",
	} {
		if !strings.Contains(s, required) {
			t.Fatalf("retrieval SQL missing %q", required)
		}
	}
}

func TestRetrievalMigrationAddsTargetedIndexes(t *testing.T) {
	content, err := os.ReadFile("../../../migrations/012_add_retrieval_indexes.sql")
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(content))
	for _, required := range []string{
		"idx_notes_retrieval_text_trgm",
		"idx_notes_retrieval_fts",
		"idx_actions_user_person_created",
		"idx_people_notes_user_person_created",
		"idx_decisions_user_status_created",
		"idx_person_aliases_user_normalized",
	} {
		if !strings.Contains(s, required) {
			t.Fatalf("retrieval migration missing %q", required)
		}
	}
}

func TestTicketRetrievalQueriesAreScopedAndBounded(t *testing.T) {
	content, err := os.ReadFile("retrieval.go")
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ToLower(string(content))
	for _, required := range []string{
		"getticketmentionbounds",
		"searchticketfallbacknotes",
		"listactionsbysourcenoteids",
		"listdecisionsbysourcenoteids",
		"em.user_id=$1 and em.entity_type=$2 and em.normalized_value=$3",
		"where user_id=$1 and (raw_text ~* $2",
		"where a.user_id=$1 and a.note_id=any($2::bigint[])",
		"where d.user_id=$1 and d.note_id=any($2::bigint[])",
		"limit $3",
	} {
		if !strings.Contains(s, required) {
			t.Fatalf("ticket retrieval SQL missing %q", required)
		}
	}
}
