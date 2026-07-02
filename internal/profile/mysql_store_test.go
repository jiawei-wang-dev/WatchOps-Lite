package profile

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"testing"
	"time"
)

func TestMySQLStoreUpsertAndGet(t *testing.T) {
	now := time.Date(2026, 7, 3, 1, 2, 3, 0, time.UTC)
	state := &profileDriverState{row: []driver.Value{
		"oncall-1",
		"Alex",
		"checkout",
		[]byte(`["checkout","payment"]`),
		"Australia/Melbourne",
		[]byte(`{"notification_style":"concise"}`),
		[]byte(`{"source":"demo"}`),
		now,
		now,
	}}
	db := sql.OpenDB(profileConnector{state: state})
	t.Cleanup(func() { _ = db.Close() })
	store, err := NewMySQLStore(db)
	if err != nil {
		t.Fatalf("NewMySQLStore() error = %v", err)
	}
	value := Profile{
		UserID:         "oncall-1",
		DisplayName:    "Alex",
		DefaultService: "checkout",
		Services:       []string{"checkout", "payment"},
		Timezone:       "Australia/Melbourne",
		Preferences:    map[string]any{"notification_style": "concise"},
		Metadata:       map[string]any{"source": "demo"},
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := store.Upsert(context.Background(), value); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	found, err := store.Get(context.Background(), "oncall-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if state.execCount != 1 ||
		found.UserID != "oncall-1" ||
		found.DefaultService != "checkout" ||
		len(found.Services) != 2 ||
		found.Preferences["notification_style"] != "concise" {
		t.Fatalf("exec=%d found=%#v", state.execCount, found)
	}
}

type profileDriverState struct {
	execCount int
	row       []driver.Value
}

type profileConnector struct {
	state *profileDriverState
}

func (c profileConnector) Connect(context.Context) (driver.Conn, error) {
	return &profileConn{state: c.state}, nil
}

func (c profileConnector) Driver() driver.Driver {
	return profileDriver{state: c.state}
}

type profileDriver struct {
	state *profileDriverState
}

func (d profileDriver) Open(string) (driver.Conn, error) {
	return &profileConn{state: d.state}, nil
}

type profileConn struct {
	state *profileDriverState
}

func (c *profileConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare is not supported")
}

func (c *profileConn) Close() error {
	return nil
}

func (c *profileConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not supported")
}

func (c *profileConn) ExecContext(
	context.Context,
	string,
	[]driver.NamedValue,
) (driver.Result, error) {
	c.state.execCount++
	return driver.RowsAffected(1), nil
}

func (c *profileConn) QueryContext(
	context.Context,
	string,
	[]driver.NamedValue,
) (driver.Rows, error) {
	return &profileRows{row: append([]driver.Value(nil), c.state.row...)}, nil
}

type profileRows struct {
	row  []driver.Value
	read bool
}

func (r *profileRows) Columns() []string {
	return []string{
		"user_id",
		"display_name",
		"default_service",
		"services",
		"timezone",
		"preferences",
		"metadata",
		"created_at",
		"updated_at",
	}
}

func (r *profileRows) Close() error {
	return nil
}

func (r *profileRows) Next(destination []driver.Value) error {
	if r.read {
		return io.EOF
	}
	copy(destination, r.row)
	r.read = true
	return nil
}

var _ driver.ExecerContext = (*profileConn)(nil)
var _ driver.QueryerContext = (*profileConn)(nil)
