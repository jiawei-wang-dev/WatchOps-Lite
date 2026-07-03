package demo

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestChineseLogFixtureIsUTF8JSONLWithStableIdentifiers(t *testing.T) {
	file, err := os.Open("logs/checkout_logs_zh.jsonl")
	if err != nil {
		t.Fatalf("open Chinese logs: %v", err)
	}
	defer file.Close()

	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		count++
		line := scanner.Bytes()
		if !utf8.Valid(line) {
			t.Fatalf("line %d is not valid UTF-8", count)
		}
		var event struct {
			ID        string `json:"id"`
			Service   string `json:"service"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
			TraceID   string `json:"trace_id"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("line %d is not valid JSON: %v", count, err)
		}
		if event.Service != "checkout" ||
			!strings.HasPrefix(event.ID, "log_checkout_zh_") ||
			!strings.HasPrefix(event.RequestID, "req-checkout-zh-") ||
			!strings.HasPrefix(event.TraceID, "trace-checkout-zh-") {
			t.Fatalf("line %d has unstable identifiers: %#v", count, event)
		}
		if !strings.ContainsAny(event.Message, "支付调用请求检测恢复超时重试") {
			t.Fatalf("line %d does not contain Chinese display text", count)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan Chinese logs: %v", err)
	}
	if count < 6 {
		t.Fatalf("Chinese log count = %d, want at least 6", count)
	}
}

func TestChineseRunbookIsUTF8AndBilingual(t *testing.T) {
	content, err := os.ReadFile("knowledge/checkout_runbook_zh.md")
	if err != nil {
		t.Fatalf("read Chinese runbook: %v", err)
	}
	if !utf8.Valid(content) {
		t.Fatal("Chinese runbook is not valid UTF-8")
	}
	text := string(content)
	for _, expected := range []string{
		"checkout",
		"payment",
		"支付依赖",
		"错误率",
		"重试放大",
		"retry amplification",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("Chinese runbook does not contain %q", expected)
		}
	}
}

func TestEnglishDemoFixturesRemainAvailable(t *testing.T) {
	for _, path := range []string{
		"knowledge/checkout_runbook.md",
		"logs/checkout_logs.jsonl",
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("English fixture %s is unavailable: %v", path, err)
		}
	}
}
