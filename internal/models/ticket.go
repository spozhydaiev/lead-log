package models

import "time"

const (
	TicketAssociationDirect   = "direct"
	TicketAssociationPossible = "possible"
)

type TicketContext struct {
	TicketKey      string
	FirstMentionAt *time.Time
	LastMentionAt  *time.Time
	Mentions       []TicketMention
	Actions        []TicketAction
	Decisions      []TicketDecision
	KnownStatus    string
	Sources        []TicketSource
}

type TicketMention struct {
	SourceNoteID int64
	Date         time.Time
	Snippet      string
	FromEntity   bool
}

type TicketAction struct {
	ID              int64
	Title           string
	Status          string
	PersonName      string
	SourceNoteID    int64
	Date            time.Time
	AssociationType string
}

type TicketDecision struct {
	ID              int64
	Text            string
	Status          string
	SourceNoteID    int64
	Date            time.Time
	AssociationType string
}

type TicketSource struct {
	NoteID int64
	Date   time.Time
}
