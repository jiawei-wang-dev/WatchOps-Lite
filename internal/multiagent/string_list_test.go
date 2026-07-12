package multiagent

import (
	"encoding/json"
	"testing"
)

func TestStringListUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "array", raw: `["restart service","check dependency health"]`, want: []string{"restart service", "check dependency health"}},
		{name: "single string", raw: `"restart service"`, want: []string{"restart service"}},
		{name: "empty string", raw: `""`, want: []string{}},
		{name: "null", raw: `null`, want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got StringList
			if err := json.Unmarshal([]byte(tt.raw), &got); err != nil {
				t.Fatalf("UnmarshalJSON() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
			for index := range tt.want {
				if got[index] != tt.want[index] {
					t.Fatalf("got %#v, want %#v", got, tt.want)
				}
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("MarshalJSON() error = %v", err)
			}
			if len(encoded) == 0 || encoded[0] != '[' {
				t.Fatalf("MarshalJSON() = %s, want JSON array", encoded)
			}
		})
	}
}

func TestStringListRejectsInvalidTypes(t *testing.T) {
	for _, raw := range []string{`123`, `{"action":"restart"}`} {
		var got StringList
		if err := json.Unmarshal([]byte(raw), &got); err == nil {
			t.Fatalf("UnmarshalJSON(%s) error = nil, want invalid type", raw)
		}
	}
}
