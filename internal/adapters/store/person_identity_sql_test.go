package store

import (
	"os"
	"strings"
	"testing"
)

func TestPersonIdentityMigrationAddsProfileAndMergeFoundation(t *testing.T) {
	content, err := os.ReadFile("../../../migrations/015_person_identity_foundation.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToLower(string(content))
	for _, want := range []string{
		"add column if not exists first_name text",
		"add column if not exists last_name text",
		"add column if not exists job_title text",
		"add column if not exists team text",
		"add column if not exists company text",
		"add column if not exists notes text",
		"add column if not exists normalized_name text",
		"add column if not exists merged_into_person_id bigint null references people(id) on delete set null",
		"add column if not exists updated_at timestamptz not null default now()",
		"idx_people_user_active_normalized_name",
		"where merged_into_person_id is null",
		"unique active-person normalized-name index is intentionally deferred",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}

func TestPersonCreationSQLAvoidsAliasDoNothingRace(t *testing.T) {
	content, err := os.ReadFile("store.go")
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToLower(string(content))
	for _, want := range []string{
		"pg_advisory_xact_lock",
		"insert into people (user_id, name, normalized_name, updated_at)",
		"on conflict (user_id, normalized_alias) do update",
		"returning person_id",
		"delete from people where user_id=$1 and id=$2",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("person creation path missing %q", want)
		}
	}
	if strings.Contains(sql[strings.Index(sql, "func upsertperson"):strings.Index(sql, "func (s *store) claimnoteforenrichment")], "do nothing") {
		t.Fatal("upsertPerson must not use ON CONFLICT DO NOTHING for aliases")
	}
}

func TestUpdatePersonProfileSQLUsesTransactionalLocksAndConflictHandling(t *testing.T) {
	content, err := os.ReadFile("store.go")
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToLower(string(content))
	start := strings.Index(sql, "func (s *store) updatepersonprofile")
	if start < 0 {
		t.Fatal("missing UpdatePersonProfile")
	}
	body := sql[start:]
	for _, want := range []string{
		"begintx(ctx, pgx.txoptions{isolevel: pgx.serializable}",
		"for update",
		"pg_advisory_xact_lock",
		"normalized_name=$4",
		"delete from person_aliases",
		"insert into person_aliases",
		"on conflict (user_id,normalized_alias)",
		"errpersonidentityconflict",
		"errpersonupdateconflict",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("UpdatePersonProfile missing %q", want)
		}
	}
}
