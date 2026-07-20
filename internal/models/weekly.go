package models

type WeeklyDigest struct {
	Summary        string           `json:"summary"`
	Highlights     []WeeklyTextItem `json:"highlights"`
	Actions        []WeeklyTextItem `json:"actions"`
	Decisions      []WeeklyTextItem `json:"decisions"`
	People         []WeeklyTextItem `json:"people"`
	Tickets        []WeeklyTextItem `json:"tickets"`
	Risks          []WeeklyTextItem `json:"risks"`
	OpenQuestions  []WeeklyTextItem `json:"open_questions"`
	RepeatedTopics []WeeklyTextItem `json:"repeated_topics"`
}

type WeeklyTextItem struct {
	Text          string  `json:"text"`
	SourceNoteIDs []int64 `json:"source_note_ids,omitempty"`
}
