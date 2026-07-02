package longterm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

const (
	defaultSearchLimit = 3
	maxSearchLimit     = 20
	maxTitleLength     = 200
	maxSummaryLength   = 1000
)

type Service struct {
	store        Store
	defaultLimit int
	now          func() time.Time
	newID        func() (string, error)
}

func NewService(store Store, defaultLimit int) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: store is required", ErrInvalidArgument)
	}
	if defaultLimit == 0 {
		defaultLimit = defaultSearchLimit
	}
	if defaultLimit < 1 || defaultLimit > maxSearchLimit {
		return nil, fmt.Errorf(
			"%w: default limit must be between 1 and %d",
			ErrInvalidArgument,
			maxSearchLimit,
		)
	}
	return &Service{
		store:        store,
		defaultLimit: defaultLimit,
		now:          func() time.Time { return time.Now().UTC() },
		newID:        generateID,
	}, nil
}

func (s *Service) Save(ctx context.Context, memory Memory) error {
	ctx, span := observability.StartSpan(
		ctx,
		"longterm_memory.save",
		attribute.String("source_type", memory.SourceType),
		attribute.String("service", memory.Service),
		attribute.Int("evidence_count", len(memory.EvidenceIDs)),
	)
	defer span.End()

	memory = normalizeMemory(memory)
	if err := validateMemory(memory); err != nil {
		observability.MarkError(span, "long-term memory validation failed")
		return err
	}
	if memory.ID == "" {
		id, err := s.newID()
		if err != nil {
			observability.MarkError(span, "long-term memory ID generation failed")
			return fmt.Errorf("generate long-term memory ID: %w", err)
		}
		memory.ID = id
	}
	now := s.now()
	if memory.CreatedAt.IsZero() {
		memory.CreatedAt = now
	}
	memory.UpdatedAt = now
	if err := s.store.Save(ctx, memory); err != nil {
		observability.MarkError(span, "long-term memory persistence failed")
		return err
	}
	span.SetAttributes(attribute.String("memory_id", memory.ID))
	return nil
}

func (s *Service) Search(ctx context.Context, query SearchQuery) ([]Memory, error) {
	query.Query = strings.TrimSpace(query.Query)
	query.Service = strings.TrimSpace(query.Service)
	query.Tags = normalizeStrings(query.Tags)
	if query.Limit == 0 {
		query.Limit = s.defaultLimit
	}
	if query.Limit < 1 || query.Limit > maxSearchLimit {
		return nil, fmt.Errorf(
			"%w: search limit must be between 1 and %d",
			ErrInvalidArgument,
			maxSearchLimit,
		)
	}

	ctx, span := observability.StartSpan(
		ctx,
		"longterm_memory.search",
		attribute.Int("query_length", len(query.Query)),
		attribute.String("service", query.Service),
		attribute.Int("tag_count", len(query.Tags)),
		attribute.Int("limit", query.Limit),
	)
	defer span.End()

	memories, err := s.store.Search(ctx, query)
	if err != nil {
		observability.MarkError(span, "long-term memory search failed")
		return nil, err
	}
	if memories == nil {
		memories = []Memory{}
	}
	if len(memories) > query.Limit {
		memories = memories[:query.Limit]
	}
	span.SetAttributes(attribute.Int("result_count", len(memories)))
	return memories, nil
}

func (s *Service) Get(ctx context.Context, id string) (Memory, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Memory{}, fmt.Errorf("%w: memory ID is required", ErrInvalidArgument)
	}
	return s.store.Get(ctx, id)
}

func normalizeMemory(memory Memory) Memory {
	memory.ID = strings.TrimSpace(memory.ID)
	memory.SourceType = strings.ToLower(strings.TrimSpace(memory.SourceType))
	memory.SourceID = strings.TrimSpace(memory.SourceID)
	memory.Service = strings.TrimSpace(memory.Service)
	memory.Title = truncate(strings.TrimSpace(memory.Title), maxTitleLength)
	memory.Summary = truncate(strings.TrimSpace(memory.Summary), maxSummaryLength)
	memory.EvidenceIDs = normalizeStrings(memory.EvidenceIDs)
	memory.Tags = normalizeStrings(memory.Tags)
	if memory.Metadata == nil {
		memory.Metadata = map[string]any{}
	}
	return memory
}

func validateMemory(memory Memory) error {
	if !ValidSourceType(memory.SourceType) {
		return fmt.Errorf("%w: source_type is invalid", ErrInvalidArgument)
	}
	if memory.SourceID == "" || memory.Title == "" || memory.Summary == "" {
		return fmt.Errorf(
			"%w: source_id, title, and summary are required",
			ErrInvalidArgument,
		)
	}
	if memory.SourceType != SourceManual && len(memory.EvidenceIDs) == 0 {
		return fmt.Errorf(
			"%w: confirmed non-manual memory requires evidence IDs",
			ErrInvalidArgument,
		)
	}
	return nil
}

func normalizeStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func generateID() (string, error) {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "mem_" + hex.EncodeToString(value[:]), nil
}

var _ Store = (*Service)(nil)
