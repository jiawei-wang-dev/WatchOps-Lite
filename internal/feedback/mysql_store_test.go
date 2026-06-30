package feedback

import (
	"reflect"
	"testing"
)

func TestFeedbackJSONEncodingRoundTrip(t *testing.T) {
	input := []map[string]any{{
		"tool":        "query_metrics",
		"success":     true,
		"duration_ms": float64(35),
	}}
	encoded, err := encodeJSON(input)
	if err != nil {
		t.Fatalf("encodeJSON() error = %v", err)
	}
	var decoded []map[string]any
	if err := decodeJSON(encoded, &decoded); err != nil {
		t.Fatalf("decodeJSON() error = %v", err)
	}
	if !reflect.DeepEqual(decoded, input) {
		t.Fatalf("decoded = %#v, want %#v", decoded, input)
	}
}
