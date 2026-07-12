package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/spozhydaiev/lead-log/internal/models"
)

func TestRetrievalEmptyQueryValidation(t *testing.T) {
	s := New(nil, nil)
	_, err := s.Retrieve(context.Background(), models.RetrievalQuery{UserID: 1})
	if !errors.Is(err, ErrInvalidRetrievalQuery) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestRetrievalKindsDefaultAndDedupe(t *testing.T) {
	kinds, err := retrievalKinds(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(kinds) != 5 {
		t.Fatalf("expected all retrieval kinds by default, got %v", kinds)
	}
	_, err = retrievalKinds([]models.RetrievalKind{"bad"})
	if !errors.Is(err, ErrInvalidRetrievalQuery) {
		t.Fatalf("expected invalid kind error, got %v", err)
	}
}

func TestNormalizeRetrievalTicketEntity(t *testing.T) {
	for _, input := range []string{"ch-1234", "Ch-1234", "CH-1234"} {
		got, err := normalizeRetrievalEntity(models.EntityTypeTicket, input)
		if err != nil {
			t.Fatalf("normalize %q: %v", input, err)
		}
		if got != "CH-1234" {
			t.Fatalf("expected CH-1234, got %q", got)
		}
	}
}

func TestRetrievalDateBoundaryValidation(t *testing.T) {
	now := time.Now()
	from := now
	to := now
	s := New(nil, nil)
	_, err := s.Retrieve(context.Background(), models.RetrievalQuery{UserID: 1, Text: "retry", From: &from, To: &to})
	if !errors.Is(err, ErrInvalidRetrievalQuery) {
		t.Fatalf("expected invalid date boundary error, got %v", err)
	}
}
