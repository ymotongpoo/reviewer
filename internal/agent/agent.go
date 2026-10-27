// Package agent abstracts coding agents (T3 Code and Hermes Agent) that
// reviewer can hand a round of feedback to directly.
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
	Source    string    `json:"source"` // e.g. "discord", "cli", "t3"
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
	// Kind is a short identifier such as "t3" or "hermes".
	Kind() string
	// Name is shown in the UI, e.g. "T3 Code".
	Name() string
	// Sessions lists the sessions that may work on the directory dir.
	// Agents that do not know about directories list all sessions.
	Sessions(ctx context.Context, dir string) ([]Session, error)
	Start(ctx context.Context, r Request) (Run, error)
}

// SessionCreator is optionally implemented by agents that can create an empty
// titled session for the directory dir before a run starts.
type SessionCreator interface {
	CreateSession(ctx context.Context, dir, title string, opts SessionOptions) (Session, error)
}

// SessionConfigurer changes the model and options of an existing session.
// Agents that support only their own defaults do not implement it.
type SessionConfigurer interface {
	ConfigureSession(ctx context.Context, sessionID string, model ModelRef) error
}

// SessionOptions configure a new session. Agents ignore what they do not
// support.
type SessionOptions struct {
	// Model is the model of the session; the zero value lets the agent pick.
	Model ModelRef
}

// ModelRef names a model of a provider and the options chosen for it.
type ModelRef struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	// Options are values of the model's options, e.g. its reasoning effort.
	// Options left out keep their defaults.
	Options []OptionValue `json:"options,omitempty"`
}

// OptionValue is the chosen value of a model option.
type OptionValue struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

// Model is a model that a new session can use.
type Model struct {
	ModelRef
	ProviderName string `json:"providerName"`
	Label        string `json:"label"`
	// Reasoning is the model's reasoning effort option, if it has one.
	Reasoning *ModelOption `json:"reasoning,omitempty"`
}

// ModelOption is an option of a model with a fixed set of values.
type ModelOption struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Choices []Choice `json:"choices"`
	// Default is the value used when none is chosen.
	Default string `json:"default,omitempty"`
}

// Choice is a value of a ModelOption.
type Choice struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// ModelLister is optionally implemented by agents whose new sessions can
// use a chosen model.
type ModelLister interface {
	// Models lists the available models and the one most recently used for
	// the directory dir, if known.
	Models(ctx context.Context, dir string) (models []Model, recent *ModelRef, err error)
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
