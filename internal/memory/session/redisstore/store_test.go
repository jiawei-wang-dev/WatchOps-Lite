package redisstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/memory/session"
	"github.com/redis/go-redis/v9"
)

func TestStoreAppendsReadsAndTrimsRecentMessages(t *testing.T) {
	store, client := newIntegrationStore(t, 3, time.Hour)

	for index := 1; index <= 4; index++ {
		err := store.AppendMessage(context.Background(), "ses-01", session.Message{
			Role:      session.RoleUser,
			Content:   fmt.Sprintf("message-%d", index),
			CreatedAt: time.Date(2026, 6, 30, 0, index, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("AppendMessage() error = %v", err)
		}
	}

	messages, err := store.GetRecentMessages(context.Background(), "ses-01", 10)
	if err != nil {
		t.Fatalf("GetRecentMessages() error = %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("message count = %d, want 3", len(messages))
	}
	if messages[0].Content != "message-2" || messages[2].Content != "message-4" {
		t.Fatalf("messages = %#v, want the newest three in chronological order", messages)
	}
	assertTTL(t, client, recentKey("ses-01"), time.Hour)
}

func TestStoreReturnsEmptyMissingState(t *testing.T) {
	store, _ := newIntegrationStore(t, 3, time.Hour)

	summary, err := store.GetSummary(context.Background(), "ses-missing")
	if err != nil {
		t.Fatalf("GetSummary() error = %v", err)
	}
	if summary.Version != 0 || len(summary.ConfirmedFacts) != 0 {
		t.Fatalf("summary = %#v, want empty version-zero summary", summary)
	}

	messages, err := store.GetRecentMessages(context.Background(), "ses-missing", 3)
	if err != nil {
		t.Fatalf("GetRecentMessages() error = %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("messages = %#v, want empty list", messages)
	}
}

func TestStoreUpdatesSummaryWithVersionAndTTL(t *testing.T) {
	store, client := newIntegrationStore(t, 3, 2*time.Hour)
	ctx := context.Background()

	err := store.UpdateSummary(ctx, "ses-01", session.Summary{
		Content: "checkout investigation",
		Goal:    "find the error-rate cause",
	}, 0)
	if err != nil {
		t.Fatalf("UpdateSummary() error = %v", err)
	}

	summary, err := store.GetSummary(ctx, "ses-01")
	if err != nil {
		t.Fatalf("GetSummary() error = %v", err)
	}
	if summary.Version != 1 || summary.Goal != "find the error-rate cause" {
		t.Fatalf("summary = %#v, want version 1 with goal", summary)
	}
	if summary.UpdatedAt.IsZero() {
		t.Fatal("summary updated_at must be populated")
	}
	assertTTL(t, client, summaryKey("ses-01"), 2*time.Hour)

	err = store.UpdateSummary(ctx, "ses-01", session.Summary{Content: "stale"}, 0)
	if !errors.Is(err, session.ErrVersionConflict) {
		t.Fatalf("UpdateSummary() error = %v, want ErrVersionConflict", err)
	}
}

func TestStoreClearsRecentMessagesAndSummary(t *testing.T) {
	store, client := newIntegrationStore(t, 3, time.Hour)
	ctx := context.Background()
	if err := store.AppendMessage(ctx, "ses-clear", session.Message{
		Role:    session.RoleUser,
		Content: "checkout is failing",
	}); err != nil {
		t.Fatalf("AppendMessage() error = %v", err)
	}
	if err := store.UpdateSummary(
		ctx,
		"ses-clear",
		session.Summary{Content: "checkout investigation"},
		0,
	); err != nil {
		t.Fatalf("UpdateSummary() error = %v", err)
	}

	if err := store.ClearHistory(ctx, "ses-clear"); err != nil {
		t.Fatalf("ClearHistory() error = %v", err)
	}
	exists, err := client.Exists(
		ctx,
		recentKey("ses-clear"),
		summaryKey("ses-clear"),
	).Result()
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if exists != 0 {
		t.Fatalf("session keys remaining = %d, want 0", exists)
	}
}

func newIntegrationStore(
	t *testing.T,
	window int,
	ttl time.Duration,
) (*Store, *redis.Client) {
	t.Helper()

	binary, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server is not installed; skipping optional Redis integration test")
	}

	socket := filepath.Join(t.TempDir(), "redis.sock")
	command := exec.Command(
		binary,
		"--port", "0",
		"--unixsocket", socket,
		"--unixsocketperm", "700",
		"--save", "",
		"--appendonly", "no",
		"--loglevel", "warning",
	)
	command.Stdout = os.Stderr
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Skipf("could not start optional Redis integration server: %v", err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
	})

	client := redis.NewClient(&redis.Options{Network: "unix", Addr: socket})
	t.Cleanup(func() {
		_ = client.Close()
	})

	deadline := time.Now().Add(2 * time.Second)
	for {
		err = client.Ping(context.Background()).Err()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Skipf("optional Redis integration server did not become ready: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	store, err := New(client, window, ttl)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return store, client
}

func assertTTL(t *testing.T, client *redis.Client, key string, expected time.Duration) {
	t.Helper()

	ttl, err := client.TTL(context.Background(), key).Result()
	if err != nil {
		t.Fatalf("TTL() error = %v", err)
	}
	if ttl <= expected-2*time.Second || ttl > expected {
		t.Fatalf("key TTL = %s, want approximately %s", ttl, expected)
	}
}
