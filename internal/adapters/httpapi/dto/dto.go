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
	DisplayName string `json:"display_name"`
}
type Pagination struct {
	Limit      int     `json:"limit"`
	NextCursor *string `json:"next_cursor"`
}
