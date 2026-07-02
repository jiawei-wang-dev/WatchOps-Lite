package alerts

import (
	"context"
	"testing"
)

func TestMockToolSuccess(t *testing.T) {
	result, err := NewMockTool(0).Execute(context.Background(), Input{
		Service:  "checkout",
		Severity: "warning",
		Window:   "30m",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success || result.Tool != Name || len(result.Evidence) != 1 {
		t.Fatalf("result = %#v, want one successful alert evidence item", result)
	}
	if result.Evidence[0].SourceType != "alerts" ||
		result.Evidence[0].Metadata["alert_name"] != "CheckoutHighErrorRate" {
		t.Fatalf("evidence = %#v", result.Evidence[0])
	}
}
