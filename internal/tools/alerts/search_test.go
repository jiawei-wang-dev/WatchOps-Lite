package alerts

import (
	"context"
	"testing"
	"time"

	retrievalmetrics "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/metrics"
)

type storeStub struct {
	expression string
	err        error
}

func (s *storeStub) Query(
	_ context.Context,
	expression string,
	at time.Time,
) ([]retrievalmetrics.Sample, error) {
	s.expression = expression
	if s.err != nil {
		return nil, s.err
	}
	return []retrievalmetrics.Sample{{
		Name:      "ALERTS",
		Value:     1,
		Timestamp: at,
		Service:   "checkout",
		Labels: map[string]string{
			"alertname":  "CheckoutHighErrorRate",
			"alertstate": "firing",
			"service":    "checkout",
			"severity":   "critical",
		},
	}}, nil
}

func TestSearchToolMapsPrometheusAlertsToEvidence(t *testing.T) {
	store := &storeStub{}
	result, err := NewSearchTool(store, SearchToolConfig{
		Backend:        "prometheus",
		BaseURL:        "http://prometheus:9090",
		FallbackToMock: true,
	}).Execute(context.Background(), Input{
		Service:  "checkout",
		Severity: "critical",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if store.expression != `ALERTS{service="checkout",severity="critical"}` {
		t.Fatalf("expression = %q", store.expression)
	}
	if len(result.Evidence) != 1 ||
		result.Evidence[0].SourceType != "alerts" ||
		result.Evidence[0].SourceName != "prometheus-alerts" ||
		result.Evidence[0].Metadata["status"] != "firing" {
		t.Fatalf("result = %#v", result)
	}
}

func TestSearchToolFallsBackToMockAlerts(t *testing.T) {
	result, err := NewSearchTool(nil, SearchToolConfig{
		Backend:        "prometheus",
		FallbackToMock: true,
	}).Execute(context.Background(), Input{Service: "checkout"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(result.Evidence) != 1 ||
		result.Evidence[0].SourceName != "mock-alerts" ||
		result.Metadata["fallback_used"] != true {
		t.Fatalf("result = %#v", result)
	}
}
