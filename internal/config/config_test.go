package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadAppliesFileThenEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	content := []byte(`{
		"server": {"address": ":9000", "read_timeout": "20s"},
		"log": {"level": "warn"}
	}`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("WATCHOPS_SERVER_ADDRESS", ":9100")
	t.Setenv("WATCHOPS_LOG_LEVEL", "debug")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.Address != ":9100" {
		t.Fatalf("Server.Address = %q, want %q", cfg.Server.Address, ":9100")
	}
	if cfg.Server.ReadTimeout.Value() != 20*time.Second {
		t.Fatalf("Server.ReadTimeout = %s, want 20s", cfg.Server.ReadTimeout.Value())
	}
	if cfg.Log.Level != "debug" {
		t.Fatalf("Log.Level = %q, want debug", cfg.Log.Level)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"unknown": true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want an unknown-field error")
	}
}

func TestLoadAppliesRedisAndSessionEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("WATCHOPS_REDIS_ADDRESS", "redis.internal:6380")
	t.Setenv("WATCHOPS_REDIS_DB", "2")
	t.Setenv("WATCHOPS_REDIS_DIAL_TIMEOUT", "750ms")
	t.Setenv("WATCHOPS_SESSION_RECENT_WINDOW_SIZE", "20")
	t.Setenv("WATCHOPS_SESSION_SUMMARY_THRESHOLD", "16")
	t.Setenv("WATCHOPS_SESSION_TTL", "48h")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Redis.Address != "redis.internal:6380" ||
		cfg.Redis.DB != 2 ||
		cfg.Redis.DialTimeout.Value() != 750*time.Millisecond {
		t.Fatalf("Redis config = %#v, want environment overrides", cfg.Redis)
	}
	if cfg.Session.RecentWindowSize != 20 ||
		cfg.Session.SummaryThreshold != 16 ||
		cfg.Session.TTL.Value() != 48*time.Hour {
		t.Fatalf("Session config = %#v, want environment overrides", cfg.Session)
	}
}
