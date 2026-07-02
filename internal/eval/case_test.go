package eval

import "testing"

func TestBadCaseReasonsUseStableMetadataValues(t *testing.T) {
	reasons := []BadCaseReason{
		BadCaseReasonNoEvidence,
		BadCaseReasonWrongTool,
		BadCaseReasonIrrelevantAnswer,
		BadCaseReasonWrongRootCause,
		BadCaseReasonPoorFormat,
		BadCaseReasonMissingUncertainty,
	}
	seen := make(map[BadCaseReason]struct{}, len(reasons))
	for _, reason := range reasons {
		if reason == "" {
			t.Fatal("bad case reason must not be empty")
		}
		if _, exists := seen[reason]; exists {
			t.Fatalf("duplicate bad case reason %q", reason)
		}
		seen[reason] = struct{}{}
	}
}
