package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func WriteReports(report Report, jsonPath, markdownPath, badCasePath string) error {
	SortBadCases(report.BadCases)
	for _, path := range []string{jsonPath, markdownPath, badCasePath} {
		if path == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
	}
	if jsonPath != "" {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(jsonPath, append(encoded, '\n'), 0o600); err != nil {
			return err
		}
	}
	if markdownPath != "" {
		if err := os.WriteFile(markdownPath, []byte(Markdown(report)), 0o600); err != nil {
			return err
		}
	}
	if badCasePath != "" {
		encoded, err := json.MarshalIndent(report.BadCases, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(badCasePath, append(encoded, '\n'), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func Markdown(report Report) string {
	var output strings.Builder
	fmt.Fprintf(&output, "# WatchOps-Lite Evaluation Summary\n\n")
	fmt.Fprintf(&output, "- Dataset: `%s`\n", report.Version)
	fmt.Fprintf(&output, "- Evaluation mode: `%s`\n", report.Mode)
	fmt.Fprintf(&output, "- External LLM calls: `%t`\n\n", report.ExternalLLM)
	fmt.Fprintf(&output, "## Governance Quality\n\n")
	fmt.Fprintf(&output, "### Intent Challenge\n\n")
	fmt.Fprintf(&output, "- Cases: %d\n- Intent accuracy: %.4f\n- Slot completeness: %.4f\n", report.Challenge.Cases, report.Challenge.IntentAccuracy, report.Challenge.SlotCompleteness)
	fmt.Fprintf(&output, "- Clarification precision / recall: %.4f / %.4f\n", report.Challenge.ClarificationPrecision, report.Challenge.ClarificationRecall)
	fmt.Fprintf(&output, "- Over / under clarification: %.4f / %.4f\n", report.Challenge.OverClarificationRate, report.Challenge.UnderClarificationRate)
	fmt.Fprintf(&output, "- LLM escalation / incorrect escalation: %.4f / %.4f\n\n", report.Challenge.LLMEscalationRate, report.Challenge.IncorrectLLMEscalationRate)
	fmt.Fprintf(&output, "### Final Optimization Before / After\n\n")
	fmt.Fprintf(&output, "| Intent metric | Before | After |\n|---|---:|---:|\n")
	fmt.Fprintf(&output, "| LLM escalation rate | %.4f | %.4f |\n", report.Optimization.Before.Intent.LLMEscalationRate, report.Optimization.After.Intent.LLMEscalationRate)
	fmt.Fprintf(&output, "| Incorrect escalation rate | %.4f | %.4f |\n", report.Optimization.Before.Intent.IncorrectEscalationRate, report.Optimization.After.Intent.IncorrectEscalationRate)
	fmt.Fprintf(&output, "| Challenge behavior accuracy | %.4f | %.4f |\n", report.Optimization.Before.Intent.ChallengeBehaviorAccuracy, report.Optimization.After.Intent.ChallengeBehaviorAccuracy)
	fmt.Fprintf(&output, "| Fixed regression accuracy | %.4f | %.4f |\n\n", report.Optimization.Before.Intent.FixedRegressionAccuracy, report.Optimization.After.Intent.FixedRegressionAccuracy)
	fmt.Fprintf(&output, "### Rule-first Hybrid Intent\n\n")
	fmt.Fprintf(&output, "| Mode | Rules direct | LLM escalations | LLM call rate | Fallbacks | Accuracy |\n")
	fmt.Fprintf(&output, "|---|---:|---:|---:|---:|---:|\n")
	for _, item := range []RuleFirstBenchmark{report.Benchmarks.RuleFirstBefore, report.Benchmarks.RuleFirstAfter} {
		fmt.Fprintf(&output, "| %s | %d | %d | %.4f | %d | %.4f |\n", item.Mode, item.RuleDirectCount, item.LLMEscalationCount, item.LLMCallRate, item.FallbackCount, item.IntentAccuracy)
	}
	c := report.Benchmarks.Clarification
	fmt.Fprintf(&output, "\n### Clarification Early Exit\n\n- Cases: %d\n- RAG / Agent / Tool calls: %d / %d / %d\n", c.Cases, c.RAGCalls, c.AgentCalls, c.ToolCalls)
	fmt.Fprintf(&output, "- Avoided RAG / Agent / Tool calls: %d / %d / %d\n", c.AvoidedRAGCalls, c.AvoidedAgentCalls, c.AvoidedToolCalls)

	fmt.Fprintf(&output, "\n## Tool Semantics\n\n")
	fmt.Fprintf(&output, "- Cases: %d\n", report.ToolEvidence.Cases)
	fmt.Fprintf(&output, "- Required tool recall: %.4f\n- Forbidden tool violation rate: %.4f\n", report.ToolEvidence.RequiredToolRecall, report.ToolEvidence.ForbiddenToolViolationRate)
	fmt.Fprintf(&output, "- Unnecessary tool call rate: %.4f\n- Tool path validity: %.4f\n", report.ToolEvidence.UnnecessaryToolCallRate, report.ToolEvidence.ToolPathValidity)
	fmt.Fprintf(&output, "- Legacy exact selection accuracy (compatibility): %.4f\n- Argument accuracy: %.4f\n", report.ToolEvidence.ToolSelectionAccuracy, report.ToolEvidence.ToolArgumentAccuracy)
	keys := make([]string, 0, len(report.ToolEvidence.ArgumentFieldAccuracy))
	for key := range report.ToolEvidence.ArgumentFieldAccuracy {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&output, "- Argument `%s`: %.4f\n", key, report.ToolEvidence.ArgumentFieldAccuracy[key])
	}

	fmt.Fprintf(&output, "\n## Evidence Safety\n\n")
	fmt.Fprintf(&output, "- Citation validity: %.4f\n- Required evidence coverage: %.4f\n", report.ToolEvidence.EvidenceCitationValidity, report.ToolEvidence.EvidenceCoverage)
	fmt.Fprintf(&output, "- Evidence allowlist violations: %d (rate %.4f)\n\n", report.ToolEvidence.EvidenceAllowlistViolationCount, report.ToolEvidence.EvidenceAllowlistViolationRate)

	fmt.Fprintf(&output, "## Agent E2E\n\n")
	fmt.Fprintf(&output, "- Cases: %d\n- Task success: %.4f\n- Decision accuracy: %.4f\n", report.AgentE2E.Cases, report.AgentE2E.TaskSuccessRate, report.AgentE2E.DecisionAccuracy)
	fmt.Fprintf(&output, "- Required evidence coverage: %.4f\n- Unsafe action rate: %.4f\n", report.AgentE2E.RequiredEvidenceCoverage, report.AgentE2E.UnsafeActionRate)
	fmt.Fprintf(&output, "- Completed / clarified / failed / timeout: %d / %d / %d / %d\n", report.AgentE2E.Completed, report.AgentE2E.Clarified, report.AgentE2E.Failed, report.AgentE2E.Timeout)
	fmt.Fprintf(&output, "- Repeated tool call stops: %d\n- Average tool calls: %.4f\n", report.AgentE2E.RepeatedToolCallStops, report.AgentE2E.AverageToolCalls)
	if report.AgentE2E.AverageAgentSteps == nil {
		fmt.Fprintf(&output, "- Average agent steps: N/A\n")
	} else {
		fmt.Fprintf(&output, "- Average agent steps: %.4f\n", *report.AgentE2E.AverageAgentSteps)
	}
	fmt.Fprintf(&output, "- P50 / P95 latency: %.3f / %.3f ms\n", report.AgentE2E.P50LatencyMS, report.AgentE2E.P95LatencyMS)
	fmt.Fprintf(&output, "\n### Agent E2E Before / After\n\n")
	fmt.Fprintf(&output, "| Metric | Before | After |\n|---|---:|---:|\n")
	fmt.Fprintf(&output, "| Task success | %.4f | %.4f |\n", report.Optimization.Before.AgentE2E.TaskSuccessRate, report.Optimization.After.AgentE2E.TaskSuccessRate)
	fmt.Fprintf(&output, "| Decision accuracy | %.4f | %.4f |\n", report.Optimization.Before.AgentE2E.DecisionAccuracy, report.Optimization.After.AgentE2E.DecisionAccuracy)
	fmt.Fprintf(&output, "| Unsafe action rate | %.4f | %.4f |\n", report.Optimization.Before.AgentE2E.UnsafeActionRate, report.Optimization.After.AgentE2E.UnsafeActionRate)
	fmt.Fprintf(&output, "| Timeout behavior count | %d | %d |\n", report.Optimization.Before.AgentE2E.Timeout, report.Optimization.After.AgentE2E.Timeout)
	fmt.Fprintf(&output, "| Repeated tool stops | %d | %d |\n", report.Optimization.Before.AgentE2E.RepeatedStops, report.Optimization.After.AgentE2E.RepeatedStops)

	fmt.Fprintf(&output, "\n## Retrieval Quality\n\n")
	fmt.Fprintf(&output, "| Mode | Query | Recall@1 | Recall@3 | Recall@5 | MRR | Candidates | Latency ms |\n|---|---|---:|---:|---:|---:|---:|---:|\n")
	for _, item := range report.Retrieval {
		fmt.Fprintf(&output, "| %s | %s | %.4f | %.4f | %.4f | %.4f | %.2f | %.3f |\n", item.Mode, item.QueryMode, item.RecallAt1, item.RecallAt3, item.RecallAt5, item.MRR, item.AverageCandidates, item.AverageLatencyMS)
	}
	mq := report.MultiQuery
	fmt.Fprintf(&output, "\n### Conditional Multi-Query Decision\n\n")
	fmt.Fprintf(&output, "- Single / Multi cases: %d / %d\n", mq.SingleQueryCases, mq.MultiQueryCases)
	fmt.Fprintf(&output, "- Trigger rate: %.4f\n", mq.MultiQueryTriggerRate)
	fmt.Fprintf(&output, "- Correct / incorrect / neutral trigger rate: %.4f / %.4f / %.4f\n", mq.MultiQueryCorrectTriggerRate, mq.IncorrectMultiQueryTriggerRate, mq.NeutralMultiQueryTriggerRate)
	fmt.Fprintf(&output, "- Single-query subset Recall@5 / MRR: %.4f / %.4f\n", mq.SingleQueryRecallAt5, mq.SingleQueryMRR)
	fmt.Fprintf(&output, "- Multi-query subset Recall@5 / MRR: %.4f / %.4f\n", mq.MultiQueryRecallAt5, mq.MultiQueryMRR)
	fmt.Fprintf(&output, "- Conditional overall Recall@5 / MRR: %.4f / %.4f\n", mq.ConditionalRecallAt5, mq.ConditionalMRR)
	fmt.Fprintf(&output, "\n### Retrieval Before / After\n\n")
	fmt.Fprintf(&output, "| Metric | Before | After conditional subset |\n|---|---:|---:|\n")
	fmt.Fprintf(&output, "| Single Query Recall@5 | %.4f | %.4f |\n", report.Optimization.Before.Retrieval.SingleQueryRecallAt5, report.Optimization.After.Retrieval.SingleQueryRecallAt5)
	fmt.Fprintf(&output, "| Single Query MRR | %.4f | %.4f |\n", report.Optimization.Before.Retrieval.SingleQueryMRR, report.Optimization.After.Retrieval.SingleQueryMRR)
	fmt.Fprintf(&output, "| Multi Query Recall@5 | %.4f | %.4f |\n", report.Optimization.Before.Retrieval.MultiQueryRecallAt5, report.Optimization.After.Retrieval.MultiQueryRecallAt5)
	fmt.Fprintf(&output, "| Multi Query MRR | %.4f | %.4f |\n", report.Optimization.Before.Retrieval.MultiQueryMRR, report.Optimization.After.Retrieval.MultiQueryMRR)
	fmt.Fprintf(&output, "| Retrieval bad cases | %d | %d |\n", report.Optimization.Before.Retrieval.BadCases, report.Optimization.After.Retrieval.BadCases)

	fmt.Fprintf(&output, "\n## Context Governance\n\n")
	fmt.Fprintf(&output, "| Mode | Messages | Characters | Bytes | Focus bytes | Retained fields | Redacted fields |\n")
	fmt.Fprintf(&output, "|---|---:|---:|---:|---:|---|---|\n")
	for _, item := range []struct {
		name  string
		value ContextMeasurement
	}{
		{"raw_history", report.Benchmarks.Context.RawHistory},
		{"recent+summary+focus", report.Benchmarks.Context.Governed},
	} {
		fmt.Fprintf(&output, "| %s | %d | %d | %d | %d | %s | %s |\n",
			item.name, item.value.MessageCount, item.value.ContextChars,
			item.value.ContextBytes, item.value.FocusSizeBytes,
			strings.Join(item.value.RetainedImportantFields, ", "),
			strings.Join(item.value.RedactedFields, ", "))
	}

	b := report.BadCaseComparison
	fmt.Fprintf(&output, "\n## Bad Case Before / After\n\n")
	fmt.Fprintf(&output, "- Before: %d records / %d unique cases\n- After: %d records / %d unique cases\n", b.BeforeRecords, b.BeforeUnique, b.AfterRecords, b.AfterUnique)
	fmt.Fprintf(&output, "- Fixed / still failing / new regression: %d / %d / %d\n- Challenge findings: %d\n\n", b.Fixed, b.StillFailing, b.NewRegression, b.ChallengeFindings)
	fmt.Fprintf(&output, "| Case | Stage | Root cause | Fix type | Before | After | Fixed |\n|---|---|---|---|---|---|---:|\n")
	for _, item := range b.Cases {
		fmt.Fprintf(&output, "| %s | %s | %s | %s | %s | %s | %t |\n", item.CaseID, item.Stage, item.RootCause, item.FixType, item.BeforeBehavior, item.AfterBehavior, item.Fixed)
	}
	f := report.FinalBadCaseComparison
	fmt.Fprintf(&output, "\n## Final Optimization Bad Cases\n\n")
	fmt.Fprintf(&output, "- Before / After / Fixed: %d / %d / %d\n\n", f.Before, f.After, f.Fixed)
	fmt.Fprintf(&output, "| Case | Stage | Input | Root cause | Fix type | After | Fixed |\n|---|---|---|---|---|---|---:|\n")
	for _, item := range f.Cases {
		fmt.Fprintf(&output, "| %s | %s | %s | %s | %s | %s | %t |\n", item.CaseID, item.Stage, item.Input, item.RootCause, item.FixType, item.AfterBehavior, item.Fixed)
	}

	fmt.Fprintf(&output, "\n## Open Quality Findings\n\n")
	counts := map[string]int{}
	for _, item := range report.BadCases {
		counts[item.Stage]++
	}
	stages := make([]string, 0, len(counts))
	for stage := range counts {
		stages = append(stages, stage)
	}
	sort.Strings(stages)
	fmt.Fprintf(&output, "- Total: %d\n", len(report.BadCases))
	for _, stage := range stages {
		fmt.Fprintf(&output, "- %s: %d\n", stage, counts[stage])
	}
	if len(stages) == 0 {
		fmt.Fprintf(&output, "- None\n")
	}

	fmt.Fprintf(&output, "\n## Fixed Regression Safety Suite\n\n")
	fmt.Fprintf(&output, "- Intent cases: %d\n- Intent accuracy: %.4f\n- Slot accuracy: %.4f\n- Joint exact match: %.4f\n- Clarification accuracy: %.4f\n", report.Intent.Cases, report.Intent.Accuracy, report.Intent.SlotFieldAccuracy, report.Intent.JointExactMatch, report.Intent.ClarificationAccuracy)
	return output.String()
}
