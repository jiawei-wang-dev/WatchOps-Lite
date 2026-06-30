package eval

import (
	"reflect"
	"testing"
)

func TestEvalJSONEncodingRoundTrip(t *testing.T) {
	input := []string{"Do not claim root cause without evidence."}
	encoded, err := encodeJSON(input)
	if err != nil {
		t.Fatalf("encodeJSON() error = %v", err)
	}
	var decoded []string
	if err := decodeJSON(encoded, &decoded); err != nil {
		t.Fatalf("decodeJSON() error = %v", err)
	}
	if !reflect.DeepEqual(decoded, input) {
		t.Fatalf("decoded = %#v, want %#v", decoded, input)
	}
}
