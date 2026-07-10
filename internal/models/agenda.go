package models

type Agenda struct {
	DiscussionTopics         []AgendaDiscussionTopic `json:"discussion_topics"`
	OpenFollowups            []AgendaTextItem        `json:"open_followups"`
	PositiveSignals          []AgendaTextItem        `json:"positive_signals"`
	RisksOrConcernsToClarify []AgendaTextItem        `json:"risks_or_concerns_to_clarify"`
	GrowthTopics             []AgendaTextItem        `json:"growth_topics"`
	SuggestedQuestions       []AgendaTextItem        `json:"suggested_questions"`
}

type AgendaDiscussionTopic struct {
	Title         string  `json:"title"`
	Context       string  `json:"context"`
	SourceNoteIDs []int64 `json:"source_note_ids"`
}

type AgendaTextItem struct {
	Text          string  `json:"text"`
	SourceNoteIDs []int64 `json:"source_note_ids"`
}
