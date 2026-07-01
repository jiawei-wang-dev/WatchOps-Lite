package eval

import (
	"context"
	"strings"
	"testing"
)

type runStoreStub struct {
	storeStub
	run     Run
	results []CaseResult
}

func (s *runStoreStub) CreateRun(_ context.Context, run Run) error {
	s.run = run
	return s.err
}

func (s *runStoreStub) SaveCaseResult(_ context.Context, result CaseResult) error {
	s.results = append(s.results, result)
	return s.err
}

func (s *runStoreStub) CompleteRun(_ context.Context, run Run) error {
	s.run = run
	return s.err
}

func (s *runStoreStub) GetRun(context.Context, string) (Run, error) {
	return s.run, s.err
}

func (s *runStoreStub) ListCaseResults(context.Context, string) ([]CaseResult, error) {
	return s.results, s.err
}

func TestCheckCaseDetectsForbiddenPattern(t *testing.T) {
	reasons := checkCase(context.Background(), Case{
		ID:                "case-1",
		CaseType:          CaseTypeBad,
		ForbiddenPatterns: []string{"definitely the root cause"},
	}, EvalOutput{
		Text:          "Payment is definitely the root cause.",
		EvidenceCount: 1,
		ToolRunCount:  1,
	})
	if len(reasons) != 1 || !strings.Contains(reasons[0], "forbidden pattern") {
		t.Fatalf("reasons = %#v", reasons)
	}
}

func TestCheckCaseRequiresEvidenceAndLimitation(t *testing.T) {
	reasons := checkCase(context.Background(), Case{
		ID:       "case-1",
		CaseType: CaseTypeBad,
		Metadata: map[string]any{
			"require_evidence":   true,
			"require_limitation": true,
		},
	}, EvalOutput{ToolRunCount: 1})
	if len(reasons) != 2 {
		t.Fatalf("reasons = %#v", reasons)
	}
}

func TestRunnerPersistsCaseResultsAndSummary(t *testing.T) {
	store := &runStoreStub{storeStub: storeStub{cases: []Case{
		{ID: "case-pass", CaseType: CaseTypeGood, InputMessage: "Question"},
		{
			ID: "case-fail", CaseType: CaseTypeBad, InputMessage: "Question",
			ForbiddenPatterns: []string{"unsupported"},
		},
	}}}
	executor := CaseExecutorFunc(func(
		_ context.Context,
		input EvalInput,
	) (EvalOutput, error) {
		text := "Evidence-backed answer"
		if strings.Contains(input.SessionID, "case-fail") {
			text = "unsupported"
		}
		return EvalOutput{
			RequestID:       input.RequestID,
			TraceID:         "trace-1",
			Text:            text,
			EvidenceCount:   1,
			ToolRunCount:    1,
			ConclusionCount: 1,
		}, nil
	})
	service, err := NewServiceWithRunner(store, feedbackReaderStub{}, executor)
	if err != nil {
		t.Fatalf("NewServiceWithRunner() error = %v", err)
	}

	run, err := service.Run(context.Background(), RunRequest{Limit: 2})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if run.Status != RunStatusCompleted ||
		run.Total != 2 ||
		run.Passed != 1 ||
		run.Failed != 1 ||
		len(store.results) != 2 {
		t.Fatalf("run=%#v results=%#v", run, store.results)
	}
}
