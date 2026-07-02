package alerts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	retrievalmetrics "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/metrics"
	toolruntime "github.com/jiawei-wang-dev/WatchOps-Lite/internal/tool/runtime"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
	"go.opentelemetry.io/otel/attribute"
)

type Store interface {
	Query(context.Context, string, time.Time) ([]retrievalmetrics.Sample, error)
}

type SearchToolConfig struct {
	Backend        string
	BaseURL        string
	FallbackToMock bool
	Timeout        time.Duration
}

type SearchTool struct {
	store   Store
	config  SearchToolConfig
	runtime *toolruntime.Runtime
}

func NewSearchTool(store Store, config SearchToolConfig) *SearchTool {
	tool := &SearchTool{store: store, config: config}
	backend := strings.ToLower(strings.TrimSpace(config.Backend))
	if backend == "" {
		backend = "prometheus"
	}
	runtimeConfig := toolruntime.Config{
		ToolName:   Name,
		SourceType: toolruntime.SourceAlerts,
		Timeout:    config.Timeout,
		Operation: func(ctx context.Context, value any) (toolruntime.Result, error) {
			input, ok := value.(Input)
			if !ok {
				return toolruntime.Result{}, invalidArgument("invalid alerts tool input", nil)
			}
			return tool.search(ctx, input)
		},
	}
	if config.FallbackToMock {
		runtimeConfig.Fallback = mockOperation
		runtimeConfig.FallbackWarning = toolruntime.Warning{
			Code:    "ALERTS_FALLBACK",
			Message: "Prometheus ALERTS data was unavailable; mock alert evidence was returned.",
		}
		runtimeConfig.FallbackMetadata = map[string]any{
			"mode":               "mock_fallback",
			"backend":            "mock",
			"configured_backend": backend,
		}
	}
	tool.runtime = mustRuntime(runtimeConfig)
	return tool
}

func (t *SearchTool) Execute(ctx context.Context, input Input) (common.ToolResult, error) {
	return common.ExecuteRuntime(ctx, t.runtime, input)
}

func (t *SearchTool) Runtime() *toolruntime.Runtime {
	return t.runtime
}

func (t *SearchTool) search(
	ctx context.Context,
	input Input,
) (result toolruntime.Result, resultErr error) {
	backend := strings.ToLower(strings.TrimSpace(t.config.Backend))
	if backend == "" {
		backend = "prometheus"
	}
	ctx, span := observability.StartSpan(
		ctx,
		"alerts.query",
		attribute.String("alerts.backend", backend),
		attribute.String("prometheus.base_url", t.config.BaseURL),
		attribute.String("service", strings.TrimSpace(input.Service)),
	)
	defer func() {
		span.SetAttributes(attribute.Int("result_count", len(result.Evidence)))
		if resultErr != nil {
			observability.MarkError(span, "Alerts query failed")
		}
		span.End()
	}()

	if toolErr := validate(input); toolErr != nil {
		return toolruntime.Result{}, toolErr
	}
	if t.store == nil {
		return toolruntime.Result{}, dependencyUnavailable()
	}
	samples, err := t.store.Query(ctx, alertsExpression(input), time.Now().UTC())
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return toolruntime.Result{}, err
		}
		return toolruntime.Result{}, dependencyUnavailable()
	}

	evidenceItems := make([]evidence.Item, 0, len(samples))
	for index, sample := range samples {
		if sample.Value == 0 {
			continue
		}
		service := sample.Service
		if service == "" {
			service = strings.TrimSpace(input.Service)
		}
		severity := sample.Labels["severity"]
		status := sample.Labels["alertstate"]
		if status == "" {
			status = "firing"
		}
		alertName := sample.Labels["alertname"]
		if alertName == "" {
			alertName = sample.Name
		}
		if alertName == "" {
			alertName = "ALERTS"
		}
		evidenceItems = append(evidenceItems, evidence.Item{
			ID:         fmt.Sprintf("prometheus-alert-%d", index+1),
			Type:       evidence.TypeAlertSignal,
			Source:     evidence.SourceAlerts,
			SourceName: "prometheus-alerts",
			Content: fmt.Sprintf(
				"Prometheus alert %s is %s for service %s.",
				alertName,
				status,
				service,
			),
			ResourceID: service,
			Metadata: map[string]any{
				"alert_name":  alertName,
				"service":     service,
				"severity":    severity,
				"status":      status,
				"starts_at":   sample.Timestamp.UTC().Format(time.RFC3339Nano),
				"labels":      sample.Labels,
				"annotations": map[string]string{},
				"summary":     sample.Labels["summary"],
			},
		})
	}
	warnings := []toolruntime.Warning{}
	if len(evidenceItems) == 0 {
		warnings = append(warnings, toolruntime.Warning{
			Code:    "ALERTS_NO_DATA",
			Message: "Prometheus returned no active alert samples for the selected service.",
		})
	}
	return toolruntime.Result{
		Evidence: evidenceItems,
		Warnings: warnings,
		Payload: map[string]any{
			"returned_count": len(evidenceItems),
		},
		Metadata: map[string]any{
			"mode":    "prometheus",
			"backend": backend,
		},
	}, nil
}

func alertsExpression(input Input) string {
	service := strings.TrimSpace(input.Service)
	filters := []string{fmt.Sprintf(`service="%s"`, escapeLabelValue(service))}
	if severity := strings.TrimSpace(input.Severity); severity != "" {
		filters = append(filters, fmt.Sprintf(`severity="%s"`, escapeLabelValue(severity)))
	}
	return "ALERTS{" + strings.Join(filters, ",") + "}"
}

func escapeLabelValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func dependencyUnavailable() *toolruntime.ToolError {
	return toolruntime.NewToolError(
		toolruntime.ErrorCodeDependencyUnavailable,
		Name,
		"Prometheus alerts backend is unavailable",
		true,
		map[string]any{"backend": "prometheus"},
	)
}
