package longterm

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestMySQLStoreSaveSearchAndGet(t *testing.T) {
	now := time.Date(2026, 7, 2, 1, 2, 3, 0, time.UTC)
	state := &memoryDriverState{
		rows: [][]driver.Value{{
			"mem-1",
			SourceFeedbackUp,
			"fb-1",
			"checkout",
			"Confirmed checkout timeout",
			"Payment latency increased before checkout timeouts.",
			[]byte(`["metric-1","log-1"]`),
			[]byte(`["timeout"]`),
			[]byte(`{"confirmed_by":"positive_feedback"}`),
			now,
			now,
		}},
	}
	db := sql.OpenDB(&memoryTestConnector{state: state})
	t.Cleanup(func() { _ = db.Close() })
	store, err := NewMySQLStore(db)
	if err != nil {
		t.Fatalf("NewMySQLStore() error = %v", err)
	}
	memory := Memory{
		ID:          "mem-1",
		SourceType:  SourceFeedbackUp,
		SourceID:    "fb-1",
		Service:     "checkout",
		Title:       "Confirmed checkout timeout",
		Summary:     "Payment latency increased before checkout timeouts.",
		EvidenceIDs: []string{"metric-1", "log-1"},
		Tags:        []string{"timeout"},
		Metadata:    map[string]any{"confirmed_by": "positive_feedback"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := store.Save(context.Background(), memory); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	results, err := store.Search(context.Background(), SearchQuery{
		Query: "timeout",
		Tags:  []string{"timeout"},
		Limit: 3,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	found, err := store.Get(context.Background(), "mem-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if state.execCount != 1 ||
		len(results) != 1 ||
		results[0].ID != "mem-1" ||
		found.Service != "checkout" ||
		len(found.EvidenceIDs) != 2 {
		t.Fatalf(
			"exec=%d results=%#v found=%#v",
			state.execCount,
			results,
			found,
		)
	}
}

func TestBuildSearchQueryIsBoundedAndEscapesKeywords(t *testing.T) {
	statement, arguments := buildSearchQuery(SearchQuery{
		Query:   "timeout_%",
		Service: "checkout",
		Tags:    []string{"sev2"},
		Limit:   3,
	})
	if !strings.Contains(statement, "ORDER BY updated_at DESC LIMIT ?") ||
		!strings.Contains(statement, "service = ?") ||
		!strings.Contains(statement, "tags LIKE ?") {
		t.Fatalf("statement = %q", statement)
	}
	if got := arguments[len(arguments)-1]; got != 3 {
		t.Fatalf("limit argument = %#v, want 3", got)
	}
	if keyword, ok := arguments[1].(string); !ok ||
		!strings.Contains(keyword, `\_`) ||
		!strings.Contains(keyword, `\%`) {
		t.Fatalf("keyword argument = %#v, want escaped LIKE pattern", arguments[1])
	}
}

type memoryDriverState struct {
	execCount int
	rows      [][]driver.Value
}

type memoryTestConnector struct {
	state *memoryDriverState
}

func (c *memoryTestConnector) Connect(context.Context) (driver.Conn, error) {
	return &memoryTestConn{state: c.state}, nil
}

func (c *memoryTestConnector) Driver() driver.Driver {
	return memoryTestDriver{state: c.state}
}

type memoryTestDriver struct {
	state *memoryDriverState
}

func (d memoryTestDriver) Open(string) (driver.Conn, error) {
	return &memoryTestConn{state: d.state}, nil
}

type memoryTestConn struct {
	state *memoryDriverState
}

func (c *memoryTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare is not supported")
}

func (c *memoryTestConn) Close() error {
	return nil
}

func (c *memoryTestConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not supported")
}

func (c *memoryTestConn) ExecContext(
	context.Context,
	string,
	[]driver.NamedValue,
) (driver.Result, error) {
	c.state.execCount++
	return driver.RowsAffected(1), nil
}

func (c *memoryTestConn) QueryContext(
	context.Context,
	string,
	[]driver.NamedValue,
) (driver.Rows, error) {
	rows := make([][]driver.Value, len(c.state.rows))
	for index, row := range c.state.rows {
		rows[index] = append([]driver.Value(nil), row...)
	}
	return &memoryTestRows{rows: rows}, nil
}

type memoryTestRows struct {
	rows  [][]driver.Value
	index int
}

func (r *memoryTestRows) Columns() []string {
	return []string{
		"id",
		"source_type",
		"source_id",
		"service",
		"title",
		"summary",
		"evidence_ids",
		"tags",
		"metadata",
		"created_at",
		"updated_at",
	}
}

func (r *memoryTestRows) Close() error {
	return nil
}

func (r *memoryTestRows) Next(destination []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(destination, r.rows[r.index])
	r.index++
	return nil
}

var _ driver.ExecerContext = (*memoryTestConn)(nil)
var _ driver.QueryerContext = (*memoryTestConn)(nil)
