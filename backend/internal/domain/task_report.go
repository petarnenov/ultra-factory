package domain

import "time"

// StageTransition is a polled, approximate entry into a derived Kanban
// column; report history only, never a session's current status.
type StageTransition struct {
	Stage     KanbanColumn
	EnteredAt time.Time
}

// ConfigChangeSource names which record a configuration change came from.
type ConfigChangeSource string

// Configuration change sources.
const (
	ConfigChangeSession      ConfigChangeSource = "session"
	ConfigChangeConversation ConfigChangeSource = "conversation"
)

// ConfigChange is one recorded harness, model, or effort change. An empty
// conversation model or effort means the session's own value applies.
type ConfigChange struct {
	Source     ConfigChangeSource
	Harness    string
	Model      string
	Effort     string
	RecordedAt time.Time
}

// UsageRun sums one run root's usage for one model. InputTokens include
// cached input; a known count below EventCount means a partial sum.
type UsageRun struct {
	BindingID             int64
	Harness               AgentHarness
	NativeRootID          string
	BindingState          UsageBindingState
	ModelID               string
	EventCount            int64
	InputTokens           int64
	KnownInputCount       int64
	CachedInputTokens     int64
	KnownCachedInputCount int64
	OutputTokens          int64
	KnownOutputCount      int64
}
