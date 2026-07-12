package eino

import (
	"reflect"
	"testing"
)

func TestValidEvidenceIDsFiltersInvalidBlankAndDuplicateIDs(t *testing.T) {
	allowed := map[string]struct{}{
		"ev-1": {},
		"ev-2": {},
	}
	tests := []struct {
		name        string
		input       []string
		want        []string
		wantInvalid bool
	}{
		{
			name:        "valid IDs are preserved",
			input:       []string{"ev-1", "ev-2"},
			want:        []string{"ev-1", "ev-2"},
			wantInvalid: false,
		},
		{
			name:        "invalid IDs are removed",
			input:       []string{"invented"},
			want:        []string{},
			wantInvalid: true,
		},
		{
			name:        "mixed valid and invalid IDs keep only valid IDs",
			input:       []string{"ev-1", "invented", "ev-2"},
			want:        []string{"ev-1", "ev-2"},
			wantInvalid: true,
		},
		{
			name:        "blank IDs are invalid",
			input:       []string{" ", "ev-1"},
			want:        []string{"ev-1"},
			wantInvalid: true,
		},
		{
			name:        "duplicate IDs are deduplicated",
			input:       []string{"ev-1", "ev-1", "ev-2"},
			want:        []string{"ev-1", "ev-2"},
			wantInvalid: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, invalid := validEvidenceIDs(test.input, allowed)
			if !reflect.DeepEqual(got, test.want) || invalid != test.wantInvalid {
				t.Fatalf("validEvidenceIDs() = %#v, %v; want %#v, %v",
					got,
					invalid,
					test.want,
					test.wantInvalid,
				)
			}
		})
	}
}
