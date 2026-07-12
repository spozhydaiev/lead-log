package models

import "testing"

func TestNormalizeTicketKey(t *testing.T) {
	cases := map[string]string{"CH-1234": "CH-1234", "abc-42": "ABC-42", "API2-99": "API2-99", "ch-1234": "CH-1234", "Ch-1234": "CH-1234"}
	for in, want := range cases {
		got, ok := NormalizeTicketKey(in)
		if !ok || got != want {
			t.Fatalf("NormalizeTicketKey(%q)=(%q,%v), want %q,true", in, got, ok, want)
		}
	}
	for _, in := range []string{"hello-world", "2026-07", "foo-", "-123"} {
		if got, ok := NormalizeTicketKey(in); ok {
			t.Fatalf("NormalizeTicketKey(%q)=(%q,true), want false", in, got)
		}
	}
}

func TestEntityMentionValidationDedupAndFallback(t *testing.T) {
	p := AddTicketFallbackMentions(ParsedNote{EntityMentions: []EntityMention{{Type: EntityTypeTicket, Value: "ch-1234"}, {Type: "bad", Value: "x"}, {Type: EntityTypeService, Value: " Message   Processor "}}}, "see Ch-1234 and API2-99, not hello-world or 2026-07")
	out, skipped := NormalizeEntityMentionsForNote(p.EntityMentions)
	if skipped != 1 {
		t.Fatalf("skipped=%d, want 1", skipped)
	}
	seen := map[string]bool{}
	for _, r := range out {
		seen[r.Type+":"+r.NormalizedValue] = true
	}
	for _, want := range []string{"ticket:CH-1234", "ticket:API2-99", "service:message processor"} {
		if !seen[want] {
			t.Fatalf("missing %s in %#v", want, out)
		}
	}
	if len(out) != 3 {
		t.Fatalf("len=%d, want 3: %#v", len(out), out)
	}
}

func TestDecisionValidation(t *testing.T) {
	long := make([]byte, MaxDecisionTextLength+20)
	for i := range long {
		long[i] = 'a'
	}
	out, skipped := NormalizeDecisionsForNote([]ParsedDecision{{Text: "   "}, {Text: "  Decided to use Postgres  ", Topic: " arch "}, {Text: string(long)}})
	if skipped != 1 || len(out) != 2 {
		t.Fatalf("len=%d skipped=%d", len(out), skipped)
	}
	if out[0].Text != "Decided to use Postgres" || out[0].Topic != "arch" {
		t.Fatalf("unexpected trim: %#v", out[0])
	}
	if len(out[1].Text) != MaxDecisionTextLength {
		t.Fatalf("long decision len=%d", len(out[1].Text))
	}
}
