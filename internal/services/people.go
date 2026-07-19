package services

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/spozhydaiev/lead-log/internal/adapters/store"
)

const (
	PeopleAliasesLimit         = 10
	PeopleRecentNotesLimit     = 20
	PeopleOpenActionsLimit     = 20
	PeopleRecentDecisionsLimit = 10
)

type PeopleListFilter = store.PeopleListFilter
type PeopleListView struct{ Items []store.PeopleListItem }
type PersonWorkspaceView struct {
	Person          store.PersonProfile
	OpenActions     []store.APIAction
	RecentDecisions []store.DecisionView
	RecentNotes     []store.PeopleRecentNote
}

func (s *Service) ListPeople(ctx context.Context, userID int64, f PeopleListFilter, limit int, c *store.PeoplePageCursor) (PeopleListView, error) {
	items, err := s.store.ListPeopleWorkspace(ctx, userID, f, limit, c)
	return PeopleListView{Items: items}, err
}

func (s *Service) GetPersonWorkspace(ctx context.Context, userID, personID int64) (PersonWorkspaceView, error) {
	p, err := s.store.GetPersonWorkspaceProfile(ctx, userID, personID, PeopleAliasesLimit)
	if err != nil {
		return PersonWorkspaceView{}, err
	}
	v := PersonWorkspaceView{Person: p, OpenActions: []store.APIAction{}, RecentDecisions: []store.DecisionView{}, RecentNotes: []store.PeopleRecentNote{}}
	if v.OpenActions, err = s.store.OpenActionsForPerson(ctx, userID, personID, PeopleOpenActionsLimit); err != nil {
		return v, err
	}
	if v.RecentDecisions, err = s.store.RecentDecisionsForPerson(ctx, userID, personID, PeopleRecentDecisionsLimit); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return v, err
		}
	}
	if v.RecentNotes, err = s.store.RecentNotesForPerson(ctx, userID, personID, PeopleRecentNotesLimit); err != nil {
		return v, err
	}
	return v, nil
}
