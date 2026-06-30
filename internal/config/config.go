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
	Server    ServerConfig    `json:"server"`
	Log       LogConfig       `json:"log"`
	Redis     RedisConfig     `json:"redis"`
	Session   SessionConfig   `json:"session"`
	Telemetry TelemetryConfig `json:"telemetry"`
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

type TelemetryConfig struct {
	Enabled      bool    `json:"enabled"`
	ServiceName  string  `json:"service_name"`
	OTLPEndpoint string  `json:"otlp_endpoint"`
	SampleRatio  float64 `json:"sample_ratio"`
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
		Telemetry: TelemetryConfig{
			Enabled:      false,
			ServiceName:  "watchops-lite",
			OTLPEndpoint: "http://localhost:4318",
			SampleRatio:  1,
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
	setString("TELEMETRY_SERVICE_NAME", &cfg.Telemetry.ServiceName)
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
	}
	for _, item := range integerValues {
		if err := setInteger(item.name, item.target); err != nil {
			return err
		}
	}

	if value, ok := lookup("TELEMETRY_ENABLED"); ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("%sTELEMETRY_ENABLED must be a boolean: %w", envPrefix, err)
		}
		cfg.Telemetry.Enabled = parsed
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

	switch strings.ToLower(cfg.Log.Level) {
	case "debug", "info", "warn", "error":
	default:
		return errors.New("log.level must be one of debug, info, warn, or error")
	}

	if strings.TrimSpace(cfg.Telemetry.ServiceName) == "" {
		return errors.New("telemetry.service_name is required")
	}
	if cfg.Telemetry.SampleRatio < 0 || cfg.Telemetry.SampleRatio > 1 {
		return errors.New("telemetry.sample_ratio must be between 0 and 1")
	}
	if cfg.Telemetry.Enabled && strings.TrimSpace(cfg.Telemetry.OTLPEndpoint) == "" {
		return errors.New("telemetry.otlp_endpoint is required when telemetry is enabled")
	}

	return nil
}
