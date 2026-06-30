package bootstrap

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
	if app.mysqlClient != nil {
		t.Fatal("MySQL client was created while disabled")
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/feedback",
		bytes.NewBufferString(`{"request_id":"req","session_id":"ses","rating":"up"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	app.server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("feedback status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}

	if err := app.redisClient.Close(); err != nil {
		t.Fatalf("close Redis client: %v", err)
	}
	if app.elasticsearchClient != nil {
		_ = app.elasticsearchClient.Close(context.Background())
	}
}

func TestNewContinuesWhenMySQLIsUnavailable(t *testing.T) {
	cfg := config.Default()
	cfg.MySQL.Enabled = true
	cfg.MySQL.DSN = "watchops:watchops@tcp(127.0.0.1:1)/watchops_lite"
	cfg.MySQL.RequestTimeout = config.Duration(20 * time.Millisecond)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	app, err := New(cfg, logger)
	if err != nil {
		t.Fatalf("New() error = %v, want resilient startup", err)
	}
	if app.mysqlClient == nil {
		t.Fatal("MySQL client was not retained for later recovery")
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/feedback",
		bytes.NewBufferString(`{"request_id":"req","session_id":"ses","rating":"down"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	app.server.Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("feedback status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}

	if err := app.mysqlClient.Close(); err != nil {
		t.Fatalf("close MySQL client: %v", err)
	}
	if err := app.redisClient.Close(); err != nil {
		t.Fatalf("close Redis client: %v", err)
	}
}
