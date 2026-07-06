package multiagent

import (
	"context"
	"strings"

	retrievalknowledge "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/knowledge"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

type RoleAwareRetriever interface {
	HybridRetrieve(context.Context, retrievalknowledge.RetrievalRequest) (retrievalknowledge.RetrievalResult, error)
}

func buildRoleAwareRAGContext(
	result retrievalknowledge.RetrievalResult,
) RoleRAGContext {
	context := RoleRAGContext{
		ChunksByRole: map[AgentRole][]retrievalknowledge.RetrievedKnowledge{
			AgentRoleTriage:    {},
			AgentRoleEvidence:  {},
			AgentRoleKnowledge: {},
			AgentRoleSynthesis: {},
		},
		Metadata: cloneMetadata(result.Metadata),
	}
	for _, chunk := range result.Chunks {
		role := selectKnowledgeRole(chunk)
		context.ChunksByRole[role] = append(context.ChunksByRole[role], chunk)
	}
	context.SynthesisSummary = summarizeChunksForSynthesis(result.Chunks)
	if context.SynthesisSummary != "" {
		context.ChunksByRole[AgentRoleSynthesis] = append(
			context.ChunksByRole[AgentRoleSynthesis],
			retrievalknowledge.RetrievedKnowledge{
				ID:              "role-rag-synthesis-summary",
				ChunkID:         "role-rag-synthesis-summary",
				Title:           "Compressed role-aware RAG summary",
				Source:          "role-aware-rag",
				Content:         context.SynthesisSummary,
				RetrievalMethod: "compressed_summary",
			},
		)
	}
	context.Metadata["role_rag_chunk_count"] = len(result.Chunks)
	return context
}

func selectKnowledgeRole(
	chunk retrievalknowledge.RetrievedKnowledge,
) AgentRole {
	classificationText := strings.ToLower(strings.Join([]string{
		chunk.Category,
		strings.Join(chunk.Tags, " "),
		chunk.Title,
		chunk.Source,
		chunk.Content,
	}, " "))
	switch {
	case containsAny(classificationText, "metric", "metrics", "log", "logs", "trace", "traces", "observability", "dashboard", "alert"):
		return AgentRoleEvidence
	case containsAny(classificationText, "diagnosis", "playbook", "rule", "summary", "triage"):
		return AgentRoleTriage
	case containsAny(classificationText, "runbook", "incident", "knowledge", "doc", "document"):
		return AgentRoleKnowledge
	default:
		return AgentRoleKnowledge
	}
}

func summarizeChunksForSynthesis(
	chunks []retrievalknowledge.RetrievedKnowledge,
) string {
	summaries := make([]string, 0, min(len(chunks), 3))
	for _, chunk := range chunks {
		content := boundedSummary(chunk.Title+": "+chunk.Content, 220)
		if content == "" {
			continue
		}
		summaries = append(summaries, content)
		if len(summaries) == 3 {
			break
		}
	}
	return strings.Join(summaries, " | ")
}

func roleRAGPromptChunks(
	chunks []retrievalknowledge.RetrievedKnowledge,
) []map[string]any {
	result := make([]map[string]any, 0, len(chunks))
	for _, chunk := range chunks {
		result = append(result, map[string]any{
			"id":               chunk.ID,
			"chunk_id":         chunk.ChunkID,
			"document_id":      chunk.DocumentID,
			"title":            boundedSummary(chunk.Title, 160),
			"source":           chunk.Source,
			"content":          boundedSummary(chunk.Content, 500),
			"score":            chunk.Score,
			"retrieval_method": chunk.RetrievalMethod,
			"category":         chunk.Category,
			"tags":             chunk.Tags,
		})
	}
	return result
}

func roleRAGChunksAsEvidence(
	chunks []retrievalknowledge.RetrievedKnowledge,
) []common.EvidenceItem {
	evidence := make([]common.EvidenceItem, 0, len(chunks))
	for _, chunk := range chunks {
		score := chunk.Score
		evidence = append(evidence, common.EvidenceItem{
			ID:         chunk.ID,
			SourceType: "knowledge",
			SourceName: chunk.Source,
			Content:    chunk.Content,
			ResourceID: chunk.DocumentID,
			Score:      &score,
			Metadata: map[string]any{
				"title":            chunk.Title,
				"chunk_id":         chunk.ChunkID,
				"document_id":      chunk.DocumentID,
				"retrieval_method": chunk.RetrievalMethod,
				"category":         chunk.Category,
				"role_rag":         true,
			},
		})
	}
	return evidence
}
