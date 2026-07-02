package configs

import (
	"os"
	"strings"
	"testing"
)

func TestPrometheusConfigLoadsDemoAlertRules(t *testing.T) {
	config, err := os.ReadFile("prometheus.yml")
	if err != nil {
		t.Fatalf("read prometheus.yml: %v", err)
	}
	if !strings.Contains(
		string(config),
		"/etc/prometheus/alert_rules.yml",
	) {
		t.Fatal("prometheus.yml does not reference alert_rules.yml")
	}

	rules, err := os.ReadFile("prometheus/alert_rules.yml")
	if err != nil {
		t.Fatalf("read alert_rules.yml: %v", err)
	}
	for _, name := range []string{
		"CheckoutHighErrorRate",
		"CheckoutHighLatency",
		"PaymentTimeoutSpike",
		"RedisLatencyHigh",
	} {
		if !strings.Contains(string(rules), "alert: "+name) {
			t.Fatalf("alert_rules.yml is missing %q", name)
		}
	}
}
