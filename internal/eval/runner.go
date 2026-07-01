package eval

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	runtimemetrics "github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability/metrics"
	"go.opentelemetry.io/otel/attribute"
)

const defaultRunLimit = 20

func (s *Service) Run(ctx context.Context, request RunRequest) (result Run, resultErr error) {
	defer func() {
		status := "completed"
		if resultErr != nil {
			status = "failed"
		}
		runtimemetrics.IncEvalRun(status)
	}()
	ctx, span := observability.StartSpan(
		ctx,
		"eval.run",
		attribute.String("case_type", string(request.CaseType)),
	)
	defer span.End()
	if s.runStore == nil || s.caseExecutor == nil {
		return Run{}, ErrUnavailable
	}
	request.CaseType = CaseType(strings.ToLower(strings.TrimSpace(string(request.CaseType))))
	if request.CaseType != "" && !validCaseType(request.CaseType) {
		return Run{}, fmt.Errorf("%w: case_type must be good_case or bad_case", ErrInvalidArgument)
	}
	if request.Limit == 0 {
		request.Limit = defaultRunLimit
	}
	if request.Limit < 1 || request.Limit > maxListLimit {
		return Run{}, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidArgument, maxListLimit)
	}
	cases, err := s.List(ctx, ListQuery{CaseType: request.CaseType, Limit: request.Limit})
	if err != nil {
		return Run{}, err
	}
	runID, err := generateRunID()
	if err != nil {
		return Run{}, fmt.Errorf("generate eval run ID: %w", err)
	}
	run := Run{
		ID:        runID,
		CaseType:  request.CaseType,
		Status:    RunStatusRunning,
		Total:     len(cases),
		CreatedAt: s.now(),
	}
	if err := s.runStore.CreateRun(ctx, run); err != nil {
		return Run{}, err
	}
	for _, evalCase := range cases {
		result := s.executeCase(ctx, run, evalCase)
		if result.Passed {
			run.Passed++
		} else {
			run.Failed++
		}
		if err := s.runStore.SaveCaseResult(ctx, result); err != nil {
			return Run{}, err
		}
	}
	run.Status = RunStatusCompleted
	run.CompletedAt = s.now()
	if err := s.runStore.CompleteRun(ctx, run); err != nil {
		return Run{}, err
	}
	span.SetAttributes(
		attribute.String("run_id", run.ID),
		attribute.Int("total", run.Total),
		attribute.Int("passed", run.Passed),
		attribute.Int("failed", run.Failed),
	)
	return run, nil
}

func (s *Service) executeCase(
	ctx context.Context,
	run Run,
	evalCase Case,
) CaseResult {
	ctx, span := observability.StartSpan(
		ctx,
		"eval.case.execute",
		attribute.String("run_id", run.ID),
		attribute.String("case_id", evalCase.ID),
		attribute.String("case_type", string(evalCase.CaseType)),
	)
	defer span.End()
	startedAt := time.Now()
	requestID := run.ID + "-" + evalCase.ID
	output, err := s.caseExecutor.ExecuteEvalCase(ctx, EvalInput{
		RequestID: requestID,
		SessionID: "eval:" + run.ID + ":" + evalCase.ID,
		Message:   evalCase.InputMessage,
	})
	reasons := []string{}
	if err != nil {
		reasons = append(reasons, "chat execution failed")
	} else {
		reasons = checkCase(ctx, evalCase, output)
	}
	resultID, idErr := generateResultID()
	if idErr != nil {
		resultID = run.ID + "-" + evalCase.ID
	}
	result := CaseResult{
		ID:             resultID,
		RunID:          run.ID,
		CaseID:         evalCase.ID,
		Passed:         len(reasons) == 0,
		FailureReasons: reasons,
		RequestID:      output.RequestID,
		TraceID:        output.TraceID,
		DurationMS:     time.Since(startedAt).Milliseconds(),
		CreatedAt:      s.now(),
	}
	if result.RequestID == "" {
		result.RequestID = requestID
	}
	span.SetAttributes(
		attribute.Bool("passed", result.Passed),
		attribute.Int("failure_count", len(reasons)),
		attribute.Int64("duration_ms", result.DurationMS),
	)
	return result
}

func checkCase(ctx context.Context, evalCase Case, output EvalOutput) []string {
	_, span := observability.StartSpan(
		ctx,
		"eval.case.check",
		attribute.String("case_id", evalCase.ID),
	)
	defer span.End()
	reasons := []string{}
	requireEvidence := metadataBool(evalCase.Metadata, "require_evidence", true)
	requireToolRuns := metadataBool(evalCase.Metadata, "require_tool_runs", true)
	requireLimitation := metadataBool(evalCase.Metadata, "require_limitation", false)
	requireConclusions := metadataBool(evalCase.Metadata, "require_conclusions", false)
	requireRecommendations := metadataBool(evalCase.Metadata, "require_recommendations", false)
	if requireEvidence && output.EvidenceCount == 0 {
		reasons = append(reasons, "evidence is required")
	}
	if requireToolRuns && output.ToolRunCount == 0 {
		reasons = append(reasons, "tool_runs are required")
	}
	if requireLimitation && output.LimitationCount == 0 {
		reasons = append(reasons, "a limitation is required")
	}
	if requireConclusions && output.ConclusionCount == 0 {
		reasons = append(reasons, "conclusions are required")
	}
	if requireRecommendations && output.RecommendationCount == 0 {
		reasons = append(reasons, "recommendations are required")
	}
	if evalCase.CaseType == CaseTypeGood && output.ToolErrorCount > 0 {
		reasons = append(reasons, "unexpected tool errors were returned")
	}
	lowerText := strings.ToLower(output.Text)
	for _, pattern := range evalCase.ForbiddenPatterns {
		if strings.Contains(lowerText, strings.ToLower(pattern)) {
			reasons = append(reasons, "forbidden pattern matched: "+pattern)
		}
	}
	return reasons
}

func metadataBool(metadata map[string]any, key string, fallback bool) bool {
	value, ok := metadata[key]
	if !ok {
		return fallback
	}
	typed, ok := value.(bool)
	if !ok {
		return fallback
	}
	return typed
}

func (s *Service) GetRun(ctx context.Context, runID string) (Run, error) {
	if s.runStore == nil {
		return Run{}, ErrUnavailable
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return Run{}, fmt.Errorf("%w: run ID is required", ErrInvalidArgument)
	}
	return s.runStore.GetRun(ctx, runID)
}

func (s *Service) ListRunResults(
	ctx context.Context,
	runID string,
) ([]CaseResult, error) {
	if s.runStore == nil {
		return nil, ErrUnavailable
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("%w: run ID is required", ErrInvalidArgument)
	}
	return s.runStore.ListCaseResults(ctx, runID)
}

func generateRunID() (string, error) {
	return randomID("run_")
}

func generateResultID() (string, error) {
	return randomID("result_")
}

func randomID(prefix string) (string, error) {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(value[:]), nil
}
