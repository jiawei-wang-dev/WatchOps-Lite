package control

import (
	"strconv"
	"strings"
	"time"
)

type ToolRun struct {
	Tool          string
	Success       bool
	ErrorCode     string
	EvidenceCount int
}

type State struct {
	ToolCallCount              int
	SuccessfulToolCount        int
	FailedToolCount            int
	ConsecutiveToolFailures    int
	RepeatedToolCallSignature  string
	RepeatedToolCallCount      int
	EvidenceCount              int
	LimitationsCount           int
	Elapsed                    time.Duration
	OutputParseSuccess         bool
	MissingRequiredSections    bool
	MaxIterationsReachedLikely bool
}

func BuildState(
	toolRuns []ToolRun,
	evidenceCount int,
	limitationsCount int,
	elapsed time.Duration,
	outputParseSuccess bool,
	missingRequiredSections bool,
	maxIterations int,
) State {
	state := State{
		ToolCallCount:           len(toolRuns),
		EvidenceCount:           evidenceCount,
		LimitationsCount:        limitationsCount,
		Elapsed:                 elapsed,
		OutputParseSuccess:      outputParseSuccess,
		MissingRequiredSections: missingRequiredSections,
	}
	signatureCounts := map[string]int{}
	for _, run := range toolRuns {
		if run.Success {
			state.SuccessfulToolCount++
			state.ConsecutiveToolFailures = 0
		} else {
			state.FailedToolCount++
			state.ConsecutiveToolFailures++
		}
		signature := toolSignature(run)
		if signature == "" {
			continue
		}
		signatureCounts[signature]++
		if signatureCounts[signature] > state.RepeatedToolCallCount {
			state.RepeatedToolCallCount = signatureCounts[signature]
			state.RepeatedToolCallSignature = signature
		}
	}
	if maxIterations > 0 && len(toolRuns) >= maxIterations {
		state.MaxIterationsReachedLikely = true
	}
	return state
}

func toolSignature(run ToolRun) string {
	tool := strings.TrimSpace(run.Tool)
	if tool == "" {
		return ""
	}
	return tool + "|" + strings.TrimSpace(run.ErrorCode) + "|" + strconv.Itoa(run.EvidenceCount)
}
