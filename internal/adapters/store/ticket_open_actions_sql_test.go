package store

import (
	"os"
	"strings"
	"testing"
)

func ticketOpenActionsSQL(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("store.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	start := strings.Index(src, "func (s *Store) OpenActionsForTicket")
	if start < 0 {
		t.Fatal("OpenActionsForTicket not found")
	}
	end := strings.Index(src[start:], "func (s *Store) RecentDecisionsForTicket")
	if end < 0 {
		t.Fatal("RecentDecisionsForTicket not found")
	}
	return src[start : start+end]
}

func normalizedSQL(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func TestTicketOpenActionsUsesCorrelatedExistsWithoutDistinct(t *testing.T) {
	sql := normalizedSQL(ticketOpenActionsSQL(t))

	for _, want := range []string{
		"SELECT a.id,a.note_id,COALESCE(a.title,''),COALESCE(a.status,'open'),p.name,p.id,a.due_at,a.created_at,a.completed_at FROM actions a",
		"WHERE a.user_id=$1 AND a.status='open'",
		"EXISTS (SELECT 1 FROM entity_mentions em WHERE em.user_id=a.user_id AND em.note_id=a.note_id AND em.entity_type='ticket' AND em.normalized_value=$2)",
		"LIMIT $3",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("ticket open actions SQL missing %q in %s", want, sql)
		}
	}
	if strings.Contains(sql, "SELECT DISTINCT") {
		t.Fatalf("ticket open actions must not use SELECT DISTINCT: %s", sql)
	}
	if strings.Contains(sql, "JOIN entity_mentions") {
		t.Fatalf("ticket open actions must not join entity_mentions in the outer action query: %s", sql)
	}
}

func TestTicketOpenActionsPreservesOrderingRules(t *testing.T) {
	sql := normalizedSQL(ticketOpenActionsSQL(t))
	want := "ORDER BY (a.due_at IS NULL),a.due_at ASC,a.created_at DESC,a.id DESC"
	if !strings.Contains(sql, want) {
		t.Fatalf("ticket open actions SQL must preserve overdue/due/null/created/id ordering %q in %s", want, sql)
	}
}

func TestTicketOpenActionsPredicatesStayUserScoped(t *testing.T) {
	sql := normalizedSQL(ticketOpenActionsSQL(t))
	for _, want := range []string{
		"a.user_id=$1",
		"em.user_id=a.user_id",
		"p.user_id=a.user_id",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("ticket open actions SQL missing user scope %q in %s", want, sql)
		}
	}
	if strings.Contains(strings.ToLower(sql), "a.title") && strings.Contains(strings.ToLower(sql), "normalized_value=a.title") {
		t.Fatalf("ticket open actions must not infer ticket associations from action text: %s", sql)
	}
}

func TestTicketOpenActionsRegressionCasesDocumentedBySQLShape(t *testing.T) {
	sql := normalizedSQL(ticketOpenActionsSQL(t))
	cases := map[string]string{
		"ticket with one open action":                                   "FROM actions a",
		"ticket with multiple open actions":                             "LIMIT $3",
		"action whose source note has duplicate ticket entity mentions": "EXISTS (SELECT 1 FROM entity_mentions em",
		"no duplicate actions in the response":                          "SELECT a.id,a.note_id",
		"overdue ordering":                                              "a.due_at ASC",
		"null due date ordering":                                        "(a.due_at IS NULL)",
		"deterministic tie ordering":                                    "a.id DESC",
		"completed actions excluded":                                    "a.status='open'",
		"foreign-user actions excluded":                                 "a.user_id=$1",
		"ticket detail returns 200 instead of 500":                      "ticket_repository.list_open_actions",
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(sql, want) {
				t.Fatalf("SQL shape for %s missing %q in %s", name, want, sql)
			}
		})
	}
}
