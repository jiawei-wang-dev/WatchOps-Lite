package bootstrap

import (
	"context"
	"strings"
	"time"

	applicationchat "github.com/jiawei-wang-dev/WatchOps-Lite/internal/application/chat"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/eval"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

type chatCaseExecutor interface {
	Execute(context.Context, applicationchat.Command) (applicationchat.Result, error)
}

func newEvalCaseExecutor(chat chatCaseExecutor) eval.CaseExecutor {
	return eval.CaseExecutorFunc(func(
		ctx context.Context,
		input eval.EvalInput,
	) (eval.EvalOutput, error) {
		now := time.Now().UTC()
		result, err := chat.Execute(ctx, applicationchat.Command{
			RequestID: input.RequestID,
			SessionID: input.SessionID,
			Message:   input.Message,
			TimeContext: common.TimeRange{
				From: now.Add(-20 * time.Minute).Format(time.RFC3339),
				To:   now.Format(time.RFC3339),
			},
		})
		if err != nil {
			return eval.EvalOutput{}, err
		}
		return mapEvalOutput(result), nil
	})
}

func mapEvalOutput(result applicationchat.Result) eval.EvalOutput {
	textParts := make([]string, 0)
	for _, conclusion := range result.Agent.Conclusions {
		textParts = appendNonEmpty(textParts, conclusion.Text)
	}
	for _, inference := range result.Agent.Inferences {
		textParts = appendNonEmpty(textParts, inference.Text)
	}
	for _, recommendation := range result.Agent.Recommendations {
		textParts = appendNonEmpty(textParts, recommendation.Text)
	}
	for _, limitation := range result.Agent.Limitations {
		textParts = appendNonEmpty(textParts, limitation.Message)
	}
	toolErrors := 0
	for _, toolRun := range result.Agent.ToolRuns {
		if !toolRun.Success || toolRun.ErrorCode != "" {
			toolErrors++
		}
	}
	return eval.EvalOutput{
		RequestID:           result.RequestID,
		TraceID:             result.TraceID,
		Text:                strings.Join(textParts, "\n"),
		EvidenceCount:       len(result.Agent.Evidence),
		ToolRunCount:        len(result.Agent.ToolRuns),
		ToolErrorCount:      toolErrors,
		LimitationCount:     len(result.Agent.Limitations),
		ConclusionCount:     len(result.Agent.Conclusions),
		RecommendationCount: len(result.Agent.Recommendations),
	}
}

func appendNonEmpty(values []string, value string) []string {
	if value = strings.TrimSpace(value); value != "" {
		return append(values, value)
	}
	return values
}
