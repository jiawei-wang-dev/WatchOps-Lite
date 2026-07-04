package knowledge

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestContentHashNormalizesWhitespaceHeadingsAndASCIICase(t *testing.T) {
	first := "# Checkout Runbook\r\n\r\n检查  payment   超时。\n"
	second := "## checkout runbook\n检查 payment 超时。"

	if ContentHash(first) != ContentHash(second) {
		t.Fatalf("equivalent content produced different hashes")
	}
}

func TestContentHashUsesCompleteContentNotOnlyTitle(t *testing.T) {
	first := "# 相同标题\n检查 payment 超时。"
	second := "# 相同标题\n检查 Redis 连接池。"

	if ContentHash(first) == ContentHash(second) {
		t.Fatalf("different content produced the same hash")
	}
}

func TestContentHashPreservesChineseText(t *testing.T) {
	normalized := normalizeKnowledgeContent("# 排障手册\n检查支付依赖超时和重试放大。")
	if !utf8.ValidString(normalized) ||
		!strings.Contains(normalized, "支付依赖超时") {
		t.Fatalf("normalized Chinese content = %q", normalized)
	}
	if len(ContentHash(normalized)) != 64 {
		t.Fatalf("SHA-256 hash has unexpected length")
	}
}
