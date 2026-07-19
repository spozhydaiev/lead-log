package services

import (
	"context"
	"encoding/json"
	"time"

	"github.com/spozhydaiev/lead-log/internal/adapters/store"
	"github.com/spozhydaiev/lead-log/internal/models"
)

const TodayLimit = 10
const NoteChildrenLimit = 50

type TodayView struct {
	Date, Timezone string
	Notes          []TodayNote
	OpenActions    []store.APIAction
	DailySummary   TodayDailySummary
}
type TodayNote struct {
	store.APINote
	Counts   store.TodayCounts
	People   []store.PersonHighlight
	Entities []store.EntityView
}
type NotesHistoryFilter = store.NotesListFilter
type NotesHistoryView struct{ Notes []TodayNote }
type TodayDailySummary struct {
	Status, ShortSummary                  string
	OpenLoops, Decisions, PeopleMentioned int
	GeneratedAt                           *time.Time
}
type NoteDetailView struct {
	Note      store.NoteDetailRecord
	Actions   []store.APIAction
	People    []store.PersonHighlight
	Decisions []store.DecisionView
	Entities  []store.EntityView
}

func (s *Service) GetToday(ctx context.Context, userID int64, now time.Time) (TodayView, error) {
	loc := s.dailyLocation
	if loc == nil {
		loc = time.Local
	}
	local := now.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)
	v := TodayView{Date: start.Format("2006-01-02"), Timezone: loc.String(), Notes: []TodayNote{}, OpenActions: []store.APIAction{}, DailySummary: TodayDailySummary{Status: "not_available"}}
	notes, err := s.store.ListTodayNotes(ctx, userID, start, end, TodayLimit)
	if err != nil {
		return v, err
	}
	ids := make([]int64, len(notes))
	for i, n := range notes {
		ids[i] = n.ID
	}
	counts, err := s.store.NoteCounts(ctx, userID, ids)
	if err != nil {
		return v, err
	}
	people, err := s.store.HighlightsForNotes(ctx, userID, ids, 3)
	if err != nil {
		return v, err
	}
	entities, err := s.store.EntitiesForNotes(ctx, userID, ids, 6)
	if err != nil {
		return v, err
	}
	for _, n := range notes {
		v.Notes = append(v.Notes, TodayNote{APINote: n, Counts: counts[n.ID], People: people[n.ID], Entities: entities[n.ID]})
	}
	v.OpenActions, err = s.store.ListAPIActions(ctx, userID, "open", TodayLimit, nil)
	if err != nil {
		return v, err
	}
	if v.OpenActions == nil {
		v.OpenActions = []store.APIAction{}
	}
	cached, err := s.store.LatestDailyCache(ctx, userID, v.Date)
	if err != nil {
		return v, err
	}
	if cached != nil {
		var d models.DailyDigest
		if json.Unmarshal([]byte(cached.ResponseJSON), &d) == nil {
			v.DailySummary = TodayDailySummary{Status: "available", ShortSummary: d.ShortSummary, OpenLoops: len(d.OpenLoops), Decisions: len(d.Decisions), PeopleMentioned: len(d.PeopleHighlights), GeneratedAt: &cached.CreatedAt}
		}
	}
	return v, nil
}

func (s *Service) GetNoteDetail(ctx context.Context, userID, noteID int64) (NoteDetailView, error) {
	n, err := s.store.NoteDetail(ctx, userID, noteID)
	if err != nil {
		return NoteDetailView{}, err
	}
	v := NoteDetailView{Note: n, Actions: []store.APIAction{}, People: []store.PersonHighlight{}, Decisions: []store.DecisionView{}, Entities: []store.EntityView{}}
	if v.Actions, err = s.store.ActionsForNote(ctx, userID, noteID, NoteChildrenLimit); err != nil {
		return v, err
	}
	pm, err := s.store.HighlightsForNotes(ctx, userID, []int64{noteID}, NoteChildrenLimit)
	if err != nil {
		return v, err
	}
	v.People = pm[noteID]
	if v.People == nil {
		v.People = []store.PersonHighlight{}
	}
	if v.Decisions, err = s.store.DecisionsForNote(ctx, userID, noteID, NoteChildrenLimit); err != nil {
		return v, err
	}
	em, err := s.store.EntitiesForNotes(ctx, userID, []int64{noteID}, NoteChildrenLimit)
	if err != nil {
		return v, err
	}
	v.Entities = em[noteID]
	if v.Actions == nil {
		v.Actions = []store.APIAction{}
	}
	if v.Decisions == nil {
		v.Decisions = []store.DecisionView{}
	}
	if v.Entities == nil {
		v.Entities = []store.EntityView{}
	}
	return v, nil
}
