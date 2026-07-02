package eino

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/control"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

func aggregateAgentEvidence(
	candidates []evidence.Candidate,
) ([]common.EvidenceItem, map[string]int) {
	aggregation := evidence.Aggregate(candidates)
	result := make([]common.EvidenceItem, 0, len(aggregation.Items))
	for _, item := range aggregation.Items {
		result = append(result, common.FromEvidenceItem(item))
	}
	return result, aggregation.GroupCounts()
}

type answerDraft struct {
	Conclusions     []draftEvidenceStatement `json:"conclusions"`
	Inferences      []draftEvidenceStatement `json:"inferences"`
	Recommendations []draftEvidenceStatement `json:"recommendations"`
	Limitations     []Limitation             `json:"limitations"`
}

type draftEvidenceStatement struct {
	Text        string   `json:"text"`
	EvidenceIDs []string `json:"evidence_ids"`
}

func parseAgentOutput(
	content string,
	evidence []common.EvidenceItem,
	toolRuns []ToolRun,
	toolLimitations []Limitation,
	metadata map[string]any,
) AgentOutput {
	output := AgentOutput{
		Conclusions:     []Conclusion{},
		Evidence:        evidence,
		Inferences:      []Inference{},
		Recommendations: []Recommendation{},
		Limitations:     append([]Limitation{}, toolLimitations...),
		ToolRuns:        toolRuns,
		Metadata:        cloneOutputMetadata(metadata),
	}

	var draft answerDraft
	if err := json.Unmarshal([]byte(stripJSONFence(content)), &draft); err != nil {
		output.Metadata["output_parse_success"] = false
		output.Limitations = append(output.Limitations, Limitation{
			Code:    "AGENT_OUTPUT_PARSE_FAILED",
			Message: "The model response could not be parsed into the required answer structure.",
		})
		return output
	}
	output.Metadata["output_parse_success"] = true
	missingSections := missingRequiredSections(stripJSONFence(content))
	if len(missingSections) > 0 {
		output.Metadata["missing_required_sections"] = true
		output.Metadata["missing_sections"] = missingSections
		output.Limitations = append(output.Limitations, Limitation{
			Code:    "AGENT_OUTPUT_MISSING_REQUIRED_SECTIONS",
			Message: "The model response omitted one or more required answer sections.",
		})
	} else {
		output.Metadata["missing_required_sections"] = false
	}

	allowedEvidence := make(map[string]struct{}, len(evidence))
	for _, item := range evidence {
		if item.ID != "" {
			allowedEvidence[item.ID] = struct{}{}
		}
	}
	invalidReferences := false
	for _, item := range draft.Conclusions {
		ids, invalid := validEvidenceIDs(item.EvidenceIDs, allowedEvidence)
		invalidReferences = invalidReferences || invalid
		if strings.TrimSpace(item.Text) == "" || len(ids) == 0 {
			if strings.TrimSpace(item.Text) != "" {
				invalidReferences = true
			}
			continue
		}
		output.Conclusions = append(output.Conclusions, Conclusion{
			Text:        strings.TrimSpace(item.Text),
			EvidenceIDs: ids,
		})
	}
	for _, item := range draft.Inferences {
		ids, invalid := validEvidenceIDs(item.EvidenceIDs, allowedEvidence)
		invalidReferences = invalidReferences || invalid
		if strings.TrimSpace(item.Text) == "" || len(ids) == 0 {
			if strings.TrimSpace(item.Text) != "" {
				invalidReferences = true
			}
			continue
		}
		output.Inferences = append(output.Inferences, Inference{
			Text:        strings.TrimSpace(item.Text),
			EvidenceIDs: ids,
		})
	}
	for _, item := range draft.Recommendations {
		ids, invalid := validEvidenceIDs(item.EvidenceIDs, allowedEvidence)
		invalidReferences = invalidReferences || invalid
		if text := strings.TrimSpace(item.Text); text != "" {
			output.Recommendations = append(output.Recommendations, Recommendation{
				Text:        text,
				EvidenceIDs: ids,
			})
		}
	}
	for _, limitation := range draft.Limitations {
		if strings.TrimSpace(limitation.Message) == "" {
			continue
		}
		limitation.Code = strings.TrimSpace(limitation.Code)
		if limitation.Code == "" {
			limitation.Code = "AGENT_LIMITATION"
		}
		limitation.Message = strings.TrimSpace(limitation.Message)
		limitation.Tool = strings.TrimSpace(limitation.Tool)
		output.Limitations = append(output.Limitations, limitation)
	}
	if invalidReferences {
		output.Limitations = append(output.Limitations, Limitation{
			Code:    "EVIDENCE_REFERENCE_INVALID",
			Message: "One or more model statements were removed because they did not reference returned evidence.",
		})
	}
	if len(output.Conclusions) == 0 && len(output.Evidence) == 0 {
		output.Limitations = append(output.Limitations, Limitation{
			Code:    "INSUFFICIENT_EVIDENCE",
			Message: "No tool evidence was returned to support a conclusion.",
		})
	}
	return output
}

func missingRequiredSections(content string) []string {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return nil
	}
	required := []string{"conclusions", "inferences", "recommendations", "limitations"}
	missing := make([]string, 0, len(required))
	for _, key := range required {
		if _, ok := raw[key]; !ok {
			missing = append(missing, key)
		}
	}
	return missing
}

func parseAgentOutputControlled(
	ctx context.Context,
	controller *control.Controller,
	content string,
	evidence []common.EvidenceItem,
	toolRuns []ToolRun,
	toolLimitations []Limitation,
	metadata map[string]any,
) AgentOutput {
	output := parseAgentOutput(content, evidence, toolRuns, toolLimitations, metadata)
	if parseSuccess, _ := output.Metadata["output_parse_success"].(bool); parseSuccess {
		return output
	}
	if controller == nil || !controller.Config().EnableJSONRepairOnce {
		return output
	}
	repaired, ok := controller.RepairJSON(ctx, stripJSONFence(content))
	output.Metadata["json_repair_attempted"] = true
	if !ok {
		output.Metadata["json_repair_success"] = false
		return output
	}
	repairedMetadata := cloneOutputMetadata(metadata)
	repairedMetadata["json_repair_attempted"] = true
	repairedMetadata["json_repair_success"] = true
	repairedOutput := parseAgentOutput(
		repaired,
		evidence,
		toolRuns,
		toolLimitations,
		repairedMetadata,
	)
	if parseSuccess, _ := repairedOutput.Metadata["output_parse_success"].(bool); parseSuccess {
		return repairedOutput
	}
	repairedOutput.Metadata["json_repair_success"] = false
	return repairedOutput
}

func validEvidenceIDs(values []string, allowed map[string]struct{}) ([]string, bool) {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	invalid := false
	for _, value := range values {
		value = strings.TrimSpace(value)
		if _, ok := allowed[value]; !ok || value == "" {
			invalid = true
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, invalid
}

func stripJSONFence(content string) string {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "```") {
		return content
	}
	if newline := strings.IndexByte(content, '\n'); newline >= 0 {
		content = content[newline+1:]
	}
	content = strings.TrimSpace(content)
	content = strings.TrimSuffix(content, "```")
	return strings.TrimSpace(content)
}

func cloneOutputMetadata(metadata map[string]any) map[string]any {
	copy := make(map[string]any, len(metadata))
	for key, value := range metadata {
		copy[key] = value
	}
	return copy
}
