package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultConfigPath = "configs/config.json"
	envPrefix         = "WATCHOPS_"
)

// Duration allows human-readable duration strings in JSON configuration.
type Duration time.Duration

func (d *Duration) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return errors.New("duration must be a string such as \"5s\"")
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", value, err)
	}

	*d = Duration(parsed)
	return nil
}

func (d Duration) Value() time.Duration {
	return time.Duration(d)
}

type Config struct {
	Server        ServerConfig        `json:"server"`
	Log           LogConfig           `json:"log"`
	Redis         RedisConfig         `json:"redis"`
	Session       SessionConfig       `json:"session"`
	Summary       SummaryConfig       `json:"summary"`
	Elasticsearch ElasticsearchConfig `json:"elasticsearch"`
	Knowledge     KnowledgeConfig     `json:"knowledge"`
	Embedding     EmbeddingConfig     `json:"embedding"`
	Logs          LogsConfig          `json:"logs"`
	Metrics       MetricsConfig       `json:"metrics"`
	Traces        TracesConfig        `json:"traces"`
	MySQL         MySQLConfig         `json:"mysql"`
	Agent         AgentConfig         `json:"agent"`
	LLM           LLMConfig           `json:"llm"`
	Telemetry     TelemetryConfig     `json:"telemetry"`
}

type ServerConfig struct {
	Address           string   `json:"address"`
	ReadHeaderTimeout Duration `json:"read_header_timeout"`
	ReadTimeout       Duration `json:"read_timeout"`
	WriteTimeout      Duration `json:"write_timeout"`
	IdleTimeout       Duration `json:"idle_timeout"`
	ShutdownTimeout   Duration `json:"shutdown_timeout"`
}

type LogConfig struct {
	Level string `json:"level"`
}

type RedisConfig struct {
	Address      string   `json:"address"`
	Username     string   `json:"username"`
	Password     string   `json:"password"`
	DB           int      `json:"db"`
	DialTimeout  Duration `json:"dial_timeout"`
	ReadTimeout  Duration `json:"read_timeout"`
	WriteTimeout Duration `json:"write_timeout"`
}

type SessionConfig struct {
	RecentWindowSize int      `json:"recent_window_size"`
	SummaryThreshold int      `json:"summary_threshold"`
	TTL              Duration `json:"ttl"`
}

type SummaryConfig struct {
	Mode                    string   `json:"mode"`
	PromptVersion           string   `json:"prompt_version"`
	Timeout                 Duration `json:"timeout"`
	FallbackToDeterministic bool     `json:"fallback_to_deterministic"`
}

type ElasticsearchConfig struct {
	Enabled        bool     `json:"enabled"`
	Addresses      []string `json:"addresses"`
	Username       string   `json:"username"`
	Password       string   `json:"password"`
	KnowledgeIndex string   `json:"knowledge_index"`
	RequestTimeout Duration `json:"request_timeout"`
}

type KnowledgeConfig struct {
	ChunkMaxSize   int    `json:"chunk_max_size"`
	RetrievalMode  string `json:"retrieval_mode"`
	BM25TopK       int    `json:"bm25_top_k"`
	VectorTopK     int    `json:"vector_top_k"`
	FinalTopK      int    `json:"final_top_k"`
	RRFK           int    `json:"rrf_k"`
	FallbackToBM25 bool   `json:"fallback_to_bm25"`
}

type EmbeddingConfig struct {
	Enabled        bool     `json:"enabled"`
	Provider       string   `json:"provider"`
	BaseURL        string   `json:"base_url"`
	APIKeyEnv      string   `json:"api_key_env"`
	Model          string   `json:"model"`
	Dimension      int      `json:"dimension"`
	RequestTimeout Duration `json:"request_timeout"`
}

type LogsConfig struct {
	Backend        string `json:"backend"`
	Index          string `json:"index"`
	FallbackToMock bool   `json:"fallback_to_mock"`
	DefaultLimit   int    `json:"default_limit"`
}

type MetricsConfig struct {
	Backend        string            `json:"backend"`
	BaseURL        string            `json:"base_url"`
	FallbackToMock bool              `json:"fallback_to_mock"`
	DefaultStep    Duration          `json:"default_step"`
	RequestTimeout Duration          `json:"request_timeout"`
	Queries        map[string]string `json:"queries"`
}

type TracesConfig struct {
	Backend        string   `json:"backend"`
	BaseURL        string   `json:"base_url"`
	FallbackToMock bool     `json:"fallback_to_mock"`
	DefaultLimit   int      `json:"default_limit"`
	RequestTimeout Duration `json:"request_timeout"`
	DefaultService string   `json:"default_service"`
}

type MySQLConfig struct {
	Enabled         bool     `json:"enabled"`
	DSN             string   `json:"dsn"`
	MaxOpenConns    int      `json:"max_open_conns"`
	MaxIdleConns    int      `json:"max_idle_conns"`
	ConnMaxLifetime Duration `json:"conn_max_lifetime"`
	RequestTimeout  Duration `json:"request_timeout"`
}

type AgentConfig struct {
	Mode          string   `json:"mode"`
	MaxIterations int      `json:"max_iterations"`
	Timeout       Duration `json:"timeout"`
	PromptVersion string   `json:"prompt_version"`
}

type LLMConfig struct {
	Enabled        bool     `json:"enabled"`
	Provider       string   `json:"provider"`
	BaseURL        string   `json:"base_url"`
	APIKeyEnv      string   `json:"api_key_env"`
	Model          string   `json:"model"`
	Temperature    float64  `json:"temperature"`
	RequestTimeout Duration `json:"request_timeout"`
}

type TelemetryConfig struct {
	Enabled       bool     `json:"enabled"`
	ServiceName   string   `json:"service_name"`
	Environment   string   `json:"environment"`
	OTLPEndpoint  string   `json:"otlp_endpoint"`
	Insecure      bool     `json:"insecure"`
	SampleRatio   float64  `json:"sample_ratio"`
	ExportTimeout Duration `json:"export_timeout"`
}

func Default() Config {
	return Config{
		Server: ServerConfig{
			Address:           ":8080",
			ReadHeaderTimeout: Duration(5 * time.Second),
			ReadTimeout:       Duration(15 * time.Second),
			WriteTimeout:      Duration(15 * time.Second),
			IdleTimeout:       Duration(60 * time.Second),
			ShutdownTimeout:   Duration(10 * time.Second),
		},
		Log: LogConfig{
			Level: "info",
		},
		Redis: RedisConfig{
			Address:      "localhost:6379",
			DB:           0,
			DialTimeout:  Duration(2 * time.Second),
			ReadTimeout:  Duration(2 * time.Second),
			WriteTimeout: Duration(2 * time.Second),
		},
		Session: SessionConfig{
			RecentWindowSize: 12,
			SummaryThreshold: 12,
			TTL:              Duration(24 * time.Hour),
		},
		Summary: SummaryConfig{
			Mode:                    "deterministic",
			PromptVersion:           "session_summary_v1",
			Timeout:                 Duration(10 * time.Second),
			FallbackToDeterministic: true,
		},
		Elasticsearch: ElasticsearchConfig{
			Enabled:        false,
			Addresses:      []string{"http://localhost:9200"},
			KnowledgeIndex: "watchops_knowledge",
			RequestTimeout: Duration(3 * time.Second),
		},
		Knowledge: KnowledgeConfig{
			ChunkMaxSize:   1200,
			RetrievalMode:  "bm25",
			BM25TopK:       10,
			VectorTopK:     10,
			FinalTopK:      5,
			RRFK:           60,
			FallbackToBM25: true,
		},
		Embedding: EmbeddingConfig{
			Enabled:        false,
			Provider:       "openai_compatible",
			APIKeyEnv:      "WATCHOPS_EMBEDDING_API_KEY",
			Dimension:      1536,
			RequestTimeout: Duration(10 * time.Second),
		},
		Logs: LogsConfig{
			Backend:        "mock",
			Index:          "watchops_logs",
			FallbackToMock: true,
			DefaultLimit:   20,
		},
		Metrics: MetricsConfig{
			Backend:        "mock",
			BaseURL:        "http://localhost:9090",
			FallbackToMock: true,
			DefaultStep:    Duration(30 * time.Second),
			RequestTimeout: Duration(3 * time.Second),
			Queries: map[string]string{
				"checkout_error_rate":        "watchops_checkout_error_rate",
				"checkout_p95_latency":       "watchops_checkout_p95_latency_seconds",
				"checkout_timeout_total":     "watchops_checkout_timeout_total",
				"payment_dependency_latency": "watchops_payment_dependency_latency_seconds",
			},
		},
		Traces: TracesConfig{
			Backend:        "mock",
			BaseURL:        "http://localhost:16686",
			FallbackToMock: true,
			DefaultLimit:   10,
			RequestTimeout: Duration(3 * time.Second),
			DefaultService: "watchops-lite",
		},
		MySQL: MySQLConfig{
			Enabled:         false,
			DSN:             "watchops:watchops@tcp(localhost:3306)/watchops_lite?parseTime=true",
			MaxOpenConns:    10,
			MaxIdleConns:    5,
			ConnMaxLifetime: Duration(30 * time.Minute),
			RequestTimeout:  Duration(3 * time.Second),
		},
		Agent: AgentConfig{
			Mode:          "deterministic",
			MaxIterations: 6,
			Timeout:       Duration(30 * time.Second),
			PromptVersion: "watchops_agent_v1",
		},
		LLM: LLMConfig{
			Enabled:        false,
			Provider:       "openai_compatible",
			APIKeyEnv:      "WATCHOPS_LLM_API_KEY",
			Temperature:    0.2,
			RequestTimeout: Duration(30 * time.Second),
		},
		Telemetry: TelemetryConfig{
			Enabled:       false,
			ServiceName:   "watchops-lite",
			Environment:   "local",
			OTLPEndpoint:  "localhost:4317",
			Insecure:      true,
			SampleRatio:   1,
			ExportTimeout: Duration(3 * time.Second),
		},
	}
}

// Load applies configuration in this order: defaults, JSON file, environment.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		path = defaultConfigPath
	}

	if err := loadFile(path, &cfg); err != nil {
		return Config{}, err
	}
	if err := applyEnvironment(&cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate configuration: %w", err)
	}

	return cfg, nil
}

func loadFile(path string, cfg *Config) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open config file %q: %w", path, err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(cfg); err != nil {
		return fmt.Errorf("decode config file %q: %w", path, err)
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode config file %q: expected one JSON object", path)
	}

	return nil
}

func applyEnvironment(cfg *Config) error {
	setString("SERVER_ADDRESS", &cfg.Server.Address)
	setString("LOG_LEVEL", &cfg.Log.Level)
	setString("REDIS_ADDRESS", &cfg.Redis.Address)
	setString("REDIS_USERNAME", &cfg.Redis.Username)
	setString("REDIS_PASSWORD", &cfg.Redis.Password)
	setString("SUMMARY_MODE", &cfg.Summary.Mode)
	setString("SUMMARY_PROMPT_VERSION", &cfg.Summary.PromptVersion)
	setString("ELASTICSEARCH_USERNAME", &cfg.Elasticsearch.Username)
	setString("ELASTICSEARCH_PASSWORD", &cfg.Elasticsearch.Password)
	setString("ELASTICSEARCH_KNOWLEDGE_INDEX", &cfg.Elasticsearch.KnowledgeIndex)
	setString("KNOWLEDGE_RETRIEVAL_MODE", &cfg.Knowledge.RetrievalMode)
	setString("EMBEDDING_PROVIDER", &cfg.Embedding.Provider)
	setString("EMBEDDING_BASE_URL", &cfg.Embedding.BaseURL)
	setString("EMBEDDING_API_KEY_ENV", &cfg.Embedding.APIKeyEnv)
	setString("EMBEDDING_MODEL", &cfg.Embedding.Model)
	setString("LOGS_BACKEND", &cfg.Logs.Backend)
	setString("LOGS_INDEX", &cfg.Logs.Index)
	setString("METRICS_BACKEND", &cfg.Metrics.Backend)
	setString("METRICS_BASE_URL", &cfg.Metrics.BaseURL)
	setString("TRACES_BACKEND", &cfg.Traces.Backend)
	setString("TRACES_BASE_URL", &cfg.Traces.BaseURL)
	setString("TRACES_DEFAULT_SERVICE", &cfg.Traces.DefaultService)
	setString("MYSQL_DSN", &cfg.MySQL.DSN)
	setString("AGENT_MODE", &cfg.Agent.Mode)
	setString("AGENT_PROMPT_VERSION", &cfg.Agent.PromptVersion)
	setString("LLM_PROVIDER", &cfg.LLM.Provider)
	setString("LLM_BASE_URL", &cfg.LLM.BaseURL)
	setString("LLM_API_KEY_ENV", &cfg.LLM.APIKeyEnv)
	setString("LLM_MODEL", &cfg.LLM.Model)
	setString("TELEMETRY_SERVICE_NAME", &cfg.Telemetry.ServiceName)
	setString("TELEMETRY_ENVIRONMENT", &cfg.Telemetry.Environment)
	setString("TELEMETRY_OTLP_ENDPOINT", &cfg.Telemetry.OTLPEndpoint)

	durationValues := []struct {
		name   string
		target *Duration
	}{
		{"SERVER_READ_HEADER_TIMEOUT", &cfg.Server.ReadHeaderTimeout},
		{"SERVER_READ_TIMEOUT", &cfg.Server.ReadTimeout},
		{"SERVER_WRITE_TIMEOUT", &cfg.Server.WriteTimeout},
		{"SERVER_IDLE_TIMEOUT", &cfg.Server.IdleTimeout},
		{"SERVER_SHUTDOWN_TIMEOUT", &cfg.Server.ShutdownTimeout},
		{"REDIS_DIAL_TIMEOUT", &cfg.Redis.DialTimeout},
		{"REDIS_READ_TIMEOUT", &cfg.Redis.ReadTimeout},
		{"REDIS_WRITE_TIMEOUT", &cfg.Redis.WriteTimeout},
		{"SESSION_TTL", &cfg.Session.TTL},
		{"SUMMARY_TIMEOUT", &cfg.Summary.Timeout},
		{"ELASTICSEARCH_REQUEST_TIMEOUT", &cfg.Elasticsearch.RequestTimeout},
		{"EMBEDDING_REQUEST_TIMEOUT", &cfg.Embedding.RequestTimeout},
		{"METRICS_DEFAULT_STEP", &cfg.Metrics.DefaultStep},
		{"METRICS_REQUEST_TIMEOUT", &cfg.Metrics.RequestTimeout},
		{"TRACES_REQUEST_TIMEOUT", &cfg.Traces.RequestTimeout},
		{"MYSQL_CONN_MAX_LIFETIME", &cfg.MySQL.ConnMaxLifetime},
		{"MYSQL_REQUEST_TIMEOUT", &cfg.MySQL.RequestTimeout},
		{"AGENT_TIMEOUT", &cfg.Agent.Timeout},
		{"LLM_REQUEST_TIMEOUT", &cfg.LLM.RequestTimeout},
		{"TELEMETRY_EXPORT_TIMEOUT", &cfg.Telemetry.ExportTimeout},
	}
	for _, item := range durationValues {
		if err := setDuration(item.name, item.target); err != nil {
			return err
		}
	}

	integerValues := []struct {
		name   string
		target *int
	}{
		{"REDIS_DB", &cfg.Redis.DB},
		{"SESSION_RECENT_WINDOW_SIZE", &cfg.Session.RecentWindowSize},
		{"SESSION_SUMMARY_THRESHOLD", &cfg.Session.SummaryThreshold},
		{"KNOWLEDGE_CHUNK_MAX_SIZE", &cfg.Knowledge.ChunkMaxSize},
		{"KNOWLEDGE_BM25_TOP_K", &cfg.Knowledge.BM25TopK},
		{"KNOWLEDGE_VECTOR_TOP_K", &cfg.Knowledge.VectorTopK},
		{"KNOWLEDGE_FINAL_TOP_K", &cfg.Knowledge.FinalTopK},
		{"KNOWLEDGE_RRF_K", &cfg.Knowledge.RRFK},
		{"EMBEDDING_DIMENSION", &cfg.Embedding.Dimension},
		{"LOGS_DEFAULT_LIMIT", &cfg.Logs.DefaultLimit},
		{"TRACES_DEFAULT_LIMIT", &cfg.Traces.DefaultLimit},
		{"MYSQL_MAX_OPEN_CONNS", &cfg.MySQL.MaxOpenConns},
		{"MYSQL_MAX_IDLE_CONNS", &cfg.MySQL.MaxIdleConns},
		{"AGENT_MAX_ITERATIONS", &cfg.Agent.MaxIterations},
	}
	for _, item := range integerValues {
		if err := setInteger(item.name, item.target); err != nil {
			return err
		}
	}

	if value, ok := lookup("ELASTICSEARCH_ADDRESSES"); ok {
		cfg.Elasticsearch.Addresses = splitCommaSeparated(value)
	}
	if value, ok := lookup("ELASTICSEARCH_ENABLED"); ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("%sELASTICSEARCH_ENABLED must be a boolean: %w", envPrefix, err)
		}
		cfg.Elasticsearch.Enabled = parsed
	}
	if value, ok := lookup("KNOWLEDGE_FALLBACK_TO_BM25"); ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("%sKNOWLEDGE_FALLBACK_TO_BM25 must be a boolean: %w", envPrefix, err)
		}
		cfg.Knowledge.FallbackToBM25 = parsed
	}
	if value, ok := lookup("EMBEDDING_ENABLED"); ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("%sEMBEDDING_ENABLED must be a boolean: %w", envPrefix, err)
		}
		cfg.Embedding.Enabled = parsed
	}
	if value, ok := lookup("SUMMARY_FALLBACK_TO_DETERMINISTIC"); ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf(
				"%sSUMMARY_FALLBACK_TO_DETERMINISTIC must be a boolean: %w",
				envPrefix,
				err,
			)
		}
		cfg.Summary.FallbackToDeterministic = parsed
	}
	if value, ok := lookup("LOGS_FALLBACK_TO_MOCK"); ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("%sLOGS_FALLBACK_TO_MOCK must be a boolean: %w", envPrefix, err)
		}
		cfg.Logs.FallbackToMock = parsed
	}
	if value, ok := lookup("METRICS_FALLBACK_TO_MOCK"); ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("%sMETRICS_FALLBACK_TO_MOCK must be a boolean: %w", envPrefix, err)
		}
		cfg.Metrics.FallbackToMock = parsed
	}
	if value, ok := lookup("TRACES_FALLBACK_TO_MOCK"); ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("%sTRACES_FALLBACK_TO_MOCK must be a boolean: %w", envPrefix, err)
		}
		cfg.Traces.FallbackToMock = parsed
	}
	metricQueryEnvironment := map[string]string{
		"METRICS_QUERY_CHECKOUT_ERROR_RATE":        "checkout_error_rate",
		"METRICS_QUERY_CHECKOUT_P95_LATENCY":       "checkout_p95_latency",
		"METRICS_QUERY_CHECKOUT_TIMEOUT_TOTAL":     "checkout_timeout_total",
		"METRICS_QUERY_PAYMENT_DEPENDENCY_LATENCY": "payment_dependency_latency",
	}
	for environmentName, queryName := range metricQueryEnvironment {
		if value, ok := lookup(environmentName); ok {
			if cfg.Metrics.Queries == nil {
				cfg.Metrics.Queries = make(map[string]string)
			}
			cfg.Metrics.Queries[queryName] = value
		}
	}
	if value, ok := lookup("MYSQL_ENABLED"); ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("%sMYSQL_ENABLED must be a boolean: %w", envPrefix, err)
		}
		cfg.MySQL.Enabled = parsed
	}
	if value, ok := lookup("LLM_ENABLED"); ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("%sLLM_ENABLED must be a boolean: %w", envPrefix, err)
		}
		cfg.LLM.Enabled = parsed
	}
	if value, ok := lookup("LLM_TEMPERATURE"); ok {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("%sLLM_TEMPERATURE must be a number: %w", envPrefix, err)
		}
		cfg.LLM.Temperature = parsed
	}

	if value, ok := lookup("TELEMETRY_ENABLED"); ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("%sTELEMETRY_ENABLED must be a boolean: %w", envPrefix, err)
		}
		cfg.Telemetry.Enabled = parsed
	}
	if value, ok := lookup("TELEMETRY_INSECURE"); ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("%sTELEMETRY_INSECURE must be a boolean: %w", envPrefix, err)
		}
		cfg.Telemetry.Insecure = parsed
	}

	if value, ok := lookup("TELEMETRY_SAMPLE_RATIO"); ok {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("%sTELEMETRY_SAMPLE_RATIO must be a number: %w", envPrefix, err)
		}
		cfg.Telemetry.SampleRatio = parsed
	}

	return nil
}

func setString(name string, target *string) {
	if value, ok := lookup(name); ok {
		*target = value
	}
}

func setDuration(name string, target *Duration) error {
	value, ok := lookup(name)
	if !ok {
		return nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("%s%s must be a duration: %w", envPrefix, name, err)
	}
	*target = Duration(parsed)
	return nil
}

func setInteger(name string, target *int) error {
	value, ok := lookup(name)
	if !ok {
		return nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("%s%s must be an integer: %w", envPrefix, name, err)
	}
	*target = parsed
	return nil
}

func lookup(name string) (string, bool) {
	value, ok := os.LookupEnv(envPrefix + name)
	return strings.TrimSpace(value), ok
}

func splitCommaSeparated(value string) []string {
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

func (cfg Config) Validate() error {
	if strings.TrimSpace(cfg.Server.Address) == "" {
		return errors.New("server.address is required")
	}

	durations := []struct {
		name  string
		value Duration
	}{
		{"server.read_header_timeout", cfg.Server.ReadHeaderTimeout},
		{"server.read_timeout", cfg.Server.ReadTimeout},
		{"server.write_timeout", cfg.Server.WriteTimeout},
		{"server.idle_timeout", cfg.Server.IdleTimeout},
		{"server.shutdown_timeout", cfg.Server.ShutdownTimeout},
	}
	for _, item := range durations {
		if item.value <= 0 {
			return fmt.Errorf("%s must be greater than zero", item.name)
		}
	}

	if strings.TrimSpace(cfg.Redis.Address) == "" {
		return errors.New("redis.address is required")
	}
	if cfg.Redis.DB < 0 {
		return errors.New("redis.db must not be negative")
	}
	redisDurations := []struct {
		name  string
		value Duration
	}{
		{"redis.dial_timeout", cfg.Redis.DialTimeout},
		{"redis.read_timeout", cfg.Redis.ReadTimeout},
		{"redis.write_timeout", cfg.Redis.WriteTimeout},
	}
	for _, item := range redisDurations {
		if item.value <= 0 {
			return fmt.Errorf("%s must be greater than zero", item.name)
		}
	}

	if cfg.Session.RecentWindowSize <= 0 {
		return errors.New("session.recent_window_size must be greater than zero")
	}
	if cfg.Session.SummaryThreshold <= 0 {
		return errors.New("session.summary_threshold must be greater than zero")
	}
	if cfg.Session.SummaryThreshold > cfg.Session.RecentWindowSize {
		return errors.New("session.summary_threshold must not exceed session.recent_window_size")
	}
	if cfg.Session.TTL <= 0 {
		return errors.New("session.ttl must be greater than zero")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Summary.Mode)) {
	case "deterministic", "llm":
	default:
		return errors.New("summary.mode must be deterministic or llm")
	}
	if strings.TrimSpace(cfg.Summary.PromptVersion) == "" {
		return errors.New("summary.prompt_version is required")
	}
	if cfg.Summary.Timeout <= 0 {
		return errors.New("summary.timeout must be greater than zero")
	}
	if strings.EqualFold(cfg.Summary.Mode, "llm") && !cfg.Summary.FallbackToDeterministic {
		return errors.New("summary.fallback_to_deterministic must be true in llm mode")
	}

	if cfg.Elasticsearch.RequestTimeout <= 0 {
		return errors.New("elasticsearch.request_timeout must be greater than zero")
	}
	if cfg.Knowledge.ChunkMaxSize <= 0 {
		return errors.New("knowledge.chunk_max_size must be greater than zero")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Knowledge.RetrievalMode)) {
	case "bm25", "vector", "hybrid":
	default:
		return errors.New("knowledge.retrieval_mode must be bm25, vector, or hybrid")
	}
	knowledgeLimits := []struct {
		name  string
		value int
		max   int
	}{
		{"knowledge.bm25_top_k", cfg.Knowledge.BM25TopK, 100},
		{"knowledge.vector_top_k", cfg.Knowledge.VectorTopK, 100},
		{"knowledge.final_top_k", cfg.Knowledge.FinalTopK, 20},
	}
	for _, item := range knowledgeLimits {
		if item.value < 1 || item.value > item.max {
			return fmt.Errorf("%s must be between 1 and %d", item.name, item.max)
		}
	}
	if cfg.Knowledge.RRFK <= 0 {
		return errors.New("knowledge.rrf_k must be greater than zero")
	}
	if !strings.EqualFold(strings.TrimSpace(cfg.Embedding.Provider), "openai_compatible") {
		return errors.New("embedding.provider must be openai_compatible")
	}
	if strings.TrimSpace(cfg.Embedding.APIKeyEnv) == "" {
		return errors.New("embedding.api_key_env is required")
	}
	if cfg.Embedding.Dimension <= 0 {
		return errors.New("embedding.dimension must be greater than zero")
	}
	if cfg.Embedding.RequestTimeout <= 0 {
		return errors.New("embedding.request_timeout must be greater than zero")
	}
	if cfg.Embedding.Enabled &&
		(strings.TrimSpace(cfg.Embedding.BaseURL) == "" || strings.TrimSpace(cfg.Embedding.Model) == "") {
		return errors.New("embedding.base_url and embedding.model are required when embeddings are enabled")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Logs.Backend)) {
	case "mock", "elasticsearch":
	default:
		return errors.New("logs.backend must be mock or elasticsearch")
	}
	if strings.TrimSpace(cfg.Logs.Index) == "" {
		return errors.New("logs.index is required")
	}
	if cfg.Logs.DefaultLimit < 1 || cfg.Logs.DefaultLimit > 100 {
		return errors.New("logs.default_limit must be between 1 and 100")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Metrics.Backend)) {
	case "mock", "prometheus":
	default:
		return errors.New("metrics.backend must be mock or prometheus")
	}
	if strings.TrimSpace(cfg.Metrics.BaseURL) == "" {
		return errors.New("metrics.base_url is required")
	}
	if cfg.Metrics.DefaultStep <= 0 {
		return errors.New("metrics.default_step must be greater than zero")
	}
	if cfg.Metrics.RequestTimeout <= 0 {
		return errors.New("metrics.request_timeout must be greater than zero")
	}
	if len(cfg.Metrics.Queries) == 0 {
		return errors.New("metrics.queries must contain at least one query")
	}
	for name, query := range cfg.Metrics.Queries {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(query) == "" {
			return errors.New("metrics.queries must not contain empty names or expressions")
		}
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Traces.Backend)) {
	case "mock", "jaeger":
	default:
		return errors.New("traces.backend must be mock or jaeger")
	}
	if strings.TrimSpace(cfg.Traces.BaseURL) == "" {
		return errors.New("traces.base_url is required")
	}
	if cfg.Traces.DefaultLimit < 1 || cfg.Traces.DefaultLimit > 100 {
		return errors.New("traces.default_limit must be between 1 and 100")
	}
	if cfg.Traces.RequestTimeout <= 0 {
		return errors.New("traces.request_timeout must be greater than zero")
	}
	if strings.TrimSpace(cfg.Traces.DefaultService) == "" {
		return errors.New("traces.default_service is required")
	}
	if cfg.Elasticsearch.Enabled {
		if len(cfg.Elasticsearch.Addresses) == 0 {
			return errors.New("elasticsearch.addresses is required when Elasticsearch is enabled")
		}
		for _, address := range cfg.Elasticsearch.Addresses {
			if strings.TrimSpace(address) == "" {
				return errors.New("elasticsearch.addresses must not contain empty values")
			}
		}
		if strings.TrimSpace(cfg.Elasticsearch.KnowledgeIndex) == "" {
			return errors.New("elasticsearch.knowledge_index is required when Elasticsearch is enabled")
		}
	}
	if cfg.MySQL.MaxOpenConns <= 0 {
		return errors.New("mysql.max_open_conns must be greater than zero")
	}
	if cfg.MySQL.MaxIdleConns < 0 {
		return errors.New("mysql.max_idle_conns must not be negative")
	}
	if cfg.MySQL.MaxIdleConns > cfg.MySQL.MaxOpenConns {
		return errors.New("mysql.max_idle_conns must not exceed mysql.max_open_conns")
	}
	if cfg.MySQL.ConnMaxLifetime <= 0 {
		return errors.New("mysql.conn_max_lifetime must be greater than zero")
	}
	if cfg.MySQL.RequestTimeout <= 0 {
		return errors.New("mysql.request_timeout must be greater than zero")
	}
	if cfg.MySQL.Enabled && strings.TrimSpace(cfg.MySQL.DSN) == "" {
		return errors.New("mysql.dsn is required when MySQL is enabled")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Agent.Mode)) {
	case "deterministic", "eino_react":
	default:
		return errors.New("agent.mode must be deterministic or eino_react")
	}
	if cfg.Agent.MaxIterations <= 0 || cfg.Agent.MaxIterations > 20 {
		return errors.New("agent.max_iterations must be between 1 and 20")
	}
	if cfg.Agent.Timeout <= 0 {
		return errors.New("agent.timeout must be greater than zero")
	}
	if strings.TrimSpace(cfg.Agent.PromptVersion) == "" {
		return errors.New("agent.prompt_version is required")
	}
	if strings.ToLower(strings.TrimSpace(cfg.LLM.Provider)) != "openai_compatible" {
		return errors.New("llm.provider must be openai_compatible")
	}
	if strings.TrimSpace(cfg.LLM.APIKeyEnv) == "" {
		return errors.New("llm.api_key_env is required")
	}
	if cfg.LLM.Temperature < 0 || cfg.LLM.Temperature > 2 {
		return errors.New("llm.temperature must be between 0 and 2")
	}
	if cfg.LLM.RequestTimeout <= 0 {
		return errors.New("llm.request_timeout must be greater than zero")
	}

	switch strings.ToLower(cfg.Log.Level) {
	case "debug", "info", "warn", "error":
	default:
		return errors.New("log.level must be one of debug, info, warn, or error")
	}

	if strings.TrimSpace(cfg.Telemetry.ServiceName) == "" {
		return errors.New("telemetry.service_name is required")
	}
	if strings.TrimSpace(cfg.Telemetry.Environment) == "" {
		return errors.New("telemetry.environment is required")
	}
	if cfg.Telemetry.ExportTimeout <= 0 {
		return errors.New("telemetry.export_timeout must be greater than zero")
	}
	if cfg.Telemetry.SampleRatio < 0 || cfg.Telemetry.SampleRatio > 1 {
		return errors.New("telemetry.sample_ratio must be between 0 and 1")
	}
	if cfg.Telemetry.Enabled && strings.TrimSpace(cfg.Telemetry.OTLPEndpoint) == "" {
		return errors.New("telemetry.otlp_endpoint is required when telemetry is enabled")
	}

	return nil
}
