package metrics

import (
	"context"
	"testing"
	"time"
)

type mcpClientStub struct {
	result map[string]any
	err    error
	tool   string
	args   map[string]any
}

func (s *mcpClientStub) CallTool(
	_ context.Context,
	tool string,
	args map[string]any,
) (map[string]any, error) {
	s.tool = tool
	s.args = args
	return s.result, s.err
}

func TestMCPStoreQueriesPrometheusTool(t *testing.T) {
	client := &mcpClientStub{result: map[string]any{
		"samples": []any{map[string]any{
			"name":      "watchops_checkout_error_rate",
			"value":     0.062,
			"timestamp": "2026-06-30T00:20:00Z",
			"service":   "checkout",
			"labels": map[string]any{
				"environment": "demo",
			},
		}},
	}}
	store := NewMCPStore(client)
	at := time.Date(2026, 6, 30, 0, 20, 0, 0, time.UTC)

	samples, err := store.Query(context.Background(), "watchops_checkout_error_rate", at)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if client.tool != "query_prometheus" ||
		client.args["query"] != "watchops_checkout_error_rate" ||
		client.args["time"] != "2026-06-30T00:20:00Z" {
		t.Fatalf("tool=%q args=%#v", client.tool, client.args)
	}
	if len(samples) != 1 ||
		samples[0].Name != "watchops_checkout_error_rate" ||
		samples[0].Value != 0.062 ||
		samples[0].Service != "checkout" ||
		samples[0].Labels["environment"] != "demo" ||
		samples[0].Query != "watchops_checkout_error_rate" {
		t.Fatalf("samples = %#v", samples)
	}
}

func TestMCPStoreParsesPrometheusStyleResult(t *testing.T) {
	store := NewMCPStore(&mcpClientStub{result: map[string]any{
		"data": map[string]any{
			"result": []any{map[string]any{
				"metric": map[string]any{
					"__name__":      "watchops_checkout_error_rate",
					"service":       "checkout",
					"environment":   "demo",
					"response_code": "500",
				},
				"value": []any{float64(1782778800), "0.062"},
			}},
		},
	}})

	samples, err := store.Query(
		context.Background(),
		"watchops_checkout_error_rate",
		time.Date(2026, 6, 30, 0, 20, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(samples) != 1 ||
		samples[0].Service != "checkout" ||
		samples[0].Labels["response_code"] != "500" ||
		samples[0].Value != 0.062 {
		t.Fatalf("samples = %#v", samples)
	}
}
