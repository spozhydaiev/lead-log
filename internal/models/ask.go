package models

import "time"

const (
	AskIntentGeneralContext = "general_context"
	AskIntentActivity       = "activity"
	AskIntentCommitments    = "commitments"
	AskIntentOpenActions    = "open_actions"
	AskIntentOpenQuestions  = "open_questions"
	AskIntentPersonContext  = "person_context"
	AskIntentEntityHistory  = "entity_history"
	AskIntentDecisions      = "decisions"
	AskIntentLatestMention  = "latest_mention"
	AskIntentRepeatedTopics = "repeated_topics"
)

const (
	AskDateToday        = "today"
	AskDateYesterday    = "yesterday"
	AskDateCurrentWeek  = "current_week"
	AskDatePreviousWeek = "previous_week"
	AskDateLast7Days    = "last_7_days"
	AskDateCurrentMonth = "current_month"
	AskDateLast30Days   = "last_30_days"
	AskDateAllTime      = "all_time"
	AskDateExplicit     = "explicit"
	AskDateUnspecified  = "unspecified"
)

type AskEntity struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}
type AskDateRange struct {
	Type string     `json:"type"`
	From *time.Time `json:"from,omitempty"`
	To   *time.Time `json:"to,omitempty"`
}
type AskIntent struct {
	IntentType       string          `json:"intent_type"`
	TextQuery        string          `json:"text_query"`
	People           []string        `json:"people"`
	Entities         []AskEntity     `json:"entities"`
	DateRange        AskDateRange    `json:"date_range"`
	Kinds            []RetrievalKind `json:"kinds"`
	ActionStatuses   []string        `json:"action_statuses"`
	PeopleNoteTypes  []string        `json:"people_note_types"`
	DecisionStatuses []string        `json:"decision_statuses"`
	SortOrder        string          `json:"sort_order"`
	Limit            int             `json:"limit"`
}

type AskCandidate struct {
	Kind         RetrievalKind `json:"kind"`
	SourceNoteID int64         `json:"source_note_id"`
	Date         string        `json:"date"`
	Title        string        `json:"title"`
	Text         string        `json:"text"`
	PersonName   string        `json:"person_name,omitempty"`
	EntityType   string        `json:"entity_type,omitempty"`
	EntityValue  string        `json:"entity_value,omitempty"`
	Status       string        `json:"status,omitempty"`
}

type AskAnswerItem struct {
	Text          string   `json:"text"`
	SourceNoteIDs []int64  `json:"source_note_ids"`
	SourceDates   []string `json:"source_dates"`
	Confidence    string   `json:"confidence"`
}
type AskAnswer struct {
	Answer           string          `json:"answer"`
	Items            []AskAnswerItem `json:"items"`
	InsufficientData bool            `json:"insufficient_data"`
	Caveats          []string        `json:"caveats"`
}
