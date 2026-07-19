package dto

import "time"

type Me struct {
	DisplayName      string `json:"display_name,omitempty"`
	ResponseLanguage string `json:"response_language"`
	Timezone         string `json:"timezone"`
}
type Note struct {
	ID               string    `json:"id"`
	RawText          string    `json:"raw_text"`
	Summary          *string   `json:"summary"`
	Tags             []string  `json:"tags"`
	ProcessingStatus string    `json:"processing_status"`
	CreatedAt        time.Time `json:"created_at"`
}
type Action struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Status       string        `json:"status"`
	LinkedPerson *LinkedPerson `json:"linked_person"`
	DueAt        *time.Time    `json:"due_at"`
	CreatedAt    time.Time     `json:"created_at"`
	CompletedAt  *time.Time    `json:"completed_at"`
	SourceNoteID *string       `json:"source_note_id"`
}
type LinkedPerson struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}
type ExtractedCounts struct {
	Actions   int `json:"actions"`
	People    int `json:"people"`
	Decisions int `json:"decisions"`
	Entities  int `json:"entities"`
}
type NotePreview struct {
	People  []LinkedPerson `json:"people"`
	Tickets []string       `json:"tickets"`
	Tags    []string       `json:"tags"`
}
type TodayNote struct {
	ID               string          `json:"id"`
	RawText          string          `json:"raw_text"`
	Summary          *string         `json:"summary"`
	ProcessingStatus string          `json:"processing_status"`
	CreatedAt        time.Time       `json:"created_at"`
	ProcessedAt      *time.Time      `json:"processed_at"`
	ExtractedCounts  ExtractedCounts `json:"extracted_counts"`
	Preview          NotePreview     `json:"preview"`
}
type DailyCounters struct {
	OpenLoops       int `json:"open_loops"`
	Decisions       int `json:"decisions"`
	PeopleMentioned int `json:"people_mentioned"`
}
type DailySummary struct {
	Date         string        `json:"date"`
	Status       string        `json:"status"`
	ShortSummary string        `json:"short_summary,omitempty"`
	Counters     DailyCounters `json:"counters"`
	GeneratedAt  *time.Time    `json:"generated_at"`
}
type Today struct {
	Date         string       `json:"date"`
	Timezone     string       `json:"timezone"`
	Notes        []TodayNote  `json:"notes"`
	OpenActions  []Action     `json:"open_actions"`
	DailySummary DailySummary `json:"daily_summary"`
}
type Highlight struct {
	Type  string `json:"type"`
	Theme string `json:"theme"`
	Text  string `json:"text"`
}
type PersonDetail struct {
	ID          string      `json:"id"`
	DisplayName string      `json:"display_name"`
	Highlights  []Highlight `json:"highlights"`
}
type Decision struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Status string `json:"status"`
	Topic  string `json:"topic,omitempty"`
}
type Entity struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}
type NoteDetail struct {
	ID               string         `json:"id"`
	RawText          string         `json:"raw_text"`
	Summary          *string        `json:"summary"`
	ProcessingStatus string         `json:"processing_status"`
	CreatedAt        time.Time      `json:"created_at"`
	ProcessedAt      *time.Time     `json:"processed_at"`
	Actions          []Action       `json:"actions"`
	People           []PersonDetail `json:"people"`
	Decisions        []Decision     `json:"decisions"`
	Entities         []Entity       `json:"entities"`
}
type Pagination struct {
	Limit      int     `json:"limit"`
	NextCursor *string `json:"next_cursor"`
}
type NotesPage struct {
	NextCursor *string `json:"next_cursor"`
	HasMore    bool    `json:"has_more"`
}
type NotesHistory struct {
	Items []TodayNote `json:"items"`
	Page  NotesPage   `json:"page"`
}

type PeopleRecentNote struct {
	ID               string    `json:"id"`
	CreatedAt        time.Time `json:"created_at"`
	Summary          *string   `json:"summary"`
	RawTextPreview   string    `json:"raw_text_preview,omitempty"`
	ProcessingStatus string    `json:"processing_status,omitempty"`
	Tickets          []string  `json:"tickets,omitempty"`
}
type PeopleListItem struct {
	ID              string            `json:"id"`
	DisplayName     string            `json:"display_name"`
	Aliases         []string          `json:"aliases"`
	LastMentionedAt time.Time         `json:"last_mentioned_at"`
	MentionCount    int               `json:"mention_count"`
	OpenActionCount int               `json:"open_action_count"`
	RecentNote      *PeopleRecentNote `json:"recent_note"`
}
type PeopleList struct {
	Items []PeopleListItem `json:"items"`
	Page  NotesPage        `json:"page"`
}
type PersonProfile struct {
	ID               string    `json:"id"`
	DisplayName      string    `json:"display_name"`
	Aliases          []string  `json:"aliases"`
	FirstMentionedAt time.Time `json:"first_mentioned_at"`
	LastMentionedAt  time.Time `json:"last_mentioned_at"`
	MentionCount     int       `json:"mention_count"`
}
type PersonWorkspaceDetail struct {
	Person          PersonProfile      `json:"person"`
	OpenActions     []Action           `json:"open_actions"`
	RecentDecisions []Decision         `json:"recent_decisions"`
	RecentNotes     []PeopleRecentNote `json:"recent_notes"`
	Page            NotesPage          `json:"page"`
}

type TicketRecentNote struct {
	ID               string         `json:"id"`
	CreatedAt        time.Time      `json:"created_at"`
	Summary          *string        `json:"summary"`
	RawTextPreview   string         `json:"raw_text_preview,omitempty"`
	ProcessingStatus string         `json:"processing_status,omitempty"`
	People           []LinkedPerson `json:"people,omitempty"`
}
type TicketListItem struct {
	Key              string            `json:"key"`
	FirstMentionedAt time.Time         `json:"first_mentioned_at"`
	LastMentionedAt  time.Time         `json:"last_mentioned_at"`
	MentionCount     int               `json:"mention_count"`
	OpenActionCount  int               `json:"open_action_count"`
	RecentNote       *TicketRecentNote `json:"recent_note"`
}
type TicketsList struct {
	Items []TicketListItem `json:"items"`
	Page  NotesPage        `json:"page"`
}
type TicketProfile struct {
	Key              string    `json:"key"`
	FirstMentionedAt time.Time `json:"first_mentioned_at"`
	LastMentionedAt  time.Time `json:"last_mentioned_at"`
	MentionCount     int       `json:"mention_count"`
}
type TicketWorkspaceDetail struct {
	Ticket          TicketProfile      `json:"ticket"`
	OpenActions     []Action           `json:"open_actions"`
	RecentDecisions []Decision         `json:"recent_decisions"`
	RecentNotes     []TicketRecentNote `json:"recent_notes"`
	Page            NotesPage          `json:"page"`
}
