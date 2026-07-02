package evidence

import "testing"

func TestNormalizeAppliesSourceTypeAndTraceID(t *testing.T) {
	item := Normalize(Item{
		ID:      " evidence-1 ",
		Content: " observed timeout ",
		Metadata: map[string]any{
			"trace_id": " trace-1 ",
		},
	}, SourceLogs)

	if item.ID != "evidence-1" || item.Source != SourceLogs ||
		item.Type != TypeLogEvent || item.TraceID != "trace-1" ||
		item.Content != "observed timeout" {
		t.Fatalf("normalized item = %#v", item)
	}
	if err := Validate(item); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestNormalizeAppliesAuxiliarySourceDefaults(t *testing.T) {
	tests := []struct {
		source       Source
		expectedType string
	}{
		{SourceAlerts, TypeAlertSignal},
		{SourceTopology, TypeTopology},
	}

	for _, test := range tests {
		item := Normalize(Item{
			ID:      "evidence-1",
			Content: "auxiliary context",
		}, test.source)
		if item.Source != test.source || item.Type != test.expectedType {
			t.Fatalf("normalized item = %#v, want source=%s type=%s", item, test.source, test.expectedType)
		}
		if err := Validate(item); err != nil {
			t.Fatalf("Validate() error = %v", err)
		}
	}
}

func TestValidateRejectsUnknownSource(t *testing.T) {
	err := Validate(Item{
		ID:      "evidence-1",
		Type:    "observation",
		Source:  Source("unknown"),
		Content: "content",
	})
	if err == nil {
		t.Fatal("expected invalid source error")
	}
}
