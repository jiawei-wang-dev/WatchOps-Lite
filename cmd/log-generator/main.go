package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultOutput = "tmp/generated_checkout_logs.jsonl"

type generatorConfig struct {
	Scenario string
	Count    int
	Seed     int64
	Now      time.Time
}

type generatedEvent struct {
	ID         string         `json:"id"`
	Timestamp  time.Time      `json:"timestamp"`
	Service    string         `json:"service"`
	Level      string         `json:"level"`
	Message    string         `json:"message"`
	RequestID  string         `json:"request_id"`
	TraceID    string         `json:"trace_id"`
	SpanID     string         `json:"span_id"`
	Endpoint   string         `json:"endpoint"`
	StatusCode int            `json:"status_code"`
	LatencyMS  int            `json:"latency_ms"`
	ErrorCode  string         `json:"error_code,omitempty"`
	Attributes map[string]any `json:"attributes"`
	CreatedAt  time.Time      `json:"created_at"`
}

type scenarioTemplate struct {
	service   string
	endpoint  string
	messages  []string
	errorCode string
}

var scenarios = map[string]scenarioTemplate{
	"checkout-timeout": {
		service:   "checkout",
		endpoint:  "/checkout",
		messages:  []string{"checkout request accepted", "payment dependency latency exceeded threshold", "context deadline exceeded calling payment", "checkout upstream timeout"},
		errorCode: "UPSTREAM_TIMEOUT",
	},
	"payment-errors": {
		service:   "payment",
		endpoint:  "/payments/authorize",
		messages:  []string{"payment authorization started", "payment provider returned retryable error", "payment authorization failed", "payment retry amplification detected"},
		errorCode: "PAYMENT_PROVIDER_ERROR",
	},
	"redis-latency": {
		service:   "redis",
		endpoint:  "GET session",
		messages:  []string{"Redis command completed", "Redis command latency above threshold", "Redis connection pool wait increased", "Redis operation timed out"},
		errorCode: "REDIS_TIMEOUT",
	},
}

func main() {
	scenario := flag.String("scenario", "checkout-timeout", "demo scenario")
	count := flag.Int("count", 200, "number of JSONL events")
	seed := flag.Int64("seed", 1, "deterministic random seed")
	output := flag.String("output", defaultOutput, "output JSONL path")
	nowValue := flag.String("now", "", "RFC3339 window end; defaults to current UTC time")
	flag.Parse()

	now := time.Now().UTC()
	if strings.TrimSpace(*nowValue) != "" {
		parsed, err := time.Parse(time.RFC3339, *nowValue)
		if err != nil {
			exitError(fmt.Errorf("parse --now: %w", err))
		}
		now = parsed.UTC()
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
		exitError(fmt.Errorf("create output directory: %w", err))
	}
	file, err := os.Create(*output)
	if err != nil {
		exitError(fmt.Errorf("create output: %w", err))
	}
	writer := bufio.NewWriter(file)
	err = generate(writer, generatorConfig{
		Scenario: *scenario,
		Count:    *count,
		Seed:     *seed,
		Now:      now,
	})
	if flushErr := writer.Flush(); err == nil {
		err = flushErr
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		exitError(err)
	}
	fmt.Printf(
		"generated %d %s log events at %s\n",
		*count,
		*scenario,
		*output,
	)
}

func generate(writer io.Writer, config generatorConfig) error {
	template, ok := scenarios[config.Scenario]
	if !ok {
		return fmt.Errorf("unsupported scenario %q", config.Scenario)
	}
	if config.Count <= 0 || config.Count > 10000 {
		return errors.New("count must be between 1 and 10000")
	}
	if config.Now.IsZero() {
		return errors.New("now is required")
	}
	random := rand.New(rand.NewSource(config.Seed))
	encoder := json.NewEncoder(writer)
	windowStart := config.Now.Add(-20 * time.Minute)
	for index := 0; index < config.Count; index++ {
		progress := time.Duration(index) * 20 * time.Minute / time.Duration(config.Count)
		timestamp := windowStart.Add(progress)
		errorEvent := index%4 != 0
		level := "info"
		statusCode := 200
		errorCode := ""
		if errorEvent {
			level = []string{"warn", "error"}[random.Intn(2)]
			statusCode = []int{500, 502, 504}[random.Intn(3)]
			errorCode = template.errorCode
		}
		event := generatedEvent{
			ID:         fmt.Sprintf("generated-%s-%04d", config.Scenario, index+1),
			Timestamp:  timestamp.UTC(),
			Service:    template.service,
			Level:      level,
			Message:    template.messages[index%len(template.messages)],
			RequestID:  fmt.Sprintf("req-%08x", random.Uint32()),
			TraceID:    fmt.Sprintf("%032x", random.Uint64()),
			SpanID:     fmt.Sprintf("%016x", random.Uint64()),
			Endpoint:   template.endpoint,
			StatusCode: statusCode,
			LatencyMS:  40 + random.Intn(2460),
			ErrorCode:  errorCode,
			Attributes: map[string]any{
				"environment": "demo",
				"scenario":    config.Scenario,
				"request_id":  "",
				"endpoint":    template.endpoint,
				"status_code": statusCode,
			},
			CreatedAt: timestamp.UTC(),
		}
		event.Attributes["request_id"] = event.RequestID
		event.Attributes["latency_ms"] = event.LatencyMS
		if errorCode != "" {
			event.Attributes["error_code"] = errorCode
		}
		if err := encoder.Encode(event); err != nil {
			return fmt.Errorf("encode event: %w", err)
		}
	}
	return nil
}

func exitError(err error) {
	fmt.Fprintln(os.Stderr, "log generator:", err)
	os.Exit(1)
}
