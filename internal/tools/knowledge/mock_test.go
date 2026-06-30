package knowledge

import (
	"context"
	"testing"
)

func TestMockToolSuccess(t *testing.T) {
	result, err := NewMockTool(0).Execute(context.Background(), Input{
		Query:    "checkout timeout runbook",
		TopK:     3,
		Category: "runbook",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success || result.Tool != Name || len(result.Evidence) != 1 {
		t.Fatalf("result = %#v, want one successful knowledge evidence item", result)
	}
}
