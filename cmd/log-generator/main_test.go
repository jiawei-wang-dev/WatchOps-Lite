package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestGenerateProducesValidScenarioJSONL(t *testing.T) {
	var output bytes.Buffer
	config := generatorConfig{
		Scenario: "checkout-timeout",
		Count:    8,
		Seed:     42,
		Now:      time.Date(2026, 7, 3, 2, 0, 0, 0, time.UTC),
	}
	if err := generate(&output, config); err != nil {
		t.Fatalf("generate() error = %v", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(output.String()))
	count := 0
	for scanner.Scan() {
		var event generatedEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		if event.ID == "" ||
			event.Service != "checkout" ||
			event.Timestamp.IsZero() ||
			event.RequestID == "" ||
			event.TraceID == "" ||
			event.SpanID == "" ||
			event.Endpoint != "/checkout" ||
			event.LatencyMS <= 0 {
			t.Fatalf("event = %#v", event)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan output: %v", err)
	}
	if count != config.Count {
		t.Fatalf("event count = %d, want %d", count, config.Count)
	}
}

func TestGenerateIsDeterministicWithSeedAndNow(t *testing.T) {
	config := generatorConfig{
		Scenario: "payment-errors",
		Count:    12,
		Seed:     99,
		Now:      time.Date(2026, 7, 3, 2, 0, 0, 0, time.UTC),
	}
	var first, second bytes.Buffer
	if err := generate(&first, config); err != nil {
		t.Fatalf("first generate() error = %v", err)
	}
	if err := generate(&second, config); err != nil {
		t.Fatalf("second generate() error = %v", err)
	}
	if first.String() != second.String() {
		t.Fatal("same seed and now produced different output")
	}
}

func TestGenerateRejectsUnsupportedInput(t *testing.T) {
	tests := []generatorConfig{
		{Scenario: "unknown", Count: 1, Now: time.Now()},
		{Scenario: "redis-latency", Count: 0, Now: time.Now()},
	}
	for _, config := range tests {
		if err := generate(&bytes.Buffer{}, config); err == nil {
			t.Fatalf("generate(%#v) error = nil", config)
		}
	}
}
