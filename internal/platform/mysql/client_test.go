package mysql

import (
	"testing"
	"time"
)

func TestNewConfiguresDatabasePoolWithoutConnecting(t *testing.T) {
	client, err := New(Config{
		DSN:             "watchops:watchops@tcp(localhost:3306)/watchops_lite",
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: 30 * time.Minute,
		RequestTimeout:  3 * time.Second,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()

	stats := client.DB().Stats()
	if stats.MaxOpenConnections != 10 {
		t.Fatalf("MaxOpenConnections = %d, want 10", stats.MaxOpenConnections)
	}
}
