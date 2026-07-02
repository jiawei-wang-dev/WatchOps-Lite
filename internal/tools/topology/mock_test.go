package topology

import (
	"context"
	"testing"
)

func TestMockToolSuccess(t *testing.T) {
	result, err := NewMockTool(0).Execute(context.Background(), Input{
		Service: "checkout",
		Depth:   1,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success || result.Tool != Name || len(result.Evidence) != 1 {
		t.Fatalf("result = %#v, want one successful topology evidence item", result)
	}
	metadata := result.Evidence[0].Metadata
	if result.Evidence[0].SourceType != "topology" ||
		metadata["service"] != "checkout" ||
		len(metadata["dependencies"].([]string)) == 0 {
		t.Fatalf("evidence = %#v", result.Evidence[0])
	}
}

func TestMockToolRejectsInvalidDepth(t *testing.T) {
	_, err := NewMockTool(0).Execute(context.Background(), Input{
		Service: "checkout",
		Depth:   4,
	})
	if err == nil {
		t.Fatal("expected invalid depth error")
	}
}
