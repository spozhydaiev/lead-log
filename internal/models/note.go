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
	Decisions          []ParsedDecision   `json:"decisions,omitempty"`
	EntityMentions     []EntityMention    `json:"entity_mentions,omitempty"`
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

type ParsedDecision struct {
	Text             string `json:"text"`
	LinkedPersonName string `json:"linked_person_name,omitempty"`
	Topic            string `json:"topic,omitempty"`
}

type EntityMention struct {
	Type         string `json:"type"`
	Value        string `json:"value"`
	RawValue     string `json:"raw_value,omitempty"`
	DisplayValue string `json:"display_value,omitempty"`
	Context      string `json:"context,omitempty"`
}

const (
	EntityTypeTicket     = "ticket"
	EntityTypeProject    = "project"
	EntityTypeService    = "service"
	EntityTypeComponent  = "component"
	EntityTypeRepository = "repository"
	EntityTypeDocument   = "document"
	EntityTypeOther      = "other"
	DecisionStatusActive = "active"
)

type TicketDraft struct {
	Title              string   `json:"title"`
	Context            string   `json:"context"`
	Problem            string   `json:"problem"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
}

type Action struct {
	ID            int64
	Title         string
	Status        string
	OutputType    string
	CreatedAt     time.Time
	PersonName    *string
	SourceNoteIDs []int64
}

type PersonContext struct {
	CanonicalName    string
	LastMentionAt    *time.Time
	MentionCount     int
	RecentNotes      []PersonContextItem
	OpenActions      []PersonContextAction
	CompletedActions []PersonContextAction
	Commitments      []PersonContextItem
	FollowUps        []PersonContextItem
	Feedback         []PersonContextItem
	Achievements     []PersonContextItem
	Concerns         []PersonContextItem
	Decisions        []PersonContextDecision
	OpenQuestions    []PersonContextItem
	PossibleMentions []PersonContextItem
	Sources          []PersonContextSource
}

type PersonContextItem struct {
	Text         string
	Type         string
	SourceNoteID int64
	Date         time.Time
}
type PersonContextAction struct {
	ID           int64
	Title        string
	Status       string
	SourceNoteID int64
	Date         time.Time
	DueAt        *time.Time
}
type PersonContextDecision struct {
	Text         string
	Topic        string
	Status       string
	SourceNoteID int64
	Date         time.Time
}
type PersonContextSource struct {
	NoteID int64
	Date   time.Time
}

type AgendaItemKind string

const (
	AgendaItemOpenAction   AgendaItemKind = "open_action"
	AgendaItemCommitment   AgendaItemKind = "commitment"
	AgendaItemFollowUp     AgendaItemKind = "follow_up"
	AgendaItemOpenQuestion AgendaItemKind = "open_question"
	AgendaItemConcern      AgendaItemKind = "concern"
	AgendaItemDecision     AgendaItemKind = "decision"
	AgendaItemAchievement  AgendaItemKind = "achievement"
	AgendaItemContext      AgendaItemKind = "context"
)

type AgendaPriority string

const (
	AgendaPriorityHigh   AgendaPriority = "high"
	AgendaPriorityNormal AgendaPriority = "normal"
	AgendaPriorityLow    AgendaPriority = "low"
)

type AgendaItem struct {
	Kind         AgendaItemKind
	Text         string
	SourceNoteID int64
	SourceDate   time.Time
	ActionID     *int64
	DueAt        *time.Time
	Priority     AgendaPriority
	IsInferred   bool
}
type AgendaSource struct {
	NoteID int64
	Date   time.Time
}
type PersonAgenda struct {
	CanonicalName                                                            string
	GeneratedAt                                                              time.Time
	MustDiscuss, FollowUps, OpenQuestions, Decisions, PositiveNotes, Context []AgendaItem
	Sources                                                                  []AgendaSource
	HiddenMustDiscuss                                                        int
}

type DecisionRecord struct {
	ID               int64
	UserID           int64
	NoteID           int64
	Text             string
	NormalizedText   string
	LinkedPersonID   *int64
	LinkedPersonName *string
	Topic            string
	Status           string
	DecidedAt        time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type EntityMentionRecord struct {
	ID              int64
	UserID          int64
	NoteID          int64
	Type            string
	RawValue        string
	NormalizedValue string
	DisplayValue    string
	Context         string
	CreatedAt       time.Time
}
