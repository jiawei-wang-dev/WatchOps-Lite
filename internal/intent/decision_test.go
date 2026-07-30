package intent

import "testing"

func TestValidateSlotsClarifiesMissingService(t *testing.T) {
	decision := ValidateSlots("查错误率", IntentResult{
		Intent: IntentMetricsQuery, Confidence: 0.8, Source: "test",
	}, nil, FocusView{}, 0.55)
	if decision.Decision != DecisionClarify ||
		!containsString(decision.MissingRequired, "service") {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestValidateSlotsCurrentServiceOverridesFocus(t *testing.T) {
	decision := ValidateSlots("换成 payment", IntentResult{
		Intent: IntentIncidentTriage, Confidence: 0.8,
		Service: "payment", Source: "test",
	}, nil, FocusView{
		Available:  true,
		KnownSlots: map[string]string{"service": "checkout"},
	}, 0.55)
	if decision.Decision != DecisionProceed ||
		decision.KnownSlots["service"] != "payment" {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestValidateSlotsTraceRequiresAlternativeGroup(t *testing.T) {
	decision := ValidateSlots("分析 trace", IntentResult{
		Intent: IntentTraceAnalysis, Confidence: 0.9, Source: "test",
	}, nil, FocusView{}, 0.55)
	if decision.Decision != DecisionClarify ||
		decision.ReasonCode != "MISSING_REQUIRED_SLOT" {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestValidateSlotsGeneralChatNeedsNoService(t *testing.T) {
	decision := ValidateSlots("你好", IntentResult{
		Intent: IntentGeneralChat, Confidence: 0.5, Source: "test",
	}, nil, FocusView{}, 0.55)
	if decision.Decision != DecisionProceed {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestValidateSlotsStructuredTimeOverridesFocusUnlessCurrentIsExplicit(t *testing.T) {
	focus := FocusView{
		Available: true,
		KnownSlots: map[string]string{
			"service": "checkout", "time_range": "last_10_minutes",
		},
	}
	decision := ValidateSlots("就刚才那个", IntentResult{
		Intent: IntentMetricsQuery, Confidence: 0.8, Source: "test",
		TimeRange: &TimeRangeHint{Relative: "last_10_minutes"},
	}, map[string]string{"time_range": "command-range"}, focus, 0.55)
	if decision.KnownSlots["time_range"] != "command-range" {
		t.Fatalf("decision = %#v", decision)
	}
	decision = ValidateSlots("换成昨天", IntentResult{
		Intent: IntentMetricsQuery, Confidence: 0.8, Source: "test",
		TimeRange: &TimeRangeHint{Relative: "yesterday"},
	}, map[string]string{"time_range": "command-range"}, focus, 0.55)
	if decision.KnownSlots["time_range"] != "yesterday" {
		t.Fatalf("decision = %#v", decision)
	}
}
