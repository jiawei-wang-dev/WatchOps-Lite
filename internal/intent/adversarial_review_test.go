package intent

import (
	"context"
	"testing"
	"time"
)

func TestAdversarialCurrentIntentAndTimeRange(t *testing.T) {
	tests := []struct {
		message string
		intent  IntentType
		service string
		time    string
	}{
		{"checkout 没报错，我只是想找文档", IntentKnowledgeQuery, "checkout", ""},
		{"不用查日志，只看 trace", IntentTraceAnalysis, "", ""},
		{"payment 已经恢复了，总结刚才的过程", IntentStatusSummary, "payment", ""},
		{"ckout 最近寄了", IntentGeneralChat, "", ""},
		{"为什么昨天慢？先看今天的指标", IntentMetricsQuery, "", "today"},
		{"看最近十分钟 checkout 指标", IntentMetricsQuery, "checkout", "last_10_minutes"},
		{"查 checkout 上周的指标", IntentMetricsQuery, "checkout", "last_week"},
	}
	recognizer := NewRuleBasedRecognizer()
	for _, test := range tests {
		t.Run(test.message, func(t *testing.T) {
			result, err := recognizer.Recognize(context.Background(), RecognitionInput{
				Message: test.message,
				Now:     time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC),
			})
			if err != nil {
				t.Fatalf("Recognize() error = %v", err)
			}
			if result.Intent != test.intent || result.Service != test.service {
				t.Fatalf("result = %#v", result)
			}
			actualTime := ""
			if result.TimeRange != nil {
				actualTime = result.TimeRange.Relative
			}
			if actualTime != test.time {
				t.Fatalf("time range = %q, want %q; result=%#v", actualTime, test.time, result)
			}
		})
	}
}

func TestAdversarialReferenceResolutionAndExpiry(t *testing.T) {
	recognizer := NewRuleBasedRecognizer()
	focus := FocusView{
		Available:  true,
		LastIntent: string(IntentIncidentTriage),
		KnownSlots: map[string]string{},
		Candidates: []string{"checkout", "payment"},
	}
	for _, test := range []struct {
		message string
		service string
	}{
		{"第二个", "payment"},
		{"算了，还是第一个", "checkout"},
	} {
		result, err := recognizer.Recognize(context.Background(), RecognitionInput{
			Message: test.message, Focus: focus,
		})
		if err != nil || result.Service != test.service {
			t.Fatalf("message=%q result=%#v error=%v", test.message, result, err)
		}
	}

	result, err := recognizer.Recognize(context.Background(), RecognitionInput{
		Message: "就刚才那个",
	})
	if err != nil {
		t.Fatalf("Recognize() error = %v", err)
	}
	decision := ValidateSlots("就刚才那个", result, nil, FocusView{}, 0.55)
	if decision.Decision != DecisionClarify ||
		decision.ReasonCode != "AMBIGUOUS_REFERENCE" {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestAdversarialMultiTurnContinuity(t *testing.T) {
	recognizer := NewRuleBasedRecognizer()
	focus := FocusView{KnownSlots: map[string]string{}}
	runTurn := func(message string) IntentDecision {
		result, err := recognizer.Recognize(context.Background(), RecognitionInput{
			Message: message, Focus: focus,
		})
		if err != nil {
			t.Fatalf("Recognize(%q) error = %v", message, err)
		}
		decision := ValidateSlots(message, result, nil, focus, 0.55)
		focus.Available = true
		focus.LastIntent = string(decision.Result.Intent)
		focus.KnownSlots = cloneStringMapForTest(decision.KnownSlots)
		focus.PendingQuestion = decision.ClarifyQuestion
		focus.TurnStatus = string(decision.Decision)
		return decision
	}

	first := runTurn("查 checkout 和 payment")
	if first.Decision != DecisionClarify {
		t.Fatalf("first decision = %#v", first)
	}
	focus.Candidates = []string{"checkout", "payment"}
	second := runTurn("第二个")
	if second.KnownSlots["service"] != "payment" {
		t.Fatalf("second decision = %#v", second)
	}
	focus.Candidates = []string{"checkout", "payment"}
	third := runTurn("算了，还是第一个")
	if third.KnownSlots["service"] != "checkout" {
		t.Fatalf("third decision = %#v", third)
	}

	focus = FocusView{KnownSlots: map[string]string{}}
	runTurn("查昨天的 checkout")
	changed := runTurn("换成 payment")
	unchanged := runTurn("时间不变")
	if changed.KnownSlots["service"] != "payment" ||
		changed.KnownSlots["time_range"] != "yesterday" ||
		unchanged.KnownSlots["service"] != "payment" ||
		unchanged.KnownSlots["time_range"] != "yesterday" {
		t.Fatalf("changed=%#v unchanged=%#v", changed, unchanged)
	}

	focus = FocusView{
		Available:       true,
		LastIntent:      string(IntentTraceAnalysis),
		KnownSlots:      map[string]string{},
		PendingQuestion: "请提供服务名",
		TurnStatus:      string(DecisionClarify),
	}
	trace := runTurn("我不知道服务名，但有 trace ID abcdef0123456789")
	if trace.Decision != DecisionProceed ||
		trace.KnownSlots["trace_id"] != "abcdef0123456789" {
		t.Fatalf("trace decision = %#v", trace)
	}

	focus = FocusView{
		Available:       true,
		LastIntent:      string(IntentIncidentTriage),
		KnownSlots:      map[string]string{},
		PendingQuestion: "需要排查哪个服务？",
		TurnStatus:      string(DecisionClarify),
	}
	rag := runTurn("我先不查了，给我解释一下 RAG")
	if rag.Decision != DecisionProceed || rag.Result.Intent != IntentGeneralChat {
		t.Fatalf("RAG decision = %#v", rag)
	}
}

func cloneStringMapForTest(source map[string]string) map[string]string {
	result := map[string]string{}
	for key, value := range source {
		result[key] = value
	}
	return result
}
