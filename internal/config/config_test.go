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

func TestRuntimeMetricsDefaultsAndEnvironmentOverride(t *testing.T) {
	cfg := Default()
	if !cfg.RuntimeMetrics.Enabled {
		t.Fatal("expected runtime metrics to be enabled by default")
	}

	t.Setenv("WATCHOPS_RUNTIME_METRICS_ENABLED", "false")
	if err := applyEnvironment(&cfg); err != nil {
		t.Fatalf("apply environment: %v", err)
	}
	if cfg.RuntimeMetrics.Enabled {
		t.Fatal("expected environment to disable runtime metrics")
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

func TestLocalDemoExampleIsValid(t *testing.T) {
	path := filepath.Join("..", "..", "configs", "config.example.json")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(%q) error = %v", path, err)
	}
	if !cfg.Elasticsearch.Enabled || !cfg.MySQL.Enabled || !cfg.Telemetry.Enabled {
		t.Fatalf("local demo dependencies are not enabled: %#v", cfg)
	}
	if cfg.LLM.Enabled || cfg.Agent.Mode != "deterministic" {
		t.Fatalf("local demo must not require an LLM: Agent=%#v LLM=%#v", cfg.Agent, cfg.LLM)
	}
	if cfg.Logs.Backend != "elasticsearch" || !cfg.Logs.FallbackToMock {
		t.Fatalf("local demo logs config = %#v, want Elasticsearch with mock fallback", cfg.Logs)
	}
	if cfg.Metrics.Backend != "prometheus" || !cfg.Metrics.FallbackToMock {
		t.Fatalf("local demo metrics config = %#v, want Prometheus with mock fallback", cfg.Metrics)
	}
	if cfg.Traces.Backend != "jaeger" || !cfg.Traces.FallbackToMock {
		t.Fatalf("local demo traces config = %#v, want Jaeger with mock fallback", cfg.Traces)
	}
}

func TestDefaultLogsConfigurationUsesMock(t *testing.T) {
	cfg := Default()

	if cfg.Logs.Backend != "mock" ||
		cfg.Logs.Index != "watchops_logs" ||
		!cfg.Logs.FallbackToMock ||
		cfg.Logs.DefaultLimit != 20 {
		t.Fatalf("Logs config = %#v", cfg.Logs)
	}
}

func TestLoadAppliesLogsEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WATCHOPS_LOGS_BACKEND", "elasticsearch")
	t.Setenv("WATCHOPS_LOGS_INDEX", "logs_test")
	t.Setenv("WATCHOPS_LOGS_FALLBACK_TO_MOCK", "false")
	t.Setenv("WATCHOPS_LOGS_DEFAULT_LIMIT", "35")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Logs.Backend != "elasticsearch" ||
		cfg.Logs.Index != "logs_test" ||
		cfg.Logs.FallbackToMock ||
		cfg.Logs.DefaultLimit != 35 {
		t.Fatalf("Logs config = %#v", cfg.Logs)
	}
}

func TestDefaultMetricsConfigurationUsesMock(t *testing.T) {
	cfg := Default()

	if cfg.Metrics.Backend != "mock" ||
		cfg.Metrics.BaseURL != "http://localhost:9090" ||
		!cfg.Metrics.FallbackToMock ||
		cfg.Metrics.DefaultStep.Value() != 30*time.Second ||
		cfg.Metrics.RequestTimeout.Value() != 3*time.Second ||
		cfg.Metrics.Queries["checkout_error_rate"] != "watchops_checkout_error_rate" {
		t.Fatalf("Metrics config = %#v", cfg.Metrics)
	}
}

func TestLoadAppliesMetricsEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WATCHOPS_METRICS_BACKEND", "prometheus")
	t.Setenv("WATCHOPS_METRICS_BASE_URL", "http://prometheus:9090")
	t.Setenv("WATCHOPS_METRICS_FALLBACK_TO_MOCK", "false")
	t.Setenv("WATCHOPS_METRICS_DEFAULT_STEP", "15s")
	t.Setenv("WATCHOPS_METRICS_REQUEST_TIMEOUT", "750ms")
	t.Setenv("WATCHOPS_METRICS_QUERY_CHECKOUT_ERROR_RATE", "custom_checkout_error_rate")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Metrics.Backend != "prometheus" ||
		cfg.Metrics.BaseURL != "http://prometheus:9090" ||
		cfg.Metrics.FallbackToMock ||
		cfg.Metrics.DefaultStep.Value() != 15*time.Second ||
		cfg.Metrics.RequestTimeout.Value() != 750*time.Millisecond ||
		cfg.Metrics.Queries["checkout_error_rate"] != "custom_checkout_error_rate" {
		t.Fatalf("Metrics config = %#v", cfg.Metrics)
	}
}

func TestDefaultTracesConfigurationUsesMock(t *testing.T) {
	cfg := Default()

	if cfg.Traces.Backend != "mock" ||
		cfg.Traces.BaseURL != "http://localhost:16686" ||
		!cfg.Traces.FallbackToMock ||
		cfg.Traces.DefaultLimit != 10 ||
		cfg.Traces.RequestTimeout.Value() != 3*time.Second ||
		cfg.Traces.DefaultService != "watchops-lite" {
		t.Fatalf("Traces config = %#v", cfg.Traces)
	}
}

func TestLoadAppliesTracesEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WATCHOPS_TRACES_BACKEND", "jaeger")
	t.Setenv("WATCHOPS_TRACES_BASE_URL", "http://jaeger:16686")
	t.Setenv("WATCHOPS_TRACES_FALLBACK_TO_MOCK", "false")
	t.Setenv("WATCHOPS_TRACES_DEFAULT_LIMIT", "25")
	t.Setenv("WATCHOPS_TRACES_REQUEST_TIMEOUT", "750ms")
	t.Setenv("WATCHOPS_TRACES_DEFAULT_SERVICE", "watchops-api")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Traces.Backend != "jaeger" ||
		cfg.Traces.BaseURL != "http://jaeger:16686" ||
		cfg.Traces.FallbackToMock ||
		cfg.Traces.DefaultLimit != 25 ||
		cfg.Traces.RequestTimeout.Value() != 750*time.Millisecond ||
		cfg.Traces.DefaultService != "watchops-api" {
		t.Fatalf("Traces config = %#v", cfg.Traces)
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

func TestDefaultSummaryConfigurationUsesDeterministicMode(t *testing.T) {
	cfg := Default()

	if cfg.Summary.Mode != "deterministic" ||
		cfg.Summary.PromptVersion != "session_summary_v1" ||
		cfg.Summary.Timeout.Value() != 10*time.Second ||
		!cfg.Summary.FallbackToDeterministic {
		t.Fatalf("Summary config = %#v", cfg.Summary)
	}
}

func TestLoadAppliesSummaryEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WATCHOPS_SUMMARY_MODE", "llm")
	t.Setenv("WATCHOPS_SUMMARY_PROMPT_VERSION", "session_summary_v2")
	t.Setenv("WATCHOPS_SUMMARY_TIMEOUT", "750ms")
	t.Setenv("WATCHOPS_SUMMARY_FALLBACK_TO_DETERMINISTIC", "true")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Summary.Mode != "llm" ||
		cfg.Summary.PromptVersion != "session_summary_v2" ||
		cfg.Summary.Timeout.Value() != 750*time.Millisecond ||
		!cfg.Summary.FallbackToDeterministic {
		t.Fatalf("Summary config = %#v", cfg.Summary)
	}
}

func TestLoadAppliesElasticsearchEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("WATCHOPS_ELASTICSEARCH_ENABLED", "true")
	t.Setenv("WATCHOPS_ELASTICSEARCH_ADDRESSES", "http://es-1:9200, http://es-2:9200")
	t.Setenv("WATCHOPS_ELASTICSEARCH_KNOWLEDGE_INDEX", "knowledge_test")
	t.Setenv("WATCHOPS_ELASTICSEARCH_REQUEST_TIMEOUT", "750ms")
	t.Setenv("WATCHOPS_KNOWLEDGE_CHUNK_MAX_SIZE", "640")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Elasticsearch.Enabled ||
		len(cfg.Elasticsearch.Addresses) != 2 ||
		cfg.Elasticsearch.KnowledgeIndex != "knowledge_test" ||
		cfg.Elasticsearch.RequestTimeout.Value() != 750*time.Millisecond {
		t.Fatalf("Elasticsearch config = %#v", cfg.Elasticsearch)
	}
	if cfg.Knowledge.ChunkMaxSize != 640 {
		t.Fatalf("Knowledge config = %#v", cfg.Knowledge)
	}
}

func TestDefaultKnowledgeRetrievalUsesBM25WithoutEmbeddings(t *testing.T) {
	cfg := Default()

	if cfg.Knowledge.RetrievalMode != "bm25" ||
		cfg.Knowledge.BM25TopK != 10 ||
		cfg.Knowledge.VectorTopK != 10 ||
		cfg.Knowledge.FinalTopK != 5 ||
		cfg.Knowledge.RRFK != 60 ||
		!cfg.Knowledge.FallbackToBM25 ||
		cfg.Embedding.Enabled ||
		cfg.Embedding.Dimension != 1536 {
		t.Fatalf("Knowledge=%#v Embedding=%#v", cfg.Knowledge, cfg.Embedding)
	}
}

func TestLoadAppliesKnowledgeAndEmbeddingEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WATCHOPS_KNOWLEDGE_RETRIEVAL_MODE", "hybrid")
	t.Setenv("WATCHOPS_KNOWLEDGE_BM25_TOP_K", "12")
	t.Setenv("WATCHOPS_KNOWLEDGE_VECTOR_TOP_K", "14")
	t.Setenv("WATCHOPS_KNOWLEDGE_FINAL_TOP_K", "6")
	t.Setenv("WATCHOPS_KNOWLEDGE_RRF_K", "70")
	t.Setenv("WATCHOPS_KNOWLEDGE_FALLBACK_TO_BM25", "true")
	t.Setenv("WATCHOPS_EMBEDDING_ENABLED", "true")
	t.Setenv("WATCHOPS_EMBEDDING_BASE_URL", "http://embedding.test/v1")
	t.Setenv("WATCHOPS_EMBEDDING_MODEL", "embed-test")
	t.Setenv("WATCHOPS_EMBEDDING_DIMENSION", "8")
	t.Setenv("WATCHOPS_EMBEDDING_REQUEST_TIMEOUT", "750ms")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Knowledge.RetrievalMode != "hybrid" ||
		cfg.Knowledge.BM25TopK != 12 ||
		cfg.Knowledge.VectorTopK != 14 ||
		cfg.Knowledge.FinalTopK != 6 ||
		cfg.Knowledge.RRFK != 70 ||
		!cfg.Embedding.Enabled ||
		cfg.Embedding.Model != "embed-test" ||
		cfg.Embedding.Dimension != 8 ||
		cfg.Embedding.RequestTimeout.Value() != 750*time.Millisecond {
		t.Fatalf("Knowledge=%#v Embedding=%#v", cfg.Knowledge, cfg.Embedding)
	}
}

func TestLoadAppliesMySQLEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("WATCHOPS_MYSQL_ENABLED", "true")
	t.Setenv("WATCHOPS_MYSQL_DSN", "user:pass@tcp(mysql:3306)/watchops")
	t.Setenv("WATCHOPS_MYSQL_MAX_OPEN_CONNS", "20")
	t.Setenv("WATCHOPS_MYSQL_MAX_IDLE_CONNS", "8")
	t.Setenv("WATCHOPS_MYSQL_CONN_MAX_LIFETIME", "10m")
	t.Setenv("WATCHOPS_MYSQL_REQUEST_TIMEOUT", "900ms")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.MySQL.Enabled ||
		cfg.MySQL.MaxOpenConns != 20 ||
		cfg.MySQL.MaxIdleConns != 8 ||
		cfg.MySQL.ConnMaxLifetime.Value() != 10*time.Minute ||
		cfg.MySQL.RequestTimeout.Value() != 900*time.Millisecond {
		t.Fatalf("MySQL config = %#v", cfg.MySQL)
	}
}

func TestLoadAppliesTelemetryEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("WATCHOPS_TELEMETRY_ENABLED", "true")
	t.Setenv("WATCHOPS_TELEMETRY_ENVIRONMENT", "test")
	t.Setenv("WATCHOPS_TELEMETRY_OTLP_ENDPOINT", "collector:4317")
	t.Setenv("WATCHOPS_TELEMETRY_INSECURE", "false")
	t.Setenv("WATCHOPS_TELEMETRY_SAMPLE_RATIO", "0.5")
	t.Setenv("WATCHOPS_TELEMETRY_EXPORT_TIMEOUT", "750ms")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Telemetry.Enabled ||
		cfg.Telemetry.Environment != "test" ||
		cfg.Telemetry.OTLPEndpoint != "collector:4317" ||
		cfg.Telemetry.Insecure ||
		cfg.Telemetry.SampleRatio != 0.5 ||
		cfg.Telemetry.ExportTimeout.Value() != 750*time.Millisecond {
		t.Fatalf("Telemetry config = %#v", cfg.Telemetry)
	}
}

func TestLoadAppliesAgentAndLLMEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("WATCHOPS_AGENT_MODE", "eino_react")
	t.Setenv("WATCHOPS_AGENT_MAX_ITERATIONS", "4")
	t.Setenv("WATCHOPS_AGENT_TIMEOUT", "12s")
	t.Setenv("WATCHOPS_AGENT_PROMPT_VERSION", "watchops_agent_v1")
	t.Setenv("WATCHOPS_LLM_ENABLED", "true")
	t.Setenv("WATCHOPS_LLM_BASE_URL", "http://model.local/v1")
	t.Setenv("WATCHOPS_LLM_MODEL", "test-model")
	t.Setenv("WATCHOPS_LLM_TEMPERATURE", "0.4")
	t.Setenv("WATCHOPS_LLM_REQUEST_TIMEOUT", "8s")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Agent.Mode != "eino_react" ||
		cfg.Agent.MaxIterations != 4 ||
		cfg.Agent.Timeout.Value() != 12*time.Second ||
		!cfg.LLM.Enabled ||
		cfg.LLM.Model != "test-model" ||
		cfg.LLM.Temperature != 0.4 ||
		cfg.LLM.RequestTimeout.Value() != 8*time.Second {
		t.Fatalf("Agent=%#v LLM=%#v", cfg.Agent, cfg.LLM)
	}
}
