package knowledge

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
	ctx, span := observability.StartSpan(ctx, "knowledge.ensure_index")
	defer span.End()
	if err := s.store.EnsureIndex(ctx); err != nil {
		observability.MarkError(span, "knowledge index unavailable")
		return err
	}
	return nil
}

func (s *Service) Ingest(ctx context.Context, document Document) (IngestResult, error) {
	ctx, span := observability.StartSpan(
		ctx,
		"knowledge.ingest_document",
		attribute.Int("content_length", len(document.Content)),
	)
	defer span.End()

	document.Title = strings.TrimSpace(document.Title)
	document.Source = strings.TrimSpace(document.Source)
	document.Content = strings.TrimSpace(document.Content)
	if document.Title == "" || document.Source == "" || document.Content == "" {
		observability.MarkError(span, "knowledge document validation failed")
		return IngestResult{}, fmt.Errorf("%w: title, source, and content are required", ErrInvalidArgument)
	}
	if document.ID == "" {
		id, err := s.newID()
		if err != nil {
			observability.MarkError(span, "knowledge document ID generation failed")
			return IngestResult{}, fmt.Errorf("generate document ID: %w", err)
		}
		document.ID = id
	}
	if document.CreatedAt.IsZero() {
		document.CreatedAt = s.now()
	}
	document.Metadata = cloneMetadata(document.Metadata)
	span.SetAttributes(attribute.String("document_id", document.ID))

	_, chunkSpan := observability.StartSpan(
		ctx,
		"knowledge.chunk_document",
		attribute.String("document_id", document.ID),
	)
	chunks := s.chunker.Split(document)
	chunkSpan.SetAttributes(attribute.Int("chunk_count", len(chunks)))
	chunkSpan.End()
	if len(chunks) == 0 {
		observability.MarkError(span, "knowledge chunking produced no chunks")
		return IngestResult{}, fmt.Errorf("%w: content produced no chunks", ErrInvalidArgument)
	}
	indexContext, indexSpan := observability.StartSpan(
		ctx,
		"knowledge.index_chunks",
		attribute.String("document_id", document.ID),
		attribute.Int("chunk_count", len(chunks)),
	)
	if err := s.store.EnsureIndex(indexContext); err != nil {
		observability.MarkError(indexSpan, "knowledge index unavailable")
		indexSpan.End()
		observability.MarkError(span, "knowledge ingestion failed")
		return IngestResult{}, err
	}
	if err := s.store.IndexChunks(indexContext, chunks); err != nil {
		observability.MarkError(indexSpan, "knowledge chunk indexing failed")
		indexSpan.End()
		observability.MarkError(span, "knowledge ingestion failed")
		return IngestResult{}, err
	}
	indexSpan.End()
	span.SetAttributes(attribute.Int("chunk_count", len(chunks)))
	return IngestResult{DocumentID: document.ID, ChunkCount: len(chunks)}, nil
}

func (s *Service) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	ctx, span := observability.StartSpan(
		ctx,
		"knowledge.search",
		attribute.Int("query_length", len(query.Query)),
	)
	defer span.End()

	query.Query = strings.TrimSpace(query.Query)
	if query.Query == "" {
		observability.MarkError(span, "knowledge search validation failed")
		return nil, fmt.Errorf("%w: query is required", ErrInvalidArgument)
	}
	if query.Limit == 0 {
		query.Limit = defaultSearchLimit
	}
	if query.Limit < 1 || query.Limit > maxSearchLimit {
		observability.MarkError(span, "knowledge search validation failed")
		return nil, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidArgument, maxSearchLimit)
	}
	for key, value := range query.Filters {
		if !validFilterKey(key) || strings.TrimSpace(value) == "" {
			observability.MarkError(span, "knowledge search validation failed")
			return nil, fmt.Errorf("%w: filter keys must use letters, numbers, underscores, or hyphens and values must not be empty", ErrInvalidArgument)
		}
		query.Filters[key] = strings.TrimSpace(value)
	}
	results, err := s.store.Search(ctx, query)
	if err != nil {
		observability.MarkError(span, "knowledge search failed")
		return nil, err
	}
	span.SetAttributes(attribute.Int("result_count", len(results)))
	return results, nil
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
	ctx, span := observability.StartSpan(
		ctx,
		"knowledge.get_document",
		attribute.String("document_id", documentID),
	)
	defer span.End()

	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		observability.MarkError(span, "knowledge document validation failed")
		return DocumentInfo{}, fmt.Errorf("%w: document ID is required", ErrInvalidArgument)
	}
	result, err := s.store.GetDocument(ctx, documentID)
	if err != nil {
		observability.MarkError(span, "knowledge document lookup failed")
		return DocumentInfo{}, err
	}
	span.SetAttributes(attribute.Int("chunk_count", result.ChunkCount))
	return result, nil
}

func generateDocumentID() (string, error) {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return "doc_" + hex.EncodeToString(bytes[:]), nil
}
