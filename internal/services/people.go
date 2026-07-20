package services

import (
	"context"
	"errors"
	"strings"
	"time"

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
	if v.OpenActions, err = s.store.OpenActionsForPerson(ctx, userID, p.PersonID, PeopleOpenActionsLimit); err != nil {
		return v, err
	}
	if v.RecentDecisions, err = s.store.RecentDecisionsForPerson(ctx, userID, p.PersonID, PeopleRecentDecisionsLimit); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return v, err
		}
	}
	if v.RecentNotes, err = s.store.RecentNotesForPerson(ctx, userID, p.PersonID, PeopleRecentNotesLimit); err != nil {
		return v, err
	}
	return v, nil
}

var (
	ErrPersonValidation       = errors.New("person validation failed")
	ErrPersonIdentityConflict = store.ErrPersonIdentityConflict
	ErrPersonUpdateConflict   = store.ErrPersonUpdateConflict
)

type NullableString struct {
	Set   bool
	Value *string
}
type UpdatePersonProfileInput struct {
	DisplayName       NullableString
	FirstName         NullableString
	LastName          NullableString
	JobTitle          NullableString
	Team              NullableString
	Company           NullableString
	Notes             NullableString
	AliasesSet        bool
	Aliases           []string
	ExpectedUpdatedAt *time.Time
}

const (
	personNameMax       = 200
	personFieldMax      = 200
	personNotesMax      = 10000
	personAliasMax      = 200
	personAliasCountMax = 25
)

func (s *Service) UpdatePersonProfile(ctx context.Context, userID, personID int64, in UpdatePersonProfileInput) (store.PersonProfile, error) {
	clean, err := validatePersonProfileUpdate(in)
	if err != nil {
		return store.PersonProfile{}, err
	}
	return s.store.UpdatePersonProfile(ctx, userID, personID, clean, PeopleAliasesLimit)
}

func validatePersonProfileUpdate(in UpdatePersonProfileInput) (store.UpdatePersonProfileInput, error) {
	var out store.UpdatePersonProfileInput
	convReq := func(v NullableString, max int) (store.OptionalStringUpdate, error) {
		if !v.Set {
			return store.OptionalStringUpdate{}, nil
		}
		if v.Value == nil {
			return store.OptionalStringUpdate{Set: true}, ErrPersonValidation
		}
		x := strings.TrimSpace(*v.Value)
		if x == "" || len([]rune(x)) > max || hasControl(x, false) {
			return store.OptionalStringUpdate{}, ErrPersonValidation
		}
		return store.OptionalStringUpdate{Set: true, Value: &x}, nil
	}
	convOpt := func(v NullableString, max int, multiline bool) (store.OptionalStringUpdate, error) {
		if !v.Set {
			return store.OptionalStringUpdate{}, nil
		}
		if v.Value == nil {
			return store.OptionalStringUpdate{Set: true, Value: nil}, nil
		}
		x := strings.TrimSpace(*v.Value)
		if x == "" {
			return store.OptionalStringUpdate{Set: true, Value: nil}, nil
		}
		if len([]rune(x)) > max || hasControl(x, multiline) {
			return store.OptionalStringUpdate{}, ErrPersonValidation
		}
		return store.OptionalStringUpdate{Set: true, Value: &x}, nil
	}
	var err error
	if out.DisplayName, err = convReq(in.DisplayName, personNameMax); err != nil {
		return out, err
	}
	if out.FirstName, err = convOpt(in.FirstName, personFieldMax, false); err != nil {
		return out, err
	}
	if out.LastName, err = convOpt(in.LastName, personFieldMax, false); err != nil {
		return out, err
	}
	if out.JobTitle, err = convOpt(in.JobTitle, personFieldMax, false); err != nil {
		return out, err
	}
	if out.Team, err = convOpt(in.Team, personFieldMax, false); err != nil {
		return out, err
	}
	if out.Company, err = convOpt(in.Company, personFieldMax, false); err != nil {
		return out, err
	}
	if out.Notes, err = convOpt(in.Notes, personNotesMax, true); err != nil {
		return out, err
	}
	out.ExpectedUpdatedAt = in.ExpectedUpdatedAt
	out.AliasesSet = in.AliasesSet
	if in.AliasesSet {
		if len(in.Aliases) > personAliasCountMax {
			return out, ErrPersonValidation
		}
		seen := map[string]bool{}
		for _, a := range in.Aliases {
			x := strings.TrimSpace(a)
			if x == "" || len([]rune(x)) > personAliasMax || hasControl(x, false) {
				return out, ErrPersonValidation
			}
			n := store.NormalizePersonName(x)
			if n == "" {
				return out, ErrPersonValidation
			}
			if !seen[n] {
				out.Aliases = append(out.Aliases, x)
				seen[n] = true
			}
		}
	}
	return out, nil
}

func hasControl(s string, multiline bool) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			if multiline && (r == '\n' || r == '\r' || r == '\t') {
				continue
			}
			return true
		}
	}
	return false
}

var ErrPersonMergeConflict = store.ErrPersonMergeConflict

type MergePeopleInput struct {
	SourcePersonID           int64
	ExpectedPrimaryUpdatedAt *time.Time
	ExpectedSourceUpdatedAt  *time.Time
	ProfileResolution        map[string]string
}

type PersonMergeResult = store.PersonMergeResult

func (s *Service) MergePeople(ctx context.Context, userID, primaryID int64, in MergePeopleInput) (PersonMergeResult, error) {
	if primaryID == in.SourcePersonID || primaryID <= 0 || in.SourcePersonID <= 0 {
		return PersonMergeResult{}, ErrPersonValidation
	}
	allowedFields := map[string]bool{"display_name": true, "first_name": true, "last_name": true, "job_title": true, "team": true, "company": true, "notes": true}
	allowedValues := map[string]bool{"primary": true, "source": true, "append": true}
	for k, v := range in.ProfileResolution {
		if !allowedFields[k] || !allowedValues[v] || (v == "append" && k != "notes") {
			return PersonMergeResult{}, ErrPersonValidation
		}
	}
	return s.store.MergePeople(ctx, userID, primaryID, in.SourcePersonID, store.MergePersonProfileInput{ExpectedPrimaryUpdatedAt: in.ExpectedPrimaryUpdatedAt, ExpectedSourceUpdatedAt: in.ExpectedSourceUpdatedAt, ProfileResolution: in.ProfileResolution}, PeopleAliasesLimit)
}
