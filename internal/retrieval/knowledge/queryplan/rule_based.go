package queryplan

import (
	"context"
	"strings"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

type RuleBasedPlanner struct{}

func NewRuleBasedPlanner() *RuleBasedPlanner {
	return &RuleBasedPlanner{}
}

func (p *RuleBasedPlanner) Plan(
	ctx context.Context,
	input QueryPlanInput,
) (RAGQueryPlan, error) {
	ctx, span := observability.StartSpan(
		ctx,
		"rag.query_plan.rule",
		attribute.String("intent.type", string(input.Intent.Intent)),
	)
	defer span.End()

	original := strings.TrimSpace(input.UserMessage)
	service := firstNonEmpty(input.Service, input.Intent.Service, serviceFromText(original))
	symptom := firstNonEmpty(input.Symptom, input.Intent.Symptom)
	keywords := append([]string{}, input.Keywords...)
	keywords = append(keywords, input.Intent.Keywords...)
	queries := []RAGSubQuery{{
		Type:   QueryOriginal,
		Query:  original,
		Weight: 1.0,
		Reason: "preserve the user's original wording",
	}}
	if canonical := canonicalQuery(input.Intent.Intent, original, service, symptom); canonical != "" {
		queries = append(queries, RAGSubQuery{
			Type:   QueryCanonical,
			Query:  canonical,
			Weight: 0.9,
			Reason: "normalize user wording into an on-call retrieval query",
		})
	}
	if synonym := synonymExpansion(original, symptom, keywords); synonym != "" {
		queries = append(queries, RAGSubQuery{
			Type:   QuerySynonym,
			Query:  synonym,
			Weight: 0.75,
			Reason: "expand diagnostic synonyms for BM25 and vector recall",
		})
	}
	if diagnostic := diagnosticQuery(service, symptom); diagnostic != "" {
		queries = append(queries, RAGSubQuery{
			Type:   QueryDiagnostic,
			Query:  diagnostic,
			Weight: 0.8,
			Reason: "retrieve possible root cause directions",
		})
	}
	if stepBack := stepBackQuery(input.Intent.Intent, symptom); stepBack != "" {
		queries = append(queries, RAGSubQuery{
			Type:   QueryStepBack,
			Query:  stepBack,
			Weight: 0.65,
			Reason: "retrieve general failure patterns and mitigation steps",
		})
	}
	queries = normalizeQueries(queries)
	if len(queries) == 0 && original != "" {
		queries = []RAGSubQuery{{Type: QueryOriginal, Query: original, Weight: 1}}
	}
	span.SetAttributes(
		attribute.Int("rag.sub_query_count", len(queries)),
		attribute.StringSlice("rag.sub_query_types", queryTypes(queries)),
	)
	return RAGQueryPlan{
		OriginalQuery: original,
		Queries:       queries,
		Source:        "rule",
		Metadata: map[string]any{
			"intent_type":           string(input.Intent.Intent),
			"service":               service,
			"symptom":               symptom,
			"query_rewrite_applied": len(queries) > 1,
		},
	}, nil
}

func canonicalQuery(intentType intent.IntentType, original string, service string, symptom string) string {
	switch intentType {
	case intent.IntentIncidentTriage:
		return strings.TrimSpace(strings.Join([]string{
			defaultService(service),
			canonicalSymptom(symptom, original),
			"root cause runbook incident mitigation",
		}, " "))
	case intent.IntentKnowledgeQuery, intent.IntentMitigation:
		return strings.TrimSpace(strings.Join([]string{
			defaultService(service),
			canonicalSymptom(symptom, original),
			"runbook playbook mitigation steps",
		}, " "))
	case intent.IntentTraceAnalysis:
		return strings.TrimSpace(strings.Join([]string{
			defaultService(service),
			"trace slow span dependency bottleneck observability runbook",
		}, " "))
	case intent.IntentMetricsQuery:
		return strings.TrimSpace(strings.Join([]string{
			defaultService(service),
			"metrics error rate latency saturation dashboard runbook",
		}, " "))
	case intent.IntentLogsQuery:
		return strings.TrimSpace(strings.Join([]string{
			defaultService(service),
			"logs errors exceptions timeout stack trace runbook",
		}, " "))
	default:
		return ""
	}
}

func synonymExpansion(original string, symptom string, keywords []string) string {
	text := strings.ToLower(strings.Join(append([]string{original, symptom}, keywords...), " "))
	switch {
	case containsAny(text, "500", "5xx", "error", "failure", "失败", "报错"):
		return "server error failure exception timeout dependency error retry amplification"
	case containsAny(text, "latency", "slow", "p95", "timeout", "超时", "慢"):
		return "latency slow timeout deadline downstream dependency saturation connection pool"
	case containsAny(text, "trace", "span", "链路"):
		return "trace span critical path slow operation dependency bottleneck"
	case containsAny(text, "runbook", "playbook", "文档"):
		return "runbook playbook mitigation known incident operating procedure"
	default:
		return ""
	}
}

func diagnosticQuery(service string, symptom string) string {
	base := defaultService(service)
	switch symptom {
	case "timeout":
		return strings.TrimSpace(base + " upstream dependency timeout database connection pool retry amplification")
	case "latency":
		return strings.TrimSpace(base + " slow downstream dependency database bottleneck connection pool external API latency")
	case "exception":
		return strings.TrimSpace(base + " panic exception stack configuration error dependency unavailable")
	case "error":
		return strings.TrimSpace(base + " HTTP 5xx errors upstream dependency timeout database failure deployment regression")
	default:
		if base != "" {
			return strings.TrimSpace(base + " incident root cause dependency database deployment traffic overload")
		}
		return ""
	}
}

func stepBackQuery(intentType intent.IntentType, symptom string) string {
	switch {
	case intentType == intent.IntentTraceAnalysis:
		return "common causes and mitigation steps for slow distributed trace spans"
	case symptom == "latency":
		return "common causes and mitigation steps for sudden service latency increase"
	case symptom == "timeout":
		return "common causes and mitigation steps for upstream timeout incidents"
	case symptom == "error" || intentType == intent.IntentIncidentTriage:
		return "common causes and mitigation steps for sudden increase in HTTP 5xx errors"
	default:
		return ""
	}
}

func normalizeQueries(queries []RAGSubQuery) []RAGSubQuery {
	result := make([]RAGSubQuery, 0, len(queries))
	seen := map[string]struct{}{}
	for _, query := range queries {
		query.Query = strings.TrimSpace(query.Query)
		if query.Query == "" {
			continue
		}
		if query.Weight <= 0 {
			query.Weight = 0.5
		}
		if query.Weight > 1 {
			query.Weight = 1
		}
		key := strings.ToLower(query.Query)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, query)
	}
	return result
}

func queryTypes(queries []RAGSubQuery) []string {
	types := make([]string, 0, len(queries))
	for _, query := range queries {
		types = append(types, string(query.Type))
	}
	return types
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func defaultService(service string) string {
	if strings.TrimSpace(service) == "" {
		return ""
	}
	return strings.TrimSpace(service)
}

func canonicalSymptom(symptom string, original string) string {
	if symptom != "" {
		return symptom
	}
	if containsAny(strings.ToLower(original), "500", "5xx") {
		return "HTTP 5xx error rate increase"
	}
	return "service reliability incident"
}

func serviceFromText(value string) string {
	for _, token := range strings.Fields(value) {
		token = strings.Trim(token, ".,;:()[]{}")
		lower := strings.ToLower(token)
		if strings.Contains(lower, "checkout") {
			return "checkout-service"
		}
		if strings.Contains(lower, "payment") {
			return "payment-service"
		}
		if strings.HasSuffix(lower, "-service") || strings.HasSuffix(lower, "_service") {
			return token
		}
	}
	return ""
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, strings.ToLower(candidate)) {
			return true
		}
	}
	return false
}
