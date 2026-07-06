package intent

import (
	"context"
	"time"
)

type Recognizer interface {
	Recognize(ctx context.Context, input RecognitionInput) (IntentResult, error)
}

type RecognitionInput struct {
	Message         string
	SessionID       string
	UserID          string
	Now             time.Time
	RecentMessages  []MessageView
	AvailableTools  []string
	AvailableSkills []string
	Metadata        map[string]any
}

type MessageView struct {
	Role      string
	Content   string
	CreatedAt time.Time
}
