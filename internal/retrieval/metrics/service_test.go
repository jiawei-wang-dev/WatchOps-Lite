package metrics

import (
	"context"
	"testing"
	"time"
)

type storeStub struct {
	expressions []string
}

func (s *storeStub) Query(
	_ context.Context,
	expression string,
	at time.Time,
) ([]Sample, error) {
	s.expressions = append(s.expressions, expression)
	return []Sample{{
		Value:     0.062,
		Timestamp: at,
		Labels:    map[string]string{"service": "checkout"},
	}}, nil
}

func TestServiceSelectsConfiguredErrorRateQuery(t *testing.T) {
	store := &storeStub{}
	service, err := NewService(store, map[string]string{
		"checkout_error_rate":  "watchops_checkout_error_rate",
		"checkout_p95_latency": "watchops_checkout_p95_latency_seconds",
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	samples, err := service.Query(context.Background(), QueryRequest{
		Service:    "checkout",
		MetricName: "http_server_error_rate",
		At:         time.Date(2026, 6, 30, 0, 20, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(store.expressions) != 1 ||
		store.expressions[0] != "watchops_checkout_error_rate" ||
		len(samples) != 1 ||
		samples[0].Name != "checkout_error_rate" ||
		samples[0].Service != "checkout" ||
		samples[0].Query != "watchops_checkout_error_rate" {
		t.Fatalf("expressions=%#v samples=%#v", store.expressions, samples)
	}
}
