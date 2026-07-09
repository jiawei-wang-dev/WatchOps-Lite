package longterm

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxSearchTokens = 8

type MySQLStore struct {
	db *sql.DB
}

func NewMySQLStore(db *sql.DB) (*MySQLStore, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: database is required", ErrInvalidArgument)
	}
	return &MySQLStore{db: db}, nil
}

func (s *MySQLStore) Save(ctx context.Context, memory Memory) error {
	evidenceIDs, err := encodeJSON(memory.EvidenceIDs)
	if err != nil {
		return err
	}
	tags, err := encodeJSON(memory.Tags)
	if err != nil {
		return err
	}
	metadata, err := encodeJSON(memory.Metadata)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO long_term_memories (
			id, source_type, source_id, service, title, summary,
			evidence_ids, tags, metadata, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			service = VALUES(service),
			title = VALUES(title),
			summary = VALUES(summary),
			evidence_ids = VALUES(evidence_ids),
			tags = VALUES(tags),
			metadata = VALUES(metadata),
			updated_at = VALUES(updated_at)`,
		memory.ID,
		memory.SourceType,
		memory.SourceID,
		memory.Service,
		memory.Title,
		memory.Summary,
		evidenceIDs,
		tags,
		metadata,
		memory.CreatedAt,
		memory.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("%w: save memory: %v", ErrUnavailable, err)
	}
	return nil
}

func (s *MySQLStore) Search(
	ctx context.Context,
	query SearchQuery,
) ([]Memory, error) {
	statement, arguments := buildSearchQuery(query)
	rows, err := s.db.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("%w: search memories: %v", ErrUnavailable, err)
	}
	defer rows.Close()

	memories := make([]Memory, 0, query.Limit)
	for rows.Next() {
		memory, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		memories = append(memories, memory)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: read memory search results: %v", ErrUnavailable, err)
	}
	return memories, nil
}

func (s *MySQLStore) Get(ctx context.Context, id string) (Memory, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, source_type, source_id, service, title, summary,
			evidence_ids, tags, metadata, created_at, updated_at
		FROM long_term_memories
		WHERE id = ?`,
		id,
	)
	memory, err := scanMemory(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Memory{}, ErrNotFound
	}
	return memory, err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanMemory(row scanner) (Memory, error) {
	var memory Memory
	var evidenceIDs, tags, metadata []byte
	if err := row.Scan(
		&memory.ID,
		&memory.SourceType,
		&memory.SourceID,
		&memory.Service,
		&memory.Title,
		&memory.Summary,
		&evidenceIDs,
		&tags,
		&metadata,
		&memory.CreatedAt,
		&memory.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Memory{}, sql.ErrNoRows
		}
		return Memory{}, fmt.Errorf("%w: scan memory: %v", ErrUnavailable, err)
	}
	if err := decodeJSON(evidenceIDs, &memory.EvidenceIDs); err != nil {
		return Memory{}, err
	}
	if err := decodeJSON(tags, &memory.Tags); err != nil {
		return Memory{}, err
	}
	if err := decodeJSON(metadata, &memory.Metadata); err != nil {
		return Memory{}, err
	}
	return memory, nil
}

func buildSearchQuery(query SearchQuery) (string, []any) {
	var builder strings.Builder
	builder.WriteString(`
		SELECT id, source_type, source_id, service, title, summary,
			evidence_ids, tags, metadata, created_at, updated_at
		FROM long_term_memories
		WHERE 1 = 1`)
	arguments := []any{}
	if query.Service != "" {
		builder.WriteString(" AND service = ?")
		arguments = append(arguments, query.Service)
	}
	tokens := searchTokens(query.Query)
	if len(tokens) > 0 {
		builder.WriteString(" AND (")
		first := true
		for _, token := range tokens {
			if !first {
				builder.WriteString(" OR ")
			}
			first = false
			term := "%" + escapeLike(token) + "%"
			builder.WriteString(
				`title LIKE ? ESCAPE '\\' OR summary LIKE ? ESCAPE '\\'` +
					` OR service LIKE ? ESCAPE '\\' OR tags LIKE ? ESCAPE '\\'`,
			)
			arguments = append(arguments, term, term, term, term)
		}
		builder.WriteString(")")
	}
	for _, tag := range query.Tags {
		builder.WriteString(` AND tags LIKE ? ESCAPE '\\'`)
		arguments = append(arguments, "%\""+escapeLike(tag)+"\"%")
	}
	builder.WriteString(" ORDER BY updated_at DESC LIMIT ?")
	arguments = append(arguments, query.Limit)
	return builder.String(), arguments
}

func searchTokens(query string) []string {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return []string{}
	}
	raw := strings.FieldsFunc(query, func(r rune) bool {
		return unicode.IsSpace(r) ||
			unicode.IsPunct(r) ||
			unicode.IsSymbol(r) ||
			strings.ContainsRune("，。！？；：、（）【】《》“”‘’", r)
	})
	result := make([]string, 0, maxSearchTokens)
	seen := map[string]struct{}{}
	for _, token := range raw {
		token = strings.TrimSpace(token)
		if utf8.RuneCountInString(token) < 2 {
			continue
		}
		if _, exists := seen[token]; exists {
			continue
		}
		seen[token] = struct{}{}
		result = append(result, token)
		if len(result) == maxSearchTokens {
			break
		}
	}
	return result
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}

func encodeJSON(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: encode memory JSON: %v", ErrInvalidArgument, err)
	}
	return encoded, nil
}

func decodeJSON(encoded []byte, target any) error {
	if err := json.Unmarshal(encoded, target); err != nil {
		return fmt.Errorf("%w: decode memory JSON: %v", ErrUnavailable, err)
	}
	return nil
}

var _ Store = (*MySQLStore)(nil)
