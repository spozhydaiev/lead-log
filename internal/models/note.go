package models

import "time"

type ParsedNote struct {
	Summary            string             `json:"summary"`
	Tags               []string           `json:"tags"`
	Actions            []ParsedAction     `json:"actions"`
	PeopleNotes        []ParsedPeopleNote `json:"people_notes"`
	PeopleMentioned    []string           `json:"people_mentioned"`
	TicketDrafts       []TicketDraft      `json:"ticket_drafts"`
	SuggestedQuestions []string           `json:"suggested_questions"`
}

type ParsedAction struct {
	Title            string  `json:"title"`
	LinkedPersonName string  `json:"linked_person_name,omitempty"`
	OutputType       string  `json:"output_type,omitempty"` // ticket, meeting, message, reminder
	SourceNoteIDs    []int64 `json:"source_note_ids,omitempty"`
}

type ParsedPeopleNote struct {
	PersonName      string  `json:"person_name"`
	Type            string  `json:"type"`  // positive_signal, concern, growth_topic, context, follow_up_needed, commitment, decision, risk, blocker, review_evidence
	Theme           string  `json:"theme"` // ownership, communication, delivery, collaboration, technical_quality, reliability, mentorship, process
	Text            string  `json:"text"`
	IncludeInReview bool    `json:"include_in_review"`
	SourceNoteIDs   []int64 `json:"source_note_ids,omitempty"`
}

type TicketDraft struct {
	Title              string   `json:"title"`
	Context            string   `json:"context"`
	Problem            string   `json:"problem"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
}

type Action struct {
	ID         int64
	Title      string
	Status     string
	OutputType string
	CreatedAt  time.Time
	PersonName *string
}

type PersonContext struct {
	PersonName string
	Notes      []PersonContextNote
	Actions    []Action
}

type PersonContextNote struct {
	Type      string
	Theme     string
	Text      string
	NoteID    int64
	CreatedAt time.Time
}
