package models

import "time"

type AgentResponse struct {
	ID            int64
	UserID        int64
	Kind          string
	ScopeKey      string
	PeriodStart   *time.Time
	PeriodEnd     *time.Time
	SourceHash    string
	PromptVersion string
	Model         string
	ResponseText  string
	ResponseJSON  string
	CreatedAt     time.Time
	LastUsedAt    time.Time
}
