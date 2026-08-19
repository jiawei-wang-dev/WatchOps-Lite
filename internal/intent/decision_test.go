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

func TestValidateSlotsDeterministicRules(t *testing.T) {
	tests := []struct {
		name    string
		message string
		result  IntentResult
		want    DecisionType
		missing string
	}{
		{
			name:    "metrics requires service",
			message: "查错误率",
			result: IntentResult{
				Intent: IntentMetricsQuery, Confidence: 0.8, Source: "test",
			},
			want: DecisionClarify, missing: "service",
		},
		{
			name:    "logs requires service",
			message: "查日志",
			result: IntentResult{
				Intent: IntentLogsQuery, Confidence: 0.8, Source: "test",
			},
			want: DecisionClarify, missing: "service",
		},
		{
			name:    "incident requires service",
			message: "排查故障",
			result: IntentResult{
				Intent: IntentIncidentTriage, Confidence: 0.8, Source: "test",
			},
			want: DecisionClarify, missing: "service",
		},
		{
			name:    "trace id is sufficient",
			message: "分析 trace 4bf92f3577b34da6",
			result: IntentResult{
				Intent: IntentTraceAnalysis, Confidence: 0.9,
				TraceID: "4bf92f3577b34da6", Source: "test",
			},
			want: DecisionProceed,
		},
		{
			name:    "trace service and time are sufficient",
			message: "分析 checkout 昨天的 trace",
			result: IntentResult{
				Intent: IntentTraceAnalysis, Confidence: 0.9,
				Service:   "checkout",
				TimeRange: &TimeRangeHint{Relative: "yesterday"},
				Source:    "test",
			},
			want: DecisionProceed,
		},
		{
			name:    "trace without either alternative clarifies",
			message: "分析 trace",
			result: IntentResult{
				Intent: IntentTraceAnalysis, Confidence: 0.9, Source: "test",
			},
			want: DecisionClarify, missing: "trace_id",
		},
		{
			name:    "knowledge topic proceeds without service",
			message: "找一下超时处理手册",
			result: IntentResult{
				Intent: IntentKnowledgeQuery, Confidence: 0.8, Source: "test",
			},
			want: DecisionProceed,
		},
		{
			name:    "general chat proceeds without diagnostic slots",
			message: "你好",
			result: IntentResult{
				Intent: IntentGeneralChat, Confidence: 0.8, Source: "test",
			},
			want: DecisionProceed,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := ValidateSlots(
				test.message,
				test.result,
				nil,
				FocusView{},
				0.55,
			)
			if decision.Decision != test.want {
				t.Fatalf("decision = %#v, want %s", decision, test.want)
			}
			if test.missing != "" &&
				!containsString(decision.MissingRequired, test.missing) {
				t.Fatalf("missing slots = %#v, want %q", decision.MissingRequired, test.missing)
			}
		})
	}
}

func TestValidateSlotsPrecedenceAndNoUnsafeDefaults(t *testing.T) {
	focus := FocusView{
		Available: true,
		KnownSlots: map[string]string{
			"service": "checkout", "time_range": "last_week",
		},
	}
	decision := ValidateSlots(
		"换成 payment，查昨天",
		IntentResult{
			Intent: IntentMetricsQuery, Confidence: 0.8,
			Service:   "payment",
			TimeRange: &TimeRangeHint{Relative: "yesterday"},
			Source:    "test",
		},
		map[string]string{
			"service": "orders", "time_range": "today",
		},
		focus,
		0.55,
	)
	if decision.KnownSlots["service"] != "payment" ||
		decision.KnownSlots["time_range"] != "yesterday" {
		t.Fatalf("current input did not win precedence: %#v", decision)
	}

	structured := ValidateSlots(
		"继续查指标",
		IntentResult{
			Intent: IntentMetricsQuery, Confidence: 0.8, Source: "test",
		},
		map[string]string{
			"service": "orders", "time_range": "today",
		},
		focus,
		0.55,
	)
	if structured.KnownSlots["service"] != "orders" ||
		structured.KnownSlots["time_range"] != "today" {
		t.Fatalf("structured slots did not override focus: %#v", structured)
	}
	for _, forbidden := range []string{
		"trace_id", "identity", "tenant", "environment",
	} {
		if structured.DefaultsApplied[forbidden] != "" {
			t.Fatalf("unsafe default %q was applied: %#v", forbidden, structured)
		}
	}
}

func TestValidateSlotsAllowsDirectedCrossServiceDependency(t *testing.T) {
	decision := ValidateSlots(
		"checkout 是否被 payment 依赖拖慢",
		IntentResult{
			Intent: IntentIncidentTriage, Confidence: 0.9,
			Service: "checkout", Symptom: "latency", Source: "test",
		},
		nil,
		FocusView{},
		0.55,
	)
	if decision.Decision != DecisionProceed || decision.KnownSlots["service"] != "checkout" {
		t.Fatalf("decision = %#v", decision)
	}
}
