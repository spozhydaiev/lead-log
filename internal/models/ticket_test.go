package models

import "testing"

func TestNormalizeTicketKeyValidation(t *testing.T) {
	valid := map[string]string{"CH-1234": "CH-1234", "ch-1234": "CH-1234", "API2-99": "API2-99", "abc-42": "ABC-42", "Ch-1234": "CH-1234"}
	for in, want := range valid {
		got, ok := NormalizeTicketKey(in)
		if !ok || got != want {
			t.Fatalf("NormalizeTicketKey(%q)=%q,%v want %q,true", in, got, ok, want)
		}
	}
	for _, in := range []string{"hello-world", "2026-07", "foo-", "-123", "CH 1234"} {
		if got, ok := NormalizeTicketKey(in); ok {
			t.Fatalf("NormalizeTicketKey(%q)=%q,true want invalid", in, got)
		}
	}
}
