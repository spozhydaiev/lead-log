package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/spozhydaiev/lead-log/internal/adapters/store"
	"github.com/spozhydaiev/lead-log/internal/models"
)

const (
	TicketsRecentNotesLimit     = 20
	TicketsOpenActionsLimit     = 20
	TicketsRecentDecisionsLimit = 10
	TicketsPeoplePreviewLimit   = 3
)

type TicketsListFilter = store.TicketsListFilter
type TicketsListView struct{ Items []store.TicketListItem }
type TicketWorkspaceView struct {
	Ticket          store.TicketProfile
	OpenActions     []store.APIAction
	RecentDecisions []store.DecisionView
	RecentNotes     []store.TicketRecentNote
}

func NormalizeWorkspaceTicketKey(raw string) (string, bool) { return models.NormalizeTicketKey(raw) }

func (s *Service) ListTickets(ctx context.Context, userID int64, f TicketsListFilter, limit int, c *store.TicketsPageCursor) (TicketsListView, error) {
	items, err := s.store.ListTicketsWorkspace(ctx, userID, f, limit, c)
	if err != nil {
		return TicketsListView{}, fmt.Errorf("list tickets workspace: %w", err)
	}
	return TicketsListView{Items: items}, nil
}

func (s *Service) GetTicketWorkspace(ctx context.Context, userID int64, key string) (TicketWorkspaceView, error) {
	profile, err := s.store.GetTicketWorkspaceProfile(ctx, userID, key)
	if err != nil {
		return TicketWorkspaceView{}, fmt.Errorf("load ticket workspace profile: %w", err)
	}
	v := TicketWorkspaceView{Ticket: profile, OpenActions: []store.APIAction{}, RecentDecisions: []store.DecisionView{}, RecentNotes: []store.TicketRecentNote{}}
	if v.OpenActions, err = s.store.OpenActionsForTicket(ctx, userID, key, TicketsOpenActionsLimit); err != nil {
		return v, fmt.Errorf("load ticket open actions: %w", err)
	}
	if v.RecentDecisions, err = s.store.RecentDecisionsForTicket(ctx, userID, key, TicketsRecentDecisionsLimit); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return v, fmt.Errorf("load ticket recent decisions: %w", err)
	}
	if v.RecentNotes, err = s.store.RecentNotesForTicket(ctx, userID, key, TicketsRecentNotesLimit); err != nil {
		return v, fmt.Errorf("load ticket recent notes: %w", err)
	}
	ids := make([]int64, 0, len(v.RecentNotes))
	for _, n := range v.RecentNotes {
		ids = append(ids, n.ID)
	}
	people, err := s.store.PeopleForNotes(ctx, userID, ids, TicketsPeoplePreviewLimit)
	if err != nil {
		return v, fmt.Errorf("load ticket people preview: %w", err)
	}
	for i := range v.RecentNotes {
		v.RecentNotes[i].People = people[v.RecentNotes[i].ID]
	}
	return v, nil
}
