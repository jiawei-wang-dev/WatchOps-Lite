package multiagent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/diagnosis"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

type SynthesisAgent struct {
	primary  Synthesizer
	fallback *DeterministicSynthesizer
}

func NewSynthesisAgent(primary Synthesizer) *SynthesisAgent {
	return &SynthesisAgent{
		primary:  primary,
		fallback: &DeterministicSynthesizer{},
	}
}

func (a *SynthesisAgent) Synthesize(
	ctx context.Context,
	input SynthesisInput,
) (agenteino.AgentOutput, error) {
	if a.primary != nil {
		primaryOutput, err := a.primary.Synthesize(ctx, input)
		if err == nil {
			// Synthesis is the only role allowed to produce conclusions, so its
			// output is revalidated against evidence IDs before it can reach the API.
			if validationErr := validateSynthesisOutput(
				primaryOutput,
				input.Evidence,
			); validationErr == nil {
				return normalizeSynthesisOutput(
					primaryOutput,
					input,
					false,
					"",
				), nil
			} else {
				err = validationErr
			}
		}
		fallback, fallbackErr := a.fallback.Synthesize(ctx, input)
		if fallbackErr != nil {
			return agenteino.AgentOutput{}, fallbackErr
		}
		mergeSynthesisMetadata(fallback.Metadata, primaryOutput.Metadata)
		return normalizeSynthesisOutput(
			fallback,
			input,
			true,
			synthesisFallbackReason(err),
		), nil
	}
	fallback, err := a.fallback.Synthesize(ctx, input)
	if err != nil {
		return agenteino.AgentOutput{}, err
	}
	return normalizeSynthesisOutput(
		fallback,
		input,
		true,
		"primary_synthesizer_unavailable",
	), nil
}

type DeterministicSynthesizer struct{}

func (DeterministicSynthesizer) Synthesize(
	_ context.Context,
	input SynthesisInput,
) (agenteino.AgentOutput, error) {
	output := agenteino.AgentOutput{
		Conclusions:     []agenteino.Conclusion{},
		Evidence:        append([]common.EvidenceItem{}, input.Evidence...),
		Inferences:      []agenteino.Inference{},
		Recommendations: []agenteino.Recommendation{},
		Limitations:     append([]agenteino.Limitation{}, input.Limitations...),
		ToolRuns:        append([]agenteino.ToolRun{}, input.ToolRuns...),
		Metadata: map[string]any{
			"synthesis_mode":     "deterministic",
			"hypotheses":         input.Hypotheses,
			"hypothesis_count":   len(input.Hypotheses.Items),
			"hypothesis_enabled": len(input.Hypotheses.Items) > 0,
			"role_skill_cards":   input.Plan.AgentPlan.RoleSkillCards[AgentRoleSynthesis],
			"role_skill_names": roleSkillNamesForRole(
				input.Plan.AgentPlan.RoleSkillHints,
				AgentRoleSynthesis,
			),
		},
	}
	for key, value := range roleContextMetadata(input.Plan.Metadata["session_context"], AgentRoleSynthesis) {
		output.Metadata[key] = value
	}
	if len(input.Evidence) == 0 {
		output.Limitations = mergeLimitations(
			output.Limitations,
			[]agenteino.Limitation{{
				Code: "MULTI_AGENT_EVIDENCE_EMPTY",
				Message: localizedTriageText(
					input.Plan.Language,
					"Multi-Agent 未取得可用证据，不能声明已观察到根因。",
					"Multi-Agent returned no usable evidence and cannot claim an observed root cause.",
				),
			}},
		)
		return output, nil
	}

	observationIDs := validFindingEvidenceIDs(
		input.EvidenceFinding,
		input.Evidence,
	)
	knowledgeIDs := validFindingEvidenceIDs(
		input.KnowledgeFinding,
		input.Evidence,
	)
	if len(observationIDs) > 0 {
		output.Conclusions = append(output.Conclusions, agenteino.Conclusion{
			Text: localizedTriageText(
				input.Plan.Language,
				"观测证据已为 service="+input.Plan.Service+" 提供可验证的故障信号。",
				"Observability evidence provides verifiable incident signals for service="+input.Plan.Service+".",
			),
			EvidenceIDs: observationIDs,
		})
	}
	if len(knowledgeIDs) > 0 {
		output.Conclusions = append(output.Conclusions, agenteino.Conclusion{
			Text: localizedTriageText(
				input.Plan.Language,
				"知识库返回了与 incident_type="+input.Plan.IncidentType+" 相关的处理指导。",
				"Knowledge retrieval returned guidance relevant to incident_type="+input.Plan.IncidentType+".",
			),
			EvidenceIDs: knowledgeIDs,
		})
	}
	if len(observationIDs) > 0 && len(knowledgeIDs) > 0 {
		combined := append(append([]string{}, observationIDs...), knowledgeIDs...)
		output.Inferences = append(output.Inferences, agenteino.Inference{
			Text: localizedTriageText(
				input.Plan.Language,
				"当前观测信号与 runbook 场景一致，但仍需按 limitations 验证后才能确认根因。",
				"Current observability signals align with the runbook scenario, but limitations must be resolved before confirming root cause.",
			),
			EvidenceIDs: combined,
		})
		output.Recommendations = append(
			output.Recommendations,
			agenteino.Recommendation{
				Text: localizedTriageText(
					input.Plan.Language,
					"按引用的 runbook 顺序验证缓解步骤，并持续对照实时指标、日志和 Trace。",
					"Validate mitigation steps in the cited runbook order while checking live metrics, logs, and traces.",
				),
				EvidenceIDs: combined,
			},
		)
	} else if len(observationIDs) > 0 {
		output.Recommendations = append(
			output.Recommendations,
			agenteino.Recommendation{
				Text: localizedTriageText(
					input.Plan.Language,
					"继续补充 runbook 或历史经验，再根据现有观测证据选择缓解动作。",
					"Retrieve runbook or historical guidance before selecting mitigation from the current observability evidence.",
				),
				EvidenceIDs: observationIDs,
			},
		)
	} else if len(knowledgeIDs) > 0 {
		output.Recommendations = append(
			output.Recommendations,
			agenteino.Recommendation{
				Text: localizedTriageText(
					input.Plan.Language,
					"先取得实时观测证据，再执行知识库中的缓解建议。",
					"Collect live observability evidence before applying the knowledge-base mitigation guidance.",
				),
				EvidenceIDs: knowledgeIDs,
			},
		)
	}
	if supported := supportedHypotheses(input.Hypotheses); len(supported) > 0 {
		best := supported[0]
		output.Inferences = append(output.Inferences, agenteino.Inference{
			Text: localizedTriageText(
				input.Plan.Language,
				"假设评估显示最可能的根因方向是 "+best.Title+"；该判断仍必须以引用证据为边界。",
				"Hypothesis evaluation indicates the most likely root-cause direction is "+best.Title+"; this remains bounded by the cited evidence.",
			),
			EvidenceIDs: citationBackedEvidenceIDs(best.SupportingEvidence, input.Evidence),
		})
	}
	if missing := missingHypothesisEvidence(input.Hypotheses); len(missing) > 0 {
		output.Metadata["hypothesis_missing_evidence"] = missing
	}
	return output, nil
}

func supportedHypotheses(set diagnosis.HypothesisSet) []diagnosis.Hypothesis {
	result := []diagnosis.Hypothesis{}
	for _, item := range set.Items {
		if item.Status == diagnosis.StatusSupported {
			result = append(result, item)
		}
	}
	return result
}

func missingHypothesisEvidence(set diagnosis.HypothesisSet) []string {
	result := []string{}
	for _, item := range set.Items {
		for _, missing := range item.MissingEvidence {
			result = appendUniqueString(result, item.ID+":"+missing)
		}
	}
	return result
}

func citationBackedEvidenceIDs(
	citations []string,
	evidence []common.EvidenceItem,
) []string {
	if len(citations) == 0 {
		return []string{}
	}
	result := []string{}
	for _, citation := range citations {
		for _, item := range evidence {
			if item.Metadata != nil && item.Metadata["citation_id"] == citation {
				result = appendUniqueString(result, item.ID)
			}
		}
	}
	if len(result) > 0 {
		return result
	}
	for _, item := range evidence {
		result = append(result, item.ID)
		if len(result) == 2 {
			break
		}
	}
	return result
}

func appendUniqueString(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func validateSynthesisOutput(
	output agenteino.AgentOutput,
	evidence []common.EvidenceItem,
) error {
	if len(evidence) > 0 && len(output.Conclusions) == 0 {
		return errors.New("synthesis output has no conclusions")
	}
	validIDs := evidenceIDSet(evidence)
	statements := make([]struct {
		text string
		ids  []string
	}, 0, len(output.Conclusions)+len(output.Inferences)+len(output.Recommendations))
	for _, item := range output.Conclusions {
		statements = append(statements, struct {
			text string
			ids  []string
		}{item.Text, item.EvidenceIDs})
	}
	for _, item := range output.Inferences {
		statements = append(statements, struct {
			text string
			ids  []string
		}{item.Text, item.EvidenceIDs})
	}
	for _, item := range output.Recommendations {
		statements = append(statements, struct {
			text string
			ids  []string
		}{item.Text, item.EvidenceIDs})
	}
	for _, statement := range statements {
		if strings.TrimSpace(statement.text) == "" {
			return errors.New("synthesis output contains empty text")
		}
		if len(statement.ids) == 0 {
			return errors.New("synthesis statement is not evidence-bound")
		}
		for _, id := range statement.ids {
			if _, exists := validIDs[id]; !exists {
				return fmt.Errorf("synthesis cites unknown evidence id %q", id)
			}
		}
	}
	return nil
}

func normalizeSynthesisOutput(
	output agenteino.AgentOutput,
	input SynthesisInput,
	fallbackUsed bool,
	fallbackReason string,
) agenteino.AgentOutput {
	output.Evidence = append([]common.EvidenceItem{}, input.Evidence...)
	output.ToolRuns = append([]agenteino.ToolRun{}, input.ToolRuns...)
	output.Limitations = mergeLimitations(input.Limitations, output.Limitations)
	if output.Metadata == nil {
		output.Metadata = map[string]any{}
	}
	if _, exists := output.Metadata["llm_attempted"]; !exists {
		for key, value := range roleLLMNotConfiguredMetadata(
			AgentRoleSynthesis,
			"primary_synthesizer_unavailable",
		) {
			output.Metadata[key] = value
		}
	}
	output.Metadata["fallback_used"] = fallbackUsed
	output.Metadata["fallback"] = fallbackUsed
	if fallbackUsed && fallbackReason != "" {
		output.Metadata["fallback_reason"] = fallbackReason
	} else if !fallbackUsed {
		delete(output.Metadata, "fallback_reason")
		delete(output.Metadata, "synthesis_fallback_reason")
	}
	if _, exists := output.Metadata["synthesis_llm_used"]; !exists {
		output.Metadata["synthesis_llm_used"] = false
	}
	if _, exists := output.Metadata["synthesis_llm_attempted"]; !exists {
		output.Metadata["synthesis_llm_attempted"] = false
	}
	if _, exists := output.Metadata["synthesis_model"]; !exists {
		output.Metadata["synthesis_model"] = ""
	}
	output.Metadata["synthesis_fallback_used"] = fallbackUsed
	output.Metadata["synthesis_fallback"] = fallbackUsed
	if _, exists := output.Metadata["synthesis_llm_duration_ms"]; !exists {
		output.Metadata["synthesis_llm_duration_ms"] = int64(0)
	}
	if _, exists := output.Metadata["synthesis_mode"]; !exists {
		output.Metadata["synthesis_mode"] = "primary"
	}
	for key, value := range roleContextMetadata(input.Plan.Metadata["session_context"], AgentRoleSynthesis) {
		output.Metadata[key] = value
	}
	finalDiagnosis := buildFinalDiagnosis(output, input)
	output.Metadata["final_diagnosis"] = finalDiagnosis
	output.Metadata["final_diagnosis_schema_version"] = finalDiagnosisSchemaVersion
	return output
}

const finalDiagnosisSchemaVersion = "watchops.final_diagnosis.v1"

func buildFinalDiagnosis(output agenteino.AgentOutput, input SynthesisInput) FinalDiagnosis {
	language := requestedLanguageForPlan(input.Plan)
	evidenceRefs := evidenceReferencesFromItems(output.Evidence, language)
	limitations, executionWarnings := splitFinalLimitations(output.Limitations)
	limitations = append(limitations, dataLimitationsFromToolRuns(output.ToolRuns, language)...)
	executionWarnings = append(executionWarnings, executionWarningsFromToolRuns(output.ToolRuns, language)...)
	limitations = dedupeFinalLimitations(limitations)
	executionWarnings = dedupeExecutionWarnings(executionWarnings)
	evidenceByID := evidenceByID(output.Evidence)
	evidenceSummary := summarizeEvidence(output.Evidence)
	hasDependencyMetrics := evidenceSummary.HasDependencyMetrics(input.Plan.Service)
	hasCausalEvidence := evidenceSummary.HasCausalEvidence(input.Plan.Service)
	hasCurrentObservations := evidenceSummary.HasCurrentObservations()
	hasHistoricalOnly := evidenceSummary.HistoricalOnly()
	validRootCandidate := false
	rootCandidate := agenteino.Inference{}
	for _, inference := range output.Inferences {
		if isCausalRootCauseCandidate(inference.Text) && !isSymptomStatement(inference.Text) {
			rootCandidate = inference
			validRootCandidate = true
			break
		}
	}
	if !validRootCandidate {
		for _, inference := range output.Inferences {
			if strings.TrimSpace(inference.Text) != "" && !isSymptomStatement(inference.Text) {
				rootCandidate = inference
				validRootCandidate = true
				break
			}
		}
	}

	findings := make([]FinalFinding, 0, len(output.Conclusions)+len(output.Inferences))
	for _, conclusion := range output.Conclusions {
		evidenceIDs := validEvidenceIDs(conclusion.EvidenceIDs, output.Evidence)
		kind := classifyFindingKind(conclusion.Text, evidenceIDs, evidenceByID, "fact")
		findings = append(findings, FinalFinding{
			Title:       finalFindingTitle(conclusion.Text, kind, language),
			Description: conclusion.Text,
			EvidenceIDs: evidenceIDs,
			Confidence:  confidenceForFinding(kind, evidenceIDs, evidenceSummary),
			Kind:        kind,
		})
	}
	for _, inference := range output.Inferences {
		evidenceIDs := validEvidenceIDs(inference.EvidenceIDs, output.Evidence)
		kind := classifyFindingKind(inference.Text, evidenceIDs, evidenceByID, "hypothesis")
		findings = append(findings, FinalFinding{
			Title:       finalFindingTitle(inference.Text, kind, language),
			Description: inference.Text,
			EvidenceIDs: evidenceIDs,
			Confidence:  confidenceForFinding(kind, evidenceIDs, evidenceSummary),
			Kind:        kind,
		})
	}
	findings = dedupeFinalFindings(findings)

	recommendations := make([]FinalRecommendation, 0, len(output.Recommendations))
	for index, recommendation := range output.Recommendations {
		evidenceIDs := validEvidenceIDs(recommendation.EvidenceIDs, output.Evidence)
		profile := recommendationProfile(recommendation.Text, evidenceIDs, output.Evidence, language)
		recommendations = append(recommendations, FinalRecommendation{
			Priority:     priorityForIndex(index),
			Action:       recommendation.Text,
			Reason:       profile.Reason,
			Risk:         profile.Risk,
			Verification: profile.Verification,
			EvidenceIDs:  evidenceIDs,
		})
	}

	root := RootCauseAssessment{
		Conclusion:   localizedInsufficientRootCause(language),
		Confidence:   "low",
		EvidenceIDs:  []string{},
		Alternatives: []string{},
		Status:       "insufficient_evidence",
	}
	if validRootCandidate {
		root.Conclusion = rootCandidate.Text
		root.EvidenceIDs = validEvidenceIDs(rootCandidate.EvidenceIDs, output.Evidence)
		root.Confidence = confidenceForEvidence(root.EvidenceIDs)
		root.Status = rootStatusForConfidence(root.Confidence)
	}
	root = adjustRootCauseConfidence(
		root,
		language,
		evidenceSummary,
		hasCurrentObservations,
		hasCausalEvidence,
		hasDependencyMetrics,
		hasHistoricalOnly,
	)
	for _, inference := range output.Inferences {
		text := strings.TrimSpace(inference.Text)
		if text != "" && text != root.Conclusion {
			root.Alternatives = append(root.Alternatives, text)
		}
	}

	summary := localizedFinalSummary(language, input.Plan.Service, input.Plan.IncidentType, len(output.Evidence))
	if root.Status != "insufficient_evidence" && strings.TrimSpace(root.Conclusion) != "" {
		summary = root.Conclusion
	} else if len(findings) > 0 && strings.TrimSpace(findings[0].Description) != "" {
		summary = findings[0].Description
	}
	evidenceCompleteness := evidenceCompleteness(evidenceSummary, limitations)
	dataDegraded := len(executionWarnings) > 0 || evidenceCompleteness != "complete"
	diagnosisStatus := diagnosisStatusForRoot(root, evidenceCompleteness)
	evidenceCounts := evidenceOriginCounts(output.Evidence)
	return FinalDiagnosis{
		SchemaVersion: finalDiagnosisSchemaVersion,
		Language:      language,
		ExecutionMode: "multi_agent",
		Summary:       summary,
		Incident: IncidentOverview{
			Service:      input.Plan.Service,
			IncidentType: input.Plan.IncidentType,
			Severity:     severityForIncident(input.Plan.IncidentType),
			Status:       statusForEvidence(language, len(output.Evidence)),
		},
		Findings:          findings,
		RootCause:         root,
		Recommendations:   recommendations,
		Limitations:       limitations,
		ExecutionWarnings: executionWarnings,
		EvidenceRefs:      evidenceRefs,
		Metadata: map[string]any{
			"schema_version":           finalDiagnosisSchemaVersion,
			"fallback_used":            output.Metadata["fallback_used"],
			"evidence_completeness":    evidenceCompleteness,
			"data_degraded":            dataDegraded,
			"diagnosis_status":         diagnosisStatus,
			"execution_status":         executionStatusForDiagnosis(dataDegraded, output.Metadata),
			"llm_execution_status":     synthesisLLMExecutionStatus(output.Metadata),
			"diagnostic_warning_count": len(executionWarnings),
			"limitation_count":         len(limitations),
			"live_evidence_count":      evidenceCounts["live"],
			"knowledge_evidence_count": evidenceCounts["knowledge"],
			"long_term_memory_count":   evidenceCounts["historical_memory"],
			"fallback_evidence_count":  evidenceCounts["fallback"],
			"total_evidence_count":     evidenceCounts["total"],
		},
	}
}

func evidenceReferencesFromItems(items []common.EvidenceItem, language string) []EvidenceReference {
	result := make([]EvidenceReference, 0, len(items))
	for _, item := range items {
		if id := strings.TrimSpace(item.ID); id != "" {
			result = append(result, EvidenceReference{
				ID:                    id,
				Type:                  item.SourceType,
				Title:                 evidenceReferenceTitle(item),
				RawExcerpt:            item.Content,
				Interpretation:        evidenceInterpretation(item, language),
				SourceLabel:           sourceLabel(item),
				Source:                item.SourceName,
				DataStatus:            evidenceDataStatus(item),
				EvidenceWeight:        evidenceWeight(item),
				EvidenceOrigin:        evidenceOrigin(item),
				CanConfirmCurrentFact: evidenceCanConfirmCurrentFact(item),
				CanSupportHypothesis:  evidenceCanSupportHypothesis(item),
				SupportsRootCause:     evidenceSupportsRootCause(item),
			})
		}
	}
	return result
}

func evidenceDataStatus(item common.EvidenceItem) string {
	if item.Metadata != nil {
		if status, _ := item.Metadata["data_status"].(string); status != "" {
			return status
		}
		if fallback, _ := item.Metadata["fallback_used"].(bool); fallback {
			return "fallback"
		}
		if mode, _ := item.Metadata["mode"].(string); strings.Contains(strings.ToLower(mode), "fallback") || strings.Contains(strings.ToLower(mode), "mock") {
			return "fallback"
		}
	}
	return "available"
}

func evidenceWeight(item common.EvidenceItem) string {
	if evidenceDataStatus(item) == "fallback" {
		return "contextual"
	}
	switch evidenceOrigin(item) {
	case "knowledge", "historical_memory", "fallback":
		return "contextual"
	}
	if item.SourceType == "knowledge" || item.SourceType == "topology" ||
		item.SourceType == "memory" || item.SourceType == "long_term_memory" {
		return "contextual"
	}
	return "primary"
}

func evidenceOrigin(item common.EvidenceItem) string {
	if item.Metadata != nil {
		if origin, _ := item.Metadata["evidence_origin"].(string); origin != "" {
			return origin
		}
	}
	if evidenceDataStatus(item) == "fallback" {
		return "fallback"
	}
	switch strings.ToLower(strings.TrimSpace(item.SourceType)) {
	case "metrics", "logs", "traces", "alerts", "topology":
		return "live"
	case "knowledge":
		return "knowledge"
	case "memory", "long_term_memory":
		return "historical_memory"
	default:
		return "inferred"
	}
}

func evidenceCanConfirmCurrentFact(item common.EvidenceItem) bool {
	return evidenceOrigin(item) == "live" && evidenceDataStatus(item) != "fallback"
}

func evidenceCanSupportHypothesis(item common.EvidenceItem) bool {
	switch evidenceOrigin(item) {
	case "live", "knowledge", "historical_memory":
		return true
	default:
		return false
	}
}

func evidenceSupportsRootCause(item common.EvidenceItem) bool {
	return evidenceWeight(item) == "primary" && evidenceDataStatus(item) != "fallback"
}

func evidenceOriginCounts(items []common.EvidenceItem) map[string]int {
	counts := map[string]int{
		"live":              0,
		"knowledge":         0,
		"historical_memory": 0,
		"fallback":          0,
		"inferred":          0,
		"total":             0,
	}
	for _, item := range items {
		origin := evidenceOrigin(item)
		counts[origin]++
		if origin != "inferred" {
			counts["total"]++
		}
	}
	return counts
}

func diagnosisStatusForRoot(root RootCauseAssessment, completeness string) string {
	if completeness == "empty" {
		return "insufficient_evidence"
	}
	if completeness == "partial" && root.Status == "confirmed" {
		return "supported"
	}
	switch root.Status {
	case "confirmed", "supported", "hypothesis_only", "insufficient_evidence":
		return root.Status
	}
	if root.Confidence == "high" && completeness == "complete" {
		return "confirmed"
	}
	if root.Confidence == "medium" {
		return "supported"
	}
	return "hypothesis_only"
}

func executionStatusForDiagnosis(dataDegraded bool, metadata map[string]any) string {
	if fallback, _ := metadata["fallback_used"].(bool); fallback {
		return "degraded"
	}
	if dataDegraded {
		return "degraded"
	}
	return "success"
}

func validEvidenceIDs(ids []string, evidence []common.EvidenceItem) []string {
	valid := evidenceIDSet(evidence)
	result := []string{}
	seen := map[string]struct{}{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if _, ok := valid[id]; !ok {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func localizedIndexedTitle(language, zhPrefix, enPrefix string, index int) string {
	if normalizeRequestedLanguage(language) == "zh-CN" {
		return zhPrefix + " " + fmt.Sprint(index)
	}
	return enPrefix + " " + fmt.Sprint(index)
}

func confidenceForEvidence(ids []string) string {
	switch {
	case len(ids) >= 2:
		return "high"
	case len(ids) == 1:
		return "medium"
	default:
		return "low"
	}
}

func rootStatusForConfidence(confidence string) string {
	switch confidence {
	case "high":
		return "likely"
	case "medium":
		return "possible"
	default:
		return "insufficient_evidence"
	}
}

func priorityForIndex(index int) string {
	if index == 0 {
		return "P0"
	}
	if index == 1 {
		return "P1"
	}
	return "P2"
}

func localizedEvidenceReason(language string, ids []string) string {
	if normalizeRequestedLanguage(language) == "zh-CN" {
		if len(ids) == 0 {
			return "当前建议缺少直接证据引用，执行前需要补充验证。"
		}
		return "该建议由引用证据支持，执行前仍需结合实时状态确认。"
	}
	if len(ids) == 0 {
		return "This recommendation has no direct evidence reference and needs validation before action."
	}
	return "This recommendation is supported by cited evidence and should still be checked against live state."
}

func localizedRecommendationRisk(language string) string {
	if normalizeRequestedLanguage(language) == "zh-CN" {
		return "未验证前直接操作可能扩大影响面。"
	}
	return "Acting before validation may increase blast radius."
}

func localizedRecommendationVerification(language string) string {
	if normalizeRequestedLanguage(language) == "zh-CN" {
		return "执行前后对比 metrics、logs、traces 与告警状态。"
	}
	return "Compare metrics, logs, traces, and alert state before and after action."
}

func localizedInsufficientRootCause(language string) string {
	if normalizeRequestedLanguage(language) == "zh-CN" {
		return "证据不足，当前不能确认已观察到根因。"
	}
	return "Evidence is insufficient to confirm an observed root cause."
}

func localizedFinalSummary(language, service, incidentType string, evidenceCount int) string {
	if normalizeRequestedLanguage(language) == "zh-CN" {
		return fmt.Sprintf("已对 service=%s 的 %s 信号完成证据约束分析，当前证据数=%d。", service, incidentType, evidenceCount)
	}
	return fmt.Sprintf("Evidence-bound analysis completed for service=%s, incident_type=%s, evidence_count=%d.", service, incidentType, evidenceCount)
}

func evidenceReferenceTitle(item common.EvidenceItem) string {
	source := strings.TrimSpace(item.SourceType)
	if source == "" {
		source = "evidence"
	}
	if item.ResourceID != "" {
		return source + ": " + item.ResourceID
	}
	return source + ": " + item.ID
}

func evidenceInterpretation(item common.EvidenceItem, language string) string {
	source := strings.TrimSpace(item.SourceType)
	if normalizeRequestedLanguage(language) == "zh-CN" {
		switch source {
		case "metrics":
			return "指标证据显示与当前故障相关的运行状态。"
		case "logs":
			return "日志证据提供了与请求或错误相关的原始事件。"
		case "traces":
			return "Trace 证据提供了调用路径或耗时线索。"
		case "knowledge":
			return "知识库证据提供了 runbook 或历史处理指导。"
		default:
			return "该证据用于支撑诊断结论。"
		}
	}
	switch source {
	case "metrics":
		return "Metric evidence shows runtime state related to the incident."
	case "logs":
		return "Log evidence provides raw events related to requests or errors."
	case "traces":
		return "Trace evidence provides call-path or latency clues."
	case "knowledge":
		return "Knowledge evidence provides runbook or historical guidance."
	default:
		return "This evidence supports the diagnosis."
	}
}

func severityForIncident(incidentType string) string {
	switch incidentType {
	case IncidentHighErrorRate, IncidentPaymentTimeout:
		return "high"
	case IncidentLatency:
		return "medium"
	default:
		return "unknown"
	}
}

func statusForEvidence(language string, evidenceCount int) string {
	if evidenceCount == 0 {
		return "insufficient_evidence"
	}
	return "investigating"
}

type finalEvidenceSummary struct {
	BySource map[string]int
	Services map[string]int
}

func summarizeEvidence(items []common.EvidenceItem) finalEvidenceSummary {
	summary := finalEvidenceSummary{
		BySource: map[string]int{},
		Services: map[string]int{},
	}
	for _, item := range items {
		if evidenceDataStatus(item) == "fallback" {
			continue
		}
		source := strings.TrimSpace(item.SourceType)
		if source == "" {
			source = "unknown"
		}
		summary.BySource[source]++
		if service := evidenceService(item); service != "" {
			summary.Services[service]++
		}
	}
	return summary
}

func (s finalEvidenceSummary) HasCurrentObservations() bool {
	for _, source := range []string{"metrics", "logs", "traces", "alerts", "topology"} {
		if s.BySource[source] > 0 {
			return true
		}
	}
	return false
}

func (s finalEvidenceSummary) HistoricalOnly() bool {
	if s.HasCurrentObservations() {
		return false
	}
	return s.BySource["knowledge"] > 0 || s.BySource["memory"] > 0 || s.BySource["long_term_memory"] > 0
}

func (s finalEvidenceSummary) HasCausalEvidence(primaryService string) bool {
	return s.BySource["traces"] > 0 || s.BySource["logs"] > 0 || s.HasDependencyMetrics(primaryService)
}

func (s finalEvidenceSummary) HasDependencyMetrics(primaryService string) bool {
	if s.BySource["metrics"] == 0 {
		return false
	}
	primaryService = strings.TrimSpace(primaryService)
	for service := range s.Services {
		if service != "" && service != primaryService {
			return true
		}
	}
	return false
}

func evidenceByID(items []common.EvidenceItem) map[string]common.EvidenceItem {
	result := map[string]common.EvidenceItem{}
	for _, item := range items {
		if id := strings.TrimSpace(item.ID); id != "" {
			result[id] = item
		}
	}
	return result
}

func evidenceService(item common.EvidenceItem) string {
	if value, ok := item.Metadata["service"].(string); ok {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(item.ResourceID)
}

func classifyFindingKind(
	text string,
	evidenceIDs []string,
	evidence map[string]common.EvidenceItem,
	defaultKind string,
) string {
	sourceCounts := map[string]int{}
	for _, id := range evidenceIDs {
		if item, ok := evidence[id]; ok {
			sourceCounts[item.SourceType]++
		}
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, "correlat") ||
		strings.Contains(text, "同时") ||
		strings.Contains(text, "重叠") ||
		strings.Contains(text, "相关") {
		return "correlation"
	}
	if sourceCounts["memory"] > 0 ||
		sourceCounts["long_term_memory"] > 0 ||
		sourceCounts["knowledge"] > 0 {
		if defaultKind == "hypothesis" && isCausalRootCauseCandidate(text) {
			return "hypothesis"
		}
		return "historical_match"
	}
	if defaultKind == "hypothesis" {
		return "hypothesis"
	}
	return "fact"
}

func confidenceForFinding(kind string, ids []string, summary finalEvidenceSummary) string {
	if kind == "historical_match" || kind == "hypothesis" {
		if len(ids) == 0 {
			return "low"
		}
		return "medium"
	}
	if kind == "correlation" {
		if len(ids) >= 2 {
			return "medium"
		}
		return "low"
	}
	return confidenceForEvidence(ids)
}

func finalFindingTitle(text string, kind string, language string) string {
	lower := strings.ToLower(text)
	switch {
	case kind == "historical_match" && normalizeRequestedLanguage(language) == "zh-CN":
		return "历史模式与当前症状相似"
	case kind == "historical_match":
		return "Historical pattern matches current symptoms"
	case kind == "correlation" && normalizeRequestedLanguage(language) == "zh-CN":
		return "多个观测信号存在时间相关性"
	case kind == "correlation":
		return "Observed signals are time-correlated"
	case kind == "hypothesis" && (strings.Contains(lower, "payment") || strings.Contains(text, "支付")):
		if normalizeRequestedLanguage(language) == "zh-CN" {
			return "payment 依赖异常需要验证"
		}
		return "Payment dependency issue needs verification"
	case kind == "hypothesis":
		if normalizeRequestedLanguage(language) == "zh-CN" {
			return "待验证的因果假设"
		}
		return "Causal hypothesis to verify"
	case strings.Contains(lower, "alert") || strings.Contains(text, "告警"):
		if normalizeRequestedLanguage(language) == "zh-CN" {
			return "相关告警已触发"
		}
		return "Related alerts are firing"
	case strings.Contains(lower, "error") || strings.Contains(lower, "5xx") || strings.Contains(text, "错误率"):
		if normalizeRequestedLanguage(language) == "zh-CN" {
			return "checkout 错误率显著升高"
		}
		return "Checkout error rate is elevated"
	case strings.Contains(lower, "timeout") || strings.Contains(text, "超时"):
		if normalizeRequestedLanguage(language) == "zh-CN" {
			return "请求超时信号出现"
		}
		return "Timeout signal detected"
	case strings.Contains(lower, "latency") || strings.Contains(text, "延迟"):
		if normalizeRequestedLanguage(language) == "zh-CN" {
			return "延迟信号升高"
		}
		return "Latency signal is elevated"
	default:
		if normalizeRequestedLanguage(language) == "zh-CN" {
			return "证据发现"
		}
		return "Evidence finding"
	}
}

func isCausalRootCauseCandidate(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "because") ||
		strings.Contains(lower, "cause") ||
		strings.Contains(lower, "contributor") ||
		strings.Contains(lower, "dependency") ||
		strings.Contains(lower, "payment") ||
		strings.Contains(lower, "retry") ||
		strings.Contains(text, "导致") ||
		strings.Contains(text, "依赖") ||
		strings.Contains(text, "可能") ||
		strings.Contains(text, "重试")
}

func isSymptomStatement(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	hasSymptom := strings.Contains(lower, "error rate") ||
		strings.Contains(lower, "错误率") ||
		strings.Contains(lower, "alert") ||
		strings.Contains(lower, "告警")
	hasCausal := strings.Contains(lower, "because") ||
		strings.Contains(lower, "cause") ||
		strings.Contains(lower, "可能") ||
		strings.Contains(lower, "导致") ||
		strings.Contains(lower, "dependency") ||
		strings.Contains(lower, "依赖")
	return hasSymptom && !hasCausal
}

func adjustRootCauseConfidence(
	root RootCauseAssessment,
	language string,
	summary finalEvidenceSummary,
	hasCurrentObservations bool,
	hasCausalEvidence bool,
	hasDependencyMetrics bool,
	historicalOnly bool,
) RootCauseAssessment {
	if strings.TrimSpace(root.Conclusion) == "" || root.Status == "insufficient_evidence" {
		return root
	}
	if historicalOnly || !hasCurrentObservations {
		root.Status = "possible"
		root.Confidence = minConfidence(root.Confidence, "low")
		root.Conclusion = appendEvidenceBoundary(root.Conclusion, language, "historical_only")
		return root
	}
	if !hasCausalEvidence {
		root.Status = "possible"
		root.Confidence = minConfidence(root.Confidence, "medium")
		root.Conclusion = appendEvidenceBoundary(root.Conclusion, language, "missing_logs_traces")
	}
	if !hasDependencyMetrics {
		root.Status = "possible"
		root.Confidence = minConfidence(root.Confidence, "medium")
		root.Conclusion = appendEvidenceBoundary(root.Conclusion, language, "missing_dependency_metrics")
	}
	if (summary.BySource["logs"] == 0 && summary.BySource["traces"] == 0) && root.Confidence == "high" {
		root.Confidence = "medium"
		if root.Status == "confirmed" {
			root.Status = "likely"
		}
	}
	return root
}

func minConfidence(current string, maximum string) string {
	rank := map[string]int{"low": 0, "medium": 1, "high": 2}
	if rank[current] > rank[maximum] {
		return maximum
	}
	if _, ok := rank[current]; !ok {
		return maximum
	}
	return current
}

func appendEvidenceBoundary(conclusion string, language string, reason string) string {
	if strings.Contains(conclusion, "缺少") || strings.Contains(strings.ToLower(conclusion), "missing") {
		return conclusion
	}
	if normalizeRequestedLanguage(language) == "zh-CN" {
		switch reason {
		case "historical_only":
			return conclusion + "，但当前判断主要依赖历史/Runbook 证据，不能独立确认当前根因。"
		case "missing_dependency_metrics":
			return conclusion + "，但当前时间窗口缺少依赖服务实时指标，暂时只能作为可能根因。"
		default:
			return conclusion + "，但当前时间窗口缺少日志或 Trace 因果链证据，不能确认为最终根因。"
		}
	}
	switch reason {
	case "historical_only":
		return conclusion + ", but this relies mainly on historical/runbook evidence and cannot independently confirm the current root cause."
	case "missing_dependency_metrics":
		return conclusion + ", but current dependency metrics are missing, so this remains a possible root cause."
	default:
		return conclusion + ", but logs or trace causal evidence is missing for the current window."
	}
}

type recommendationText struct {
	Reason       string
	Risk         string
	Verification string
}

func recommendationProfile(
	action string,
	evidenceIDs []string,
	evidence []common.EvidenceItem,
	language string,
) recommendationText {
	lower := strings.ToLower(action)
	hasKnowledge := hasEvidenceSource(evidenceIDs, evidence, "knowledge") ||
		hasEvidenceSource(evidenceIDs, evidence, "memory") ||
		hasEvidenceSource(evidenceIDs, evidence, "long_term_memory")
	if normalizeRequestedLanguage(language) == "zh-CN" {
		switch {
		case strings.Contains(lower, "payment") || strings.Contains(action, "支付"):
			return recommendationText{
				Reason:       "当前 checkout 异常与 payment 依赖线索相关，但仍缺少完整实时依赖证据。",
				Risk:         "若只依据相关性或历史模式判断，可能误把伴随现象当作因果根因。",
				Verification: "对齐 payment p95/p99 延迟、超时计数、错误率与 checkout 5xx 的时间序列，确认是否同步变化。",
			}
		case strings.Contains(lower, "retry") || strings.Contains(action, "重试"):
			return recommendationText{
				Reason:       "Runbook 或历史事件提示依赖延迟可能触发 checkout retry amplification。",
				Risk:         "过度限制重试可能降低瞬时故障下的自动恢复能力，并影响部分请求成功率。",
				Verification: "调整前后比较 checkout 重试次数、payment QPS、payment 延迟、checkout 5xx 和超时数量。",
			}
		case strings.Contains(lower, "log") || strings.Contains(action, "日志"):
			return recommendationText{
				Reason:       "当前诊断缺少请求级错误日志，无法确认错误类型与依赖返回码。",
				Risk:         "继续缺少日志会让根因判断停留在指标相关性层面。",
				Verification: "检查 checkout 与 payment 的 timeout、context deadline exceeded、5xx 和 request_id 日志。",
			}
		case strings.Contains(lower, "trace") || strings.Contains(action, "Trace"):
			return recommendationText{
				Reason:       "Trace 可以验证 checkout 到下游依赖的耗时路径和错误传播链。",
				Risk:         "没有 Trace 时容易遗漏网关、DB 或第三方依赖等实际瓶颈。",
				Verification: "按 trace_id/request_id 查看慢 span、错误 span 与 dependency path，确认瓶颈位置。",
			}
		case hasKnowledge:
			return recommendationText{
				Reason:       "知识库或长期记忆提供了相似故障模式，但需要用当前观测数据验证。",
				Risk:         "历史相似不等于当前因果，直接套用 runbook 可能偏离真实问题。",
				Verification: "逐条对照 runbook 前置条件与当前 metrics、logs、traces 是否一致。",
			}
		default:
			return recommendationText{
				Reason:       "该建议基于当前已引用证据，但仍需要补齐缺失观测后执行。",
				Risk:         "证据不完整时直接操作可能扩大影响面或导致误修复。",
				Verification: "执行前后对比相关 metrics、logs、traces 与告警状态。",
			}
		}
	}
	switch {
	case strings.Contains(lower, "payment"):
		return recommendationText{
			Reason:       "Checkout symptoms correlate with payment dependency signals, but complete live dependency evidence is still missing.",
			Risk:         "Acting on correlation alone may mistake a related symptom for the causal root cause.",
			Verification: "Align payment p95/p99 latency, timeout counts, error rate, and checkout 5xx over the same time window.",
		}
	case strings.Contains(lower, "retry"):
		return recommendationText{
			Reason:       "Runbook or historical evidence indicates dependency latency may trigger checkout retry amplification.",
			Risk:         "Over-limiting retries may reduce automatic recovery during transient failures.",
			Verification: "Compare checkout retry count, payment QPS, payment latency, checkout 5xx, and timeout counts before and after the change.",
		}
	default:
		return recommendationText{
			Reason:       "The recommendation is tied to cited evidence, but missing observations should be checked first.",
			Risk:         "Acting with incomplete evidence may increase blast radius or repair the wrong component.",
			Verification: "Compare related metrics, logs, traces, and alert state before and after action.",
		}
	}
}

func hasEvidenceSource(ids []string, evidence []common.EvidenceItem, source string) bool {
	byID := evidenceByID(evidence)
	for _, id := range ids {
		if item, ok := byID[id]; ok && item.SourceType == source {
			return true
		}
	}
	return false
}

func splitFinalLimitations(items []agenteino.Limitation) ([]FinalLimitation, []ExecutionWarning) {
	limitations := []FinalLimitation{}
	warnings := []ExecutionWarning{}
	for _, item := range items {
		code := strings.TrimSpace(item.Code)
		description := strings.TrimSpace(item.Message)
		if description == "" {
			description = code
		}
		if code == "" && description == "" {
			continue
		}
		if isExecutionWarningCode(code) {
			warnings = append(warnings, ExecutionWarning{
				Code:        nonEmpty(code, "EXECUTION_WARNING"),
				Description: description,
				Source:      item.Tool,
			})
			continue
		}
		limitations = append(limitations, FinalLimitation{
			Code:        nonEmpty(code, "LIMITATION"),
			Description: description,
			Source:      item.Tool,
		})
	}
	return limitations, warnings
}

func isExecutionWarningCode(code string) bool {
	upper := strings.ToUpper(strings.TrimSpace(code))
	return strings.HasPrefix(upper, "AGENT_") ||
		strings.Contains(upper, "MAX_ITERATION") ||
		strings.Contains(upper, "REPEATED_TOOL")
}

func dataLimitationsFromToolRuns(toolRuns []agenteino.ToolRun, language string) []FinalLimitation {
	result := []FinalLimitation{}
	for _, run := range toolRuns {
		dataStatus := toolDataStatus(run)
		if dataStatus != "empty" && dataStatus != "partial" {
			continue
		}
		code := canonicalNoDataCode(run.Tool)
		result = append(result, FinalLimitation{
			Code:        code,
			Description: localizedToolDataLimitation(run.Tool, dataStatus, language),
			Source:      run.Tool,
		})
	}
	return result
}

func canonicalNoDataCode(toolName string) string {
	upper := strings.ToUpper(strings.TrimSpace(toolName))
	switch {
	case strings.Contains(upper, "LOG"):
		return "LOGS_NO_DATA"
	case strings.Contains(upper, "TRACE"):
		return "TRACES_NO_DATA"
	case strings.Contains(upper, "METRIC"):
		return "METRICS_NO_DATA"
	case strings.Contains(upper, "KNOWLEDGE"):
		return "KNOWLEDGE_NO_DATA"
	case strings.Contains(upper, "ALERT"):
		return "ALERTS_NO_DATA"
	case strings.Contains(upper, "TOPOLOGY"):
		return "TOPOLOGY_NO_DATA"
	default:
		return upper + "_NO_DATA"
	}
}

func executionWarningsFromToolRuns(toolRuns []agenteino.ToolRun, language string) []ExecutionWarning {
	result := []ExecutionWarning{}
	for _, run := range toolRuns {
		switch toolDataStatus(run) {
		case "fallback":
			result = append(result, ExecutionWarning{
				Code:        "TOOL_FALLBACK_USED",
				Description: localizedToolWarning(run.Tool, "fallback", language),
				Source:      run.Tool,
			})
		case "partial":
			result = append(result, ExecutionWarning{
				Code:        "TOOL_DATA_PARTIAL",
				Description: localizedToolWarning(run.Tool, "partial", language),
				Source:      run.Tool,
			})
		}
	}
	return result
}

func toolDataStatus(run agenteino.ToolRun) string {
	if strings.TrimSpace(run.DataStatus) != "" {
		return run.DataStatus
	}
	if run.FallbackUsed {
		return "fallback"
	}
	if run.Metadata != nil {
		if value, ok := run.Metadata["data_status"].(string); ok && value != "" {
			return value
		}
		if fallback, _ := run.Metadata["fallback_used"].(bool); fallback {
			return "fallback"
		}
		if mode, _ := run.Metadata["mode"].(string); strings.Contains(mode, "fallback") || strings.Contains(mode, "mock") {
			return "fallback"
		}
	}
	if run.ErrorCode != "" {
		return "unknown"
	}
	if run.EvidenceCount == 0 {
		return "empty"
	}
	if run.WarningCount > 0 {
		return "partial"
	}
	return "available"
}

func localizedToolDataLimitation(toolName string, dataStatus string, language string) string {
	if normalizeRequestedLanguage(language) == "zh-CN" {
		return fmt.Sprintf("%s 在当前时间窗口未返回完整真实数据，根因判断需要补充验证。", toolName)
	}
	return fmt.Sprintf("%s did not return complete live data for the current window; root cause assessment needs more validation.", toolName)
}

func localizedToolWarning(toolName string, kind string, language string) string {
	if normalizeRequestedLanguage(language) == "zh-CN" {
		if kind == "fallback" {
			return fmt.Sprintf("%s 使用了 fallback 或演示数据，调用成功但数据质量已降级。", toolName)
		}
		return fmt.Sprintf("%s 返回了部分数据或警告，结果需要谨慎解释。", toolName)
	}
	if kind == "fallback" {
		return fmt.Sprintf("%s used fallback or demo data; invocation succeeded but data quality is degraded.", toolName)
	}
	return fmt.Sprintf("%s returned partial data or warnings and should be interpreted carefully.", toolName)
}

func dedupeFinalLimitations(items []FinalLimitation) []FinalLimitation {
	result := []FinalLimitation{}
	seen := map[string]int{}
	for _, item := range items {
		item.Code = canonicalLimitationCode(item.Code, item.Source)
		key := item.Code + "|" + item.Source
		if item.Code == "" && item.Source == "" {
			key = item.Description
		}
		if existingIndex, ok := seen[key]; ok {
			if len(item.Description) > len(result[existingIndex].Description) {
				result[existingIndex].Description = item.Description
			}
			continue
		}
		seen[key] = len(result)
		result = append(result, item)
	}
	return result
}

func canonicalLimitationCode(code string, source string) string {
	upper := strings.ToUpper(strings.TrimSpace(code))
	if strings.Contains(upper, "NO_DATA") {
		return canonicalNoDataCode(source + " " + upper)
	}
	return upper
}

func dedupeExecutionWarnings(items []ExecutionWarning) []ExecutionWarning {
	result := []ExecutionWarning{}
	seen := map[string]struct{}{}
	for _, item := range items {
		key := item.Code + "|" + item.Description + "|" + item.Source
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	return result
}

func dedupeFinalFindings(items []FinalFinding) []FinalFinding {
	result := []FinalFinding{}
	seen := map[string]struct{}{}
	for _, item := range items {
		if strings.TrimSpace(item.Description) == "" {
			continue
		}
		key := item.Kind + "|" + strings.Join(item.EvidenceIDs, ",") + "|" + strings.ToLower(strings.TrimSpace(item.Description))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	return result
}

func evidenceCompleteness(summary finalEvidenceSummary, limitations []FinalLimitation) string {
	if !summary.HasCurrentObservations() {
		return "empty"
	}
	if len(limitations) > 0 || summary.BySource["logs"] == 0 || summary.BySource["traces"] == 0 {
		return "partial"
	}
	return "complete"
}

func synthesisLLMExecutionStatus(metadata map[string]any) string {
	if metadata == nil {
		return "unknown"
	}
	if metadataBool(metadata, "synthesis_llm_success") {
		return "success"
	}
	if metadataBool(metadata, "synthesis_llm_attempted") {
		return "fallback"
	}
	return "unknown"
}

func sourceLabel(item common.EvidenceItem) string {
	if title, ok := item.Metadata["title"].(string); ok && strings.TrimSpace(title) != "" {
		return title
	}
	switch item.SourceType {
	case "knowledge":
		return "Knowledge RAG"
	case "memory", "long_term_memory":
		return "Long-term memory"
	default:
		if item.SourceName != "" {
			return item.SourceName
		}
		return item.SourceType
	}
}

func mergeSynthesisMetadata(target map[string]any, source map[string]any) {
	if target == nil {
		return
	}
	for key, value := range source {
		switch key {
		case "synthesis_llm_used",
			"synthesis_llm_attempted",
			"synthesis_llm_success",
			"synthesis_model",
			"synthesis_fallback_used",
			"synthesis_fallback",
			"synthesis_fallback_reason",
			"synthesis_llm_duration_ms",
			"synthesis_llm_elapsed_ms",
			"synthesis_llm_timeout_ms",
			"synthesis_llm_retry_count",
			"synthesis_llm_error_code",
			"synthesis_llm_error_message",
			"synthesis_primary_llm_attempted",
			"synthesis_primary_llm_success",
			"synthesis_primary_llm_elapsed_ms",
			"synthesis_primary_llm_timeout_ms",
			"synthesis_primary_error_code",
			"synthesis_primary_error_message",
			"synthesis_repair_attempted",
			"synthesis_repair_success",
			"synthesis_repair_llm_elapsed_ms",
			"synthesis_repair_llm_timeout_ms",
			"synthesis_repair_error_code",
			"synthesis_repair_error_message",
			"synthesis_repair_skip_reason",
			"synthesis_llm_primary_elapsed_ms",
			"synthesis_llm_repair_elapsed_ms",
			"synthesis_recovery_attempted",
			"synthesis_recovery_success",
			"synthesis_recovery_reason",
			"synthesis_analysis_mode",
			"llm_attempted",
			"llm_success",
			"llm_elapsed_ms",
			"llm_timeout_ms",
			"llm_retry_count",
			"llm_error_code",
			"llm_error_message",
			"primary_llm_attempted",
			"primary_llm_success",
			"primary_llm_elapsed_ms",
			"primary_llm_timeout_ms",
			"primary_error_code",
			"primary_error_message",
			"repair_attempted",
			"repair_success",
			"repair_llm_elapsed_ms",
			"repair_llm_timeout_ms",
			"repair_error_code",
			"repair_error_message",
			"repair_skip_reason",
			"llm_primary_elapsed_ms",
			"llm_repair_elapsed_ms",
			"recovery_attempted",
			"recovery_success",
			"recovery_reason",
			"fallback",
			"model",
			"analysis_mode":
			target[key] = value
		}
	}
}

func validFindingEvidenceIDs(
	finding AgentFinding,
	evidence []common.EvidenceItem,
) []string {
	valid := evidenceIDSet(evidence)
	result := []string{}
	seen := map[string]struct{}{}
	for _, id := range finding.EvidenceIDs {
		if _, exists := valid[id]; !exists {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func evidenceIDSet(evidence []common.EvidenceItem) map[string]struct{} {
	result := make(map[string]struct{}, len(evidence))
	for _, item := range evidence {
		if id := strings.TrimSpace(item.ID); id != "" {
			result[id] = struct{}{}
		}
	}
	return result
}

func synthesisFallbackReason(err error) string {
	if err == nil {
		return "primary_synthesis_invalid"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "unknown evidence id"):
		return "invalid_evidence_reference"
	case strings.Contains(message, "no conclusions"),
		strings.Contains(message, "empty text"),
		strings.Contains(message, "not evidence-bound"):
		return "invalid_synthesis_output"
	default:
		return "primary_synthesis_failed"
	}
}

func evidenceSources(evidence []common.EvidenceItem) []string {
	set := map[string]struct{}{}
	for _, item := range evidence {
		if source := strings.TrimSpace(item.SourceType); source != "" {
			set[source] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for source := range set {
		result = append(result, source)
	}
	sort.Strings(result)
	return result
}
