package knowledge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	runtimemetrics "github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability/metrics"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/embedding"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/rerank"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultSearchLimit = 5
	maxSearchLimit     = 20
)

type Service struct {
	store     Store
	chunker   *Chunker
	embedding embedding.Provider
	reranker  rerank.Reranker
	config    ServiceConfig
	now       func() time.Time
}

type ServiceConfig struct {
	ChunkMaxSize     int
	RetrievalMode    string
	BM25TopK         int
	VectorTopK       int
	FinalTopK        int
	RRFK             int
	FallbackToBM25   bool
	RerankCandidateK int
	RerankTopK       int
}

func NewService(store Store, chunkMaxSize int) (*Service, error) {
	return NewServiceWithConfig(store, nil, ServiceConfig{
		ChunkMaxSize:   chunkMaxSize,
		RetrievalMode:  "bm25",
		BM25TopK:       10,
		VectorTopK:     10,
		FinalTopK:      defaultSearchLimit,
		RRFK:           60,
		FallbackToBM25: true,
	})
}

func NewServiceWithConfig(
	store Store,
	embeddingProvider embedding.Provider,
	config ServiceConfig,
) (*Service, error) {
	return NewServiceWithReranker(store, embeddingProvider, nil, config)
}

func NewServiceWithReranker(
	store Store,
	embeddingProvider embedding.Provider,
	reranker rerank.Reranker,
	config ServiceConfig,
) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: store is required", ErrInvalidArgument)
	}
	chunker, err := NewChunker(config.ChunkMaxSize)
	if err != nil {
		return nil, err
	}
	config.RetrievalMode = strings.ToLower(strings.TrimSpace(config.RetrievalMode))
	switch config.RetrievalMode {
	case "bm25", "vector", "hybrid":
	default:
		return nil, fmt.Errorf("%w: unsupported retrieval mode", ErrInvalidArgument)
	}
	if config.BM25TopK <= 0 || config.VectorTopK <= 0 ||
		config.FinalTopK <= 0 || config.RRFK <= 0 {
		return nil, fmt.Errorf("%w: retrieval limits must be greater than zero", ErrInvalidArgument)
	}
	if config.RerankCandidateK == 0 {
		config.RerankCandidateK = max(config.BM25TopK, config.VectorTopK)
	}
	if config.RerankTopK == 0 {
		config.RerankTopK = config.FinalTopK
	}
	if config.RerankCandidateK < config.RerankTopK {
		return nil, fmt.Errorf("%w: rerank candidate limit must not be less than rerank top-k", ErrInvalidArgument)
	}
	return &Service{
		store:     store,
		chunker:   chunker,
		embedding: embeddingProvider,
		reranker:  reranker,
		config:    config,
		now:       func() time.Time { return time.Now().UTC() },
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
	contentHash := ContentHash(document.Content)
	document.Metadata = cloneMetadata(document.Metadata)
	document.Metadata["content_hash"] = contentHash
	document.Metadata["source"] = document.Source
	document.Metadata["title"] = document.Title
	if err := s.store.EnsureIndex(ctx); err != nil {
		observability.MarkError(span, "knowledge index unavailable")
		return IngestResult{}, err
	}
	if hashStore, ok := s.store.(ContentHashStore); ok {
		duplicate, err := hashStore.FindByContentHash(ctx, contentHash)
		switch {
		case err == nil && duplicate.DocumentID != "":
			span.SetAttributes(
				attribute.String("document_id", duplicate.DocumentID),
				attribute.String("ingest.status", "skipped_duplicate"),
				attribute.Bool("duplicate", true),
			)
			return IngestResult{
				DocumentID: duplicate.DocumentID,
				ChunkCount: duplicate.ChunkCount,
				Status:     "skipped_duplicate",
			}, nil
		case err != nil && !errors.Is(err, ErrNotFound):
			observability.MarkError(span, "knowledge duplicate lookup failed")
			return IngestResult{}, err
		}
	}
	if document.ID == "" {
		document.ID = documentIDFromContentHash(contentHash)
	}
	if document.CreatedAt.IsZero() {
		document.CreatedAt = s.now()
	}
	span.SetAttributes(
		attribute.String("document_id", document.ID),
	)

	_, chunkSpan := observability.StartSpan(
		ctx,
		"knowledge.chunk_document",
		attribute.String("document_id", document.ID),
	)
	chunks := s.chunker.Split(document)
	for index := range chunks {
		chunks[index].ContentHash = contentHash
	}
	chunkSpan.SetAttributes(attribute.Int("chunk_count", len(chunks)))
	chunkSpan.End()
	if len(chunks) == 0 {
		observability.MarkError(span, "knowledge chunking produced no chunks")
		return IngestResult{}, fmt.Errorf("%w: content produced no chunks", ErrInvalidArgument)
	}
	if s.embedding != nil {
		embeddingContext, embeddingSpan := observability.StartSpan(
			ctx,
			"knowledge.embedding",
			attribute.String("embedding.operation", "index"),
			attribute.Int("chunk_count", len(chunks)),
		)
		inputs := make([]string, 0, len(chunks))
		for _, chunk := range chunks {
			inputs = append(inputs, chunk.Title+"\n"+chunk.Content)
		}
		vectors, embeddingErr := s.embedding.Embed(embeddingContext, inputs)
		if embeddingErr == nil && len(vectors) == len(chunks) {
			for index := range chunks {
				chunks[index].Embedding = vectors[index]
			}
		} else if !s.config.FallbackToBM25 {
			observability.MarkError(embeddingSpan, "knowledge embedding failed")
			embeddingSpan.End()
			return IngestResult{}, fmt.Errorf("%w: embedding chunks", ErrUnavailable)
		}
		embeddingSpan.SetAttributes(
			attribute.Bool("fallback_used", embeddingErr != nil || len(vectors) != len(chunks)),
		)
		embeddingSpan.End()
	}
	indexContext, indexSpan := observability.StartSpan(
		ctx,
		"knowledge.index_chunks",
		attribute.String("document_id", document.ID),
		attribute.Int("chunk_count", len(chunks)),
	)
	if err := s.store.IndexChunks(indexContext, chunks); err != nil {
		observability.MarkError(indexSpan, "knowledge chunk indexing failed")
		indexSpan.End()
		observability.MarkError(span, "knowledge ingestion failed")
		return IngestResult{}, err
	}
	indexSpan.End()
	span.SetAttributes(attribute.Int("chunk_count", len(chunks)))
	return IngestResult{
		DocumentID: document.ID,
		ChunkCount: len(chunks),
		Status:     "seeded",
	}, nil
}

func (s *Service) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	started := time.Now()
	defer func() {
		runtimemetrics.ObserveRAGSearch(s.config.RetrievalMode, time.Since(started))
	}()
	ctx, span := observability.StartSpan(
		ctx,
		"knowledge.search",
		attribute.Int("query_length", len(query.Query)),
		attribute.String("retrieval_mode", s.config.RetrievalMode),
	)
	defer span.End()

	query.Query = strings.TrimSpace(query.Query)
	if query.Query == "" {
		observability.MarkError(span, "knowledge search validation failed")
		return nil, fmt.Errorf("%w: query is required", ErrInvalidArgument)
	}
	if query.Limit == 0 {
		query.Limit = s.config.FinalTopK
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
	candidateQuery := query
	candidateQuery.Limit = min(
		maxSearchLimit,
		max(query.Limit*4, query.Limit),
	)
	if s.reranker != nil {
		candidateQuery.Limit = max(
			candidateQuery.Limit,
			s.config.RerankCandidateK,
		)
	}
	results, err := s.searchByMode(ctx, candidateQuery, span)
	if err != nil {
		observability.MarkError(span, "knowledge search failed")
		return nil, err
	}
	results = dedupeSearchResults(results)
	if s.reranker != nil && len(results) > 0 {
		results, err = s.rerankResults(ctx, query.Query, results, min(query.Limit, s.config.RerankTopK))
		if err != nil {
			observability.MarkError(span, "knowledge rerank failed")
			return nil, err
		}
	} else {
		results = trimResults(results, query.Limit)
	}
	results = trimResults(dedupeSearchResults(results), query.Limit)
	span.SetAttributes(
		attribute.Int("result_count", len(results)),
		attribute.Int("deduped_duplicate_count", dedupedResultCount(results)),
	)
	return results, nil
}

func dedupedResultCount(results []SearchResult) int {
	total := 0
	for _, result := range results {
		if count, ok := metadataFloat(
			result.Metadata,
			"deduped_duplicate_count",
		); ok {
			total += int(count)
		}
	}
	return total
}

func (s *Service) rerankResults(
	ctx context.Context,
	query string,
	results []SearchResult,
	topK int,
) ([]SearchResult, error) {
	ctx, span := observability.StartSpan(
		ctx,
		"retrieval.rerank",
		attribute.Int("candidate_count", len(results)),
		attribute.Int("top_k", topK),
	)
	defer span.End()

	candidates := make([]rerank.Candidate, 0, len(results))
	originals := make(map[string]SearchResult, len(results))
	for index, result := range results {
		candidate := rerankCandidate(result, index)
		candidates = append(candidates, candidate)
		originals[candidate.ID] = result
	}
	reranked, err := s.reranker.Rerank(ctx, query, candidates, topK)
	if err != nil {
		if ctx.Err() != nil {
			observability.MarkError(span, "knowledge rerank canceled")
			return nil, ctx.Err()
		}
		observability.MarkError(span, "knowledge rerank unavailable; retrieval order retained")
		fallback := trimResults(results, topK)
		for index := range fallback {
			fallback[index].Metadata = cloneMetadata(fallback[index].Metadata)
			fallback[index].Metadata["rerank_provider"] = "none"
			fallback[index].Metadata["rerank_fallback_reason"] = "reranker_error"
		}
		span.SetAttributes(
			attribute.Bool("fallback_used", true),
			attribute.String("fallback_reason", "reranker_error"),
			attribute.Int("result_count", len(fallback)),
		)
		return fallback, nil
	}
	if len(reranked) == 0 {
		fallback := trimResults(results, topK)
		for index := range fallback {
			fallback[index].Metadata = cloneMetadata(fallback[index].Metadata)
			fallback[index].Metadata["rerank_provider"] = "none"
			fallback[index].Metadata["rerank_fallback_reason"] = "empty_rerank_result"
		}
		span.SetAttributes(
			attribute.Bool("fallback_used", true),
			attribute.String("fallback_reason", "empty_rerank_result"),
			attribute.Int("result_count", len(fallback)),
		)
		return fallback, nil
	}

	final := make([]SearchResult, 0, len(reranked))
	fallbackUsed := false
	fallbackReason := ""
	for _, ranked := range reranked {
		original, ok := originals[ranked.Candidate.ID]
		if !ok {
			continue
		}
		retrievalScore := original.Score
		original.Score = ranked.Score
		original.Metadata = cloneMetadata(original.Metadata)
		original.Metadata["retrieval_score"] = retrievalScore
		original.Metadata["rerank_provider"] = ranked.Provider
		original.Metadata["rerank_score"] = ranked.Score
		original.Metadata["rerank_reason"] = ranked.Reason
		if ranked.FallbackReason != "" {
			original.Metadata["rerank_fallback_reason"] = ranked.FallbackReason
			fallbackUsed = true
			fallbackReason = ranked.FallbackReason
		}
		final = append(final, original)
	}
	if len(final) == 0 {
		fallback := trimResults(results, topK)
		for index := range fallback {
			fallback[index].Metadata = cloneMetadata(fallback[index].Metadata)
			fallback[index].Metadata["rerank_provider"] = "none"
			fallback[index].Metadata["rerank_fallback_reason"] = "unmatched_rerank_result"
		}
		span.SetAttributes(
			attribute.Bool("fallback_used", true),
			attribute.String("fallback_reason", "unmatched_rerank_result"),
			attribute.Int("result_count", len(fallback)),
		)
		return fallback, nil
	}
	provider, _ := final[0].Metadata["rerank_provider"].(string)
	span.SetAttributes(
		attribute.String("provider", provider),
		attribute.Bool("fallback_used", fallbackUsed),
		attribute.String("fallback_reason", fallbackReason),
		attribute.Int("result_count", len(final)),
	)
	return final, nil
}

func rerankCandidate(result SearchResult, index int) rerank.Candidate {
	id := result.ChunkID
	if id == "" {
		id = fmt.Sprintf("%s#%d", result.DocumentID, index)
	}
	sourceType := metadataString(result.Metadata, "source_type")
	if sourceType == "" {
		sourceType = "knowledge"
		if strings.Contains(strings.ToLower(result.Source+" "+result.Title), "runbook") {
			sourceType = "runbook"
		}
	}
	return rerank.Candidate{
		ID:          id,
		DocumentID:  result.DocumentID,
		ChunkID:     result.ChunkID,
		Title:       result.Title,
		Content:     result.Content,
		Service:     metadataString(result.Metadata, "service"),
		Source:      result.Source,
		SourceType:  sourceType,
		Score:       result.Score,
		BM25Score:   optionalScore(result.BM25Score),
		VectorScore: optionalScore(result.VectorScore),
		HybridScore: optionalScore(result.RRFScore),
		Metadata:    cloneMetadata(result.Metadata),
		CreatedAt:   metadataTime(result.Metadata, "created_at"),
	}
}

func metadataString(metadata map[string]any, key string) string {
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

func metadataTime(metadata map[string]any, key string) time.Time {
	switch value := metadata[key].(type) {
	case time.Time:
		return value
	case string:
		parsed, _ := time.Parse(time.RFC3339, value)
		return parsed
	default:
		return time.Time{}
	}
}

func optionalScore(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func (s *Service) searchByMode(
	ctx context.Context,
	query SearchQuery,
	span trace.Span,
) ([]SearchResult, error) {
	switch s.config.RetrievalMode {
	case "bm25":
		results, err := s.searchBM25(ctx, query)
		span.SetAttributes(attribute.Int("bm25_result_count", len(results)))
		return trimResults(results, query.Limit), err
	case "vector":
		results, err := s.searchVector(ctx, query)
		span.SetAttributes(attribute.Int("vector_result_count", len(results)))
		return trimResults(results, query.Limit), err
	case "hybrid":
		bm25Results, err := s.searchBM25(ctx, query)
		span.SetAttributes(attribute.Int("bm25_result_count", len(bm25Results)))
		if err != nil {
			return nil, err
		}
		vectorResults, vectorErr := s.searchVector(ctx, query)
		span.SetAttributes(attribute.Int("vector_result_count", len(vectorResults)))
		if vectorErr != nil {
			if !s.config.FallbackToBM25 {
				return nil, vectorErr
			}
			results := trimResults(bm25Results, query.Limit)
			for index := range results {
				results[index].Metadata = cloneMetadata(results[index].Metadata)
				results[index].Metadata["vector_fallback"] = true
				results[index].Metadata["retrieval_mode"] = "bm25"
			}
			span.SetAttributes(attribute.Bool("fallback_used", true))
			return results, nil
		}
		fusionContext, fusionSpan := observability.StartSpan(
			ctx,
			"knowledge.search.hybrid_fusion",
			attribute.Int("bm25_result_count", len(bm25Results)),
			attribute.Int("vector_result_count", len(vectorResults)),
		)
		results := fuseRRF(bm25Results, vectorResults, s.config.RRFK, query.Limit)
		fusionSpan.SetAttributes(attribute.Int("final_result_count", len(results)))
		fusionSpan.End()
		_ = fusionContext
		return results, nil
	default:
		return nil, fmt.Errorf("%w: unsupported retrieval mode", ErrInvalidArgument)
	}
}

func (s *Service) searchBM25(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	ctx, span := observability.StartSpan(
		ctx,
		"knowledge.search.bm25",
		attribute.Int("candidate_limit", s.config.BM25TopK),
	)
	defer span.End()
	candidate := query
	candidate.Limit = max(query.Limit, s.config.BM25TopK)
	results, err := s.store.Search(ctx, candidate)
	if err != nil {
		observability.MarkError(span, "BM25 search failed")
		return nil, err
	}
	for index := range results {
		score := results[index].Score
		results[index].BM25Score = &score
		results[index].RetrievalMode = "bm25"
		results[index].Metadata = cloneMetadata(results[index].Metadata)
		results[index].Metadata["retrieval_mode"] = "bm25"
	}
	span.SetAttributes(attribute.Int("result_count", len(results)))
	return results, nil
}

func (s *Service) searchVector(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	if s.embedding == nil {
		return nil, fmt.Errorf("%w: embedding provider is unavailable", ErrUnavailable)
	}
	vectorStore, ok := s.store.(VectorStore)
	if !ok {
		return nil, fmt.Errorf("%w: vector store is unavailable", ErrUnavailable)
	}
	embeddingContext, embeddingSpan := observability.StartSpan(
		ctx,
		"knowledge.embedding",
		attribute.String("embedding.operation", "query"),
		attribute.Int("input_count", 1),
	)
	vectors, err := s.embedding.Embed(embeddingContext, []string{query.Query})
	if err != nil || len(vectors) != 1 {
		observability.MarkError(embeddingSpan, "query embedding failed")
		embeddingSpan.End()
		return nil, fmt.Errorf("%w: query embedding failed", ErrUnavailable)
	}
	embeddingSpan.End()

	searchContext, searchSpan := observability.StartSpan(
		ctx,
		"knowledge.search.vector",
		attribute.Int("candidate_limit", s.config.VectorTopK),
	)
	defer searchSpan.End()
	results, err := vectorStore.SearchVector(searchContext, VectorSearchQuery{
		Vector:  vectors[0],
		Limit:   max(query.Limit, s.config.VectorTopK),
		Filters: query.Filters,
	})
	if err != nil {
		observability.MarkError(searchSpan, "vector search failed")
		return nil, err
	}
	for index := range results {
		score := results[index].Score
		results[index].VectorScore = &score
		results[index].RetrievalMode = "vector"
		results[index].Metadata = cloneMetadata(results[index].Metadata)
		results[index].Metadata["retrieval_mode"] = "vector"
	}
	searchSpan.SetAttributes(attribute.Int("result_count", len(results)))
	return results, nil
}

func trimResults(results []SearchResult, limit int) []SearchResult {
	if len(results) <= limit {
		return results
	}
	return results[:limit]
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

func documentIDFromContentHash(contentHash string) string {
	const idHashLength = 24
	if len(contentHash) > idHashLength {
		contentHash = contentHash[:idHashLength]
	}
	return "doc_" + contentHash
}
