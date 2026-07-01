package summary

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
)

type modelSummary struct {
	Content           string   `json:"content"`
	Goal              string   `json:"goal"`
	ConfirmedFacts    []string `json:"confirmed_facts"`
	OpenQuestions     []string `json:"open_questions"`
	AttemptedActions  []string `json:"attempted_actions"`
	ImportantEntities []string `json:"important_entities"`
}

func parseSummary(
	value string,
	current session.Summary,
) (session.Summary, error) {
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()

	var parsed modelSummary
	if err := decoder.Decode(&parsed); err != nil {
		return session.Summary{}, fmt.Errorf("decode summary JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return session.Summary{}, errors.New("summary output must contain one JSON object")
	}
	if strings.TrimSpace(parsed.Content) == "" {
		return session.Summary{}, errors.New("summary content is required")
	}

	return session.Summary{
		Content:           strings.TrimSpace(parsed.Content),
		Version:           current.Version,
		UpdatedAt:         time.Now().UTC(),
		Goal:              strings.TrimSpace(parsed.Goal),
		ConfirmedFacts:    normalizeItems(parsed.ConfirmedFacts),
		OpenQuestions:     normalizeItems(parsed.OpenQuestions),
		AttemptedActions:  normalizeItems(parsed.AttemptedActions),
		ImportantEntities: normalizeItems(parsed.ImportantEntities),
	}, nil
}

func normalizeItems(values []string) []string {
	if values == nil {
		return []string{}
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = appendUnique(result, value)
		}
	}
	return keepLast(result, maxStructuredItems)
}
