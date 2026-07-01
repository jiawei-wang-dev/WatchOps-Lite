package knowledge

import "context"

type Store interface {
	EnsureIndex(context.Context) error
	IndexChunks(context.Context, []Chunk) error
	Search(context.Context, SearchQuery) ([]SearchResult, error)
	GetDocument(context.Context, string) (DocumentInfo, error)
}

type VectorStore interface {
	SearchVector(context.Context, VectorSearchQuery) ([]SearchResult, error)
}

type UnavailableStore struct{}

func (UnavailableStore) EnsureIndex(context.Context) error {
	return ErrUnavailable
}

func (UnavailableStore) IndexChunks(context.Context, []Chunk) error {
	return ErrUnavailable
}

func (UnavailableStore) Search(context.Context, SearchQuery) ([]SearchResult, error) {
	return nil, ErrUnavailable
}

func (UnavailableStore) GetDocument(context.Context, string) (DocumentInfo, error) {
	return DocumentInfo{}, ErrUnavailable
}
