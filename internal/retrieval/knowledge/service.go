package knowledge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	defaultSearchLimit = 5
	maxSearchLimit     = 20
)

type Service struct {
	store   Store
	chunker *Chunker
	now     func() time.Time
	newID   func() (string, error)
}

func NewService(store Store, chunkMaxSize int) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: store is required", ErrInvalidArgument)
	}
	chunker, err := NewChunker(chunkMaxSize)
	if err != nil {
		return nil, err
	}
	return &Service{
		store:   store,
		chunker: chunker,
		now:     func() time.Time { return time.Now().UTC() },
		newID:   generateDocumentID,
	}, nil
}

func (s *Service) EnsureIndex(ctx context.Context) error {
	return s.store.EnsureIndex(ctx)
}

func (s *Service) Ingest(ctx context.Context, document Document) (IngestResult, error) {
	document.Title = strings.TrimSpace(document.Title)
	document.Source = strings.TrimSpace(document.Source)
	document.Content = strings.TrimSpace(document.Content)
	if document.Title == "" || document.Source == "" || document.Content == "" {
		return IngestResult{}, fmt.Errorf("%w: title, source, and content are required", ErrInvalidArgument)
	}
	if document.ID == "" {
		id, err := s.newID()
		if err != nil {
			return IngestResult{}, fmt.Errorf("generate document ID: %w", err)
		}
		document.ID = id
	}
	if document.CreatedAt.IsZero() {
		document.CreatedAt = s.now()
	}
	document.Metadata = cloneMetadata(document.Metadata)

	chunks := s.chunker.Split(document)
	if len(chunks) == 0 {
		return IngestResult{}, fmt.Errorf("%w: content produced no chunks", ErrInvalidArgument)
	}
	if err := s.store.EnsureIndex(ctx); err != nil {
		return IngestResult{}, err
	}
	if err := s.store.IndexChunks(ctx, chunks); err != nil {
		return IngestResult{}, err
	}
	return IngestResult{DocumentID: document.ID, ChunkCount: len(chunks)}, nil
}

func (s *Service) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	query.Query = strings.TrimSpace(query.Query)
	if query.Query == "" {
		return nil, fmt.Errorf("%w: query is required", ErrInvalidArgument)
	}
	if query.Limit == 0 {
		query.Limit = defaultSearchLimit
	}
	if query.Limit < 1 || query.Limit > maxSearchLimit {
		return nil, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidArgument, maxSearchLimit)
	}
	for key, value := range query.Filters {
		if !validFilterKey(key) || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%w: filter keys must use letters, numbers, underscores, or hyphens and values must not be empty", ErrInvalidArgument)
		}
		query.Filters[key] = strings.TrimSpace(value)
	}
	return s.store.Search(ctx, query)
}

func validFilterKey(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '_' ||
			character == '-' {
			continue
		}
		return false
	}
	return true
}

func (s *Service) GetDocument(ctx context.Context, documentID string) (DocumentInfo, error) {
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return DocumentInfo{}, fmt.Errorf("%w: document ID is required", ErrInvalidArgument)
	}
	return s.store.GetDocument(ctx, documentID)
}

func generateDocumentID() (string, error) {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return "doc_" + hex.EncodeToString(bytes[:]), nil
}
