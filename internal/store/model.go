package store

import (
	"time"

	"github.com/ymotongpoo/reviewer/internal/anchor"
)

// Comment scopes.
const (
	ScopeProject = "project"
	ScopeFile    = "file"
	ScopeLine    = "line"
)

// Comment statuses.
const (
	StatusDraft     = "draft"     // not yet submitted
	StatusOpen      = "open"      // submitted, awaiting the agent
	StatusAddressed = "addressed" // the agent says it is fixed
	StatusWontfix   = "wontfix"   // the agent declined
	StatusQuestion  = "question"  // the agent asked a question
	StatusResolved  = "resolved"  // closed by the human
)

// Reply authors.
const (
	AuthorHuman = "human"
	AuthorAgent = "agent"
)

// Round statuses.
const (
	RoundOpen      = "open"
	RoundSubmitted = "submitted"
)

// Location is where a line comment currently points.
type Location struct {
	Start int          `json:"start"`
	End   int          `json:"end"`
	State anchor.State `json:"state"`
	// Blob is the content hash of the file the location refers to.
	Blob string `json:"blob"`
	// Anchor is the text at the location when it was last found.
	Anchor anchor.Anchor `json:"anchor"`
}

// Reply is an entry in a comment thread.
type Reply struct {
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	Status    string    `json:"status,omitempty"` // agent replies only
	Round     int       `json:"round"`
	Draft     bool      `json:"draft,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Comment is a review comment and its thread.
type Comment struct {
	ID     string `json:"id"`
	Round  int    `json:"round"`
	Scope  string `json:"scope"`
	Path   string `json:"path,omitempty"`
	Label  string `json:"label"`
	Body   string `json:"body"`
	Status string `json:"status"`
	// Anchor is the text the comment was originally written against.
	Anchor    *anchor.Anchor `json:"anchor,omitempty"`
	OrigStart int            `json:"origStart,omitempty"`
	OrigEnd   int            `json:"origEnd,omitempty"`
	OrigBlob  string         `json:"origBlob,omitempty"`
	Loc       *Location      `json:"loc,omitempty"`
	Replies   []Reply        `json:"replies"`
	// ResolvedRound is the round in which the human resolved the comment.
	ResolvedRound int       `json:"resolvedRound,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// State is the global review state.
type State struct {
	Version     int    `json:"version"`
	Round       int    `json:"round"`
	RoundStatus string `json:"roundStatus"`
	NextComment int    `json:"nextComment"`
	NextReply   int    `json:"nextReply"`
}

// Manifest records a round.
type Manifest struct {
	Round       int               `json:"round"`
	OpenedAt    time.Time         `json:"openedAt"`
	SubmittedAt *time.Time        `json:"submittedAt,omitempty"`
	OpenFiles   map[string]string `json:"openFiles"`
	SubmitFiles map[string]string `json:"submitFiles,omitempty"`
	// FeedbackIDs are the comment IDs included in the feedback.
	FeedbackIDs []string      `json:"feedbackIds,omitempty"`
	Response    *ResponseInfo `json:"response,omitempty"`
}

// ResponseInfo describes the last imported agent response of a round.
type ResponseInfo struct {
	Hash       string    `json:"hash"`
	ImportedAt time.Time `json:"importedAt"`
	Summary    string    `json:"summary,omitempty"`
	Count      int       `json:"count"`
	Warnings   []string  `json:"warnings,omitempty"`
	Error      string    `json:"error,omitempty"`
}
