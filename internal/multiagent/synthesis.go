package multiagent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
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
		output, err := a.primary.Synthesize(ctx, input)
		if err == nil {
			if validationErr := validateSynthesisOutput(output, input.Evidence); validationErr == nil {
				return normalizeSynthesisOutput(output, input, false, ""), nil
			} else {
				err = validationErr
			}
		}
		fallback, fallbackErr := a.fallback.Synthesize(ctx, input)
		if fallbackErr != nil {
			return agenteino.AgentOutput{}, fallbackErr
		}
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
			"synthesis_mode": "deterministic",
		},
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
	return output, nil
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
	output.Metadata["fallback_used"] = fallbackUsed
	if fallbackReason != "" {
		output.Metadata["fallback_reason"] = fallbackReason
	}
	if _, exists := output.Metadata["synthesis_mode"]; !exists {
		output.Metadata["synthesis_mode"] = "primary"
	}
	return output
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
