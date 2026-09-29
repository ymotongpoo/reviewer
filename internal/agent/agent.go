// Package agent abstracts coding agents (Hermes Agent today; Claude Code and
// Codex later) that reviewer can hand a round of feedback to directly.
package agent

import (
	"context"
	"errors"
	"time"
)

// Session is a conversation of the agent that feedback can be sent into.
type Session struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Source    string    `json:"source"` // e.g. "discord", "cli"
	Preview   string    `json:"preview,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Event types.
const (
	EventStarted  = "started"  // the agent accepted the turn
	EventText     = "text"     // a chunk of the agent's answer
	EventThinking = "thinking" // a chunk of reasoning
	EventTool     = "tool"     // a tool call started, completed or failed
	EventApproval = "approval" // the agent waits for an approval
	EventAnswered = "answered" // an approval was answered
	EventDone     = "done"     // the turn ended; Status tells how
	EventError    = "error"    // transport or protocol error; the run is over
)

// Terminal statuses of EventDone.
const (
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// Approval is a pending approval request.
type Approval struct {
	ID          string   `json:"id,omitempty"`
	Command     string   `json:"command,omitempty"`
	Description string   `json:"description,omitempty"`
	Choices     []string `json:"choices"`
}

// Event is one step of a run.
type Event struct {
	Type      string    `json:"type"`
	Text      string    `json:"text,omitempty"`
	Tool      string    `json:"tool,omitempty"`
	ToolState string    `json:"toolState,omitempty"` // started | completed | failed
	Status    string    `json:"status,omitempty"`
	Approval  *Approval `json:"approval,omitempty"`
	At        time.Time `json:"at"`
}

// Request starts a turn in a session.
type Request struct {
	SessionID string
	Prompt    string
	// IdempotencyKey makes a retried start return the same run.
	IdempotencyKey string
}

// Agent is a connection to an agent.
type Agent interface {
	// Kind is a short identifier such as "hermes".
	Kind() string
	// Name is shown in the UI, e.g. "Hermes Agent".
	Name() string
	Sessions(ctx context.Context) ([]Session, error)
	Start(ctx context.Context, r Request) (Run, error)
}

// SessionCreator is optionally implemented by agents that can create an empty
// titled session before a run starts.
type SessionCreator interface {
	CreateSession(ctx context.Context, title string) (Session, error)
}

// ErrTitleInUse indicates that a session title is already taken.
var ErrTitleInUse = errors.New("session title already in use")

// Run is a turn in progress.
type Run interface {
	ID() string
	// Events delivers the run's events and is closed after EventDone or
	// EventError.
	Events() <-chan Event
	Stop(ctx context.Context) error
	// Answer resolves a pending approval with one of its Choices.
	Answer(ctx context.Context, approvalID, choice string) error
}

// Notifier posts short status messages where the user talks to the agent,
// e.g. the Discord thread of the session.
type Notifier interface {
	Notify(ctx context.Context, sessionID, text string) error
}

// NopNotifier discards notifications.
type NopNotifier struct{}

// Notify implements Notifier.
func (NopNotifier) Notify(context.Context, string, string) error { return nil }
