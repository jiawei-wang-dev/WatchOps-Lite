package session

import (
	"context"
	"errors"
	"strings"
	"time"
)

var ErrVersionConflict = errors.New("session summary version conflict")

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
	RoleTool      Role = "tool"
)

type Message struct {
	Role      Role           `json:"role"`
	Content   string         `json:"content"`
	CreatedAt time.Time      `json:"created_at"`
	RequestID string         `json:"request_id,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

func (m Message) Validate() error {
	switch m.Role {
	case RoleUser, RoleAssistant, RoleSystem, RoleTool:
	default:
		return errors.New("message role is invalid")
	}
	if strings.TrimSpace(m.Content) == "" {
		return errors.New("message content is required")
	}
	return nil
}

type Summary struct {
	Content           string    `json:"content"`
	Version           int64     `json:"version"`
	UpdatedAt         time.Time `json:"updated_at"`
	Goal              string    `json:"goal"`
	ConfirmedFacts    []string  `json:"confirmed_facts"`
	OpenQuestions     []string  `json:"open_questions"`
	AttemptedActions  []string  `json:"attempted_actions"`
	ImportantEntities []string  `json:"important_entities"`
}

type ContextSnapshot struct {
	Summary        Summary   `json:"summary"`
	RecentMessages []Message `json:"recent_messages"`
}

type Store interface {
	AppendMessage(ctx context.Context, sessionID string, message Message) error
	GetRecentMessages(ctx context.Context, sessionID string, limit int) ([]Message, error)
	GetSummary(ctx context.Context, sessionID string) (Summary, error)
	UpdateSummary(ctx context.Context, sessionID string, summary Summary, expectedVersion int64) error
	LoadContext(ctx context.Context, sessionID string) (ContextSnapshot, error)
	ClearHistory(ctx context.Context, sessionID string) error
}

type Summarizer interface {
	Summarize(ctx context.Context, current Summary, messages []Message) (Summary, error)
}

func EmptySummary() Summary {
	return Summary{
		ConfirmedFacts:    []string{},
		OpenQuestions:     []string{},
		AttemptedActions:  []string{},
		ImportantEntities: []string{},
	}
}
