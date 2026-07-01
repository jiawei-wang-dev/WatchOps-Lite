package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsHandlerExposesCheckoutMetrics(t *testing.T) {
	recorder := httptest.NewRecorder()
	metricsHandler(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	for _, metricName := range []string{
		"watchops_checkout_error_rate",
		"watchops_checkout_p95_latency_seconds",
		"watchops_payment_dependency_latency_seconds",
		"watchops_checkout_timeout_total",
	} {
		if !strings.Contains(recorder.Body.String(), metricName) {
			t.Fatalf("response does not contain %q", metricName)
		}
	}
}
