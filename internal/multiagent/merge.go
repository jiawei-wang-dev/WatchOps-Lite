package multiagent

import (
	"strconv"
	"strings"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

func MergeAgentFindings(
	plan TriagePlan,
	evidenceFinding AgentFinding,
	knowledgeFinding AgentFinding,
) MergedFindings {
	evidence := make([]common.EvidenceItem, 0,
		len(evidenceFinding.Evidence)+len(knowledgeFinding.Evidence))
	evidenceIDs := make([]string, 0, cap(evidence))
	seenEvidence := map[string]struct{}{}
	for _, items := range [][]common.EvidenceItem{
		evidenceFinding.Evidence,
		knowledgeFinding.Evidence,
	} {
		for _, item := range items {
			id := strings.TrimSpace(item.ID)
			if id == "" {
				continue
			}
			if _, exists := seenEvidence[id]; exists {
				continue
			}
			seenEvidence[id] = struct{}{}
			evidence = append(evidence, item)
			evidenceIDs = append(evidenceIDs, id)
		}
	}

	toolRuns := append(
		append([]agenteino.ToolRun{}, evidenceFinding.ToolRuns...),
		knowledgeFinding.ToolRuns...,
	)
	limitations := mergeLimitations(
		plan.Limitations,
		evidenceFinding.Limitations,
		knowledgeFinding.Limitations,
	)
	return MergedFindings{
		Plan:             plan,
		EvidenceFinding:  evidenceFinding,
		KnowledgeFinding: knowledgeFinding,
		Evidence:         evidence,
		EvidenceIDs:      evidenceIDs,
		ToolRuns:         toolRuns,
		Limitations:      limitations,
		Metadata: map[string]any{
			"evidence_count":   len(evidence),
			"tool_run_count":   len(toolRuns),
			"limitation_count": len(limitations),
			"evidence_agent_failed": hasAgentFailure(
				evidenceFinding,
				AgentRoleEvidence,
			),
			"knowledge_agent_failed": hasAgentFailure(
				knowledgeFinding,
				AgentRoleKnowledge,
			),
		},
	}
}

func mergeLimitations(groups ...[]agenteino.Limitation) []agenteino.Limitation {
	result := []agenteino.Limitation{}
	seen := map[string]struct{}{}
	for _, group := range groups {
		for _, limitation := range group {
			key := limitation.Code + "\x00" +
				limitation.Tool + "\x00" +
				limitation.Message
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, limitation)
		}
	}
	return result
}

func hasAgentFailure(finding AgentFinding, role AgentRole) bool {
	expectedCode := strings.ToUpper(string(role)) + "_AGENT_FAILED"
	for _, limitation := range finding.Limitations {
		if limitation.Code == expectedCode {
			return true
		}
	}
	if value, ok := finding.Metadata["agent_failed"]; ok {
		failed, _ := strconv.ParseBool(strings.TrimSpace(toString(value)))
		return failed
	}
	return false
}

func toString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	default:
		return ""
	}
}
