package models

import "time"

type RetrievalKind string

const (
	RetrievalKindNote          RetrievalKind = "note"
	RetrievalKindAction        RetrievalKind = "action"
	RetrievalKindPeopleNote    RetrievalKind = "people_note"
	RetrievalKindDecision      RetrievalKind = "decision"
	RetrievalKindEntityMention RetrievalKind = "entity_mention"
)

type RetrievalItem struct {
	Kind         RetrievalKind
	RecordID     int64
	SourceNoteID int64
	UserID       int64
	CreatedAt    time.Time
	Title        string
	Text         string
	Context      string
	PersonID     *int64
	PersonName   string
	EntityType   string
	EntityValue  string
	Status       string
	Score        float64
}

type RetrievalQuery struct {
	UserID int64
	Text   string
	From   *time.Time
	To     *time.Time
	Kinds  []RetrievalKind

	PersonID   *int64
	PersonName string

	EntityType  string
	EntityValue string

	ActionStatuses   []string
	PeopleNoteTypes  []string
	DecisionStatuses []string
	DecisionTopic    string
	Limit            int
}
