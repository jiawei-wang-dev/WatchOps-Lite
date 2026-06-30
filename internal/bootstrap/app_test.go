package bootstrap

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/config"
)

func TestNewStartsWithElasticsearchDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.Elasticsearch.Enabled = false
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	app, err := New(cfg, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if app.elasticsearchClient != nil {
		t.Fatal("Elasticsearch client was created while disabled")
	}
	if err := app.redisClient.Close(); err != nil {
		t.Fatalf("close Redis client: %v", err)
	}
	if app.elasticsearchClient != nil {
		_ = app.elasticsearchClient.Close(context.Background())
	}
}
