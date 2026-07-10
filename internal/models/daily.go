package models

type DailyDigest struct {
	ShortSummary       string                 `json:"short_summary"`
	OpenLoops          []DailyOpenLoop        `json:"open_loops"`
	TicketCandidates   []DailyTicketCandidate `json:"ticket_candidates"`
	PeopleHighlights   []DailyPeopleHighlight `json:"people_highlights"`
	Decisions          []DailyTextItem        `json:"decisions"`
	SuggestedNextSteps []DailyTextItem        `json:"suggested_next_steps"`
	UnclearItems       []DailyTextItem        `json:"unclear_items"`
}

type DailyOpenLoop struct {
	Title         string  `json:"title"`
	Owner         *string `json:"owner"`
	DueHint       *string `json:"due_hint"`
	SourceNoteIDs []int64 `json:"source_note_ids"`
}

type DailyTicketCandidate struct {
	Title         string  `json:"title"`
	Context       string  `json:"context"`
	Owner         *string `json:"owner"`
	SourceNoteIDs []int64 `json:"source_note_ids"`
}

type DailyPeopleHighlight struct {
	PersonName    string  `json:"person_name"`
	Type          string  `json:"type"`
	Theme         string  `json:"theme"`
	Text          string  `json:"text"`
	SourceNoteIDs []int64 `json:"source_note_ids"`
}

type DailyTextItem struct {
	Text          string  `json:"text"`
	SourceNoteIDs []int64 `json:"source_note_ids"`
}
