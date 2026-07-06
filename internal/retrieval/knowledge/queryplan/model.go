package queryplan

import (
	"context"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/intent"
	retrievalknowledge "github.com/jiawei-wang-dev/WatchOps-Lite/internal/retrieval/knowledge"
)

type QueryType string

const (
	QueryOriginal   QueryType = "original"
	QueryCanonical  QueryType = "canonical"
	QuerySynonym    QueryType = "synonym_expansion"
	QueryDiagnostic QueryType = "diagnostic"
	QueryStepBack   QueryType = "step_back"
	QueryHyDE       QueryType = "hyde"
)

type RAGQueryPlan struct {
	OriginalQuery string         `json:"original_query"`
	Queries       []RAGSubQuery  `json:"queries"`
	Source        string         `json:"source"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type RAGSubQuery struct {
	Type   QueryType `json:"type"`
	Query  string    `json:"query"`
	Weight float64   `json:"weight"`
	Reason string    `json:"reason,omitempty"`
}

type QueryPlanInput struct {
	UserMessage string
	Intent      intent.IntentResult
	Service     string
	Symptom     string
	Keywords    []string
	Now         time.Time
}

type QueryPlanner interface {
	Plan(ctx context.Context, input QueryPlanInput) (RAGQueryPlan, error)
}

type RAGSubQueryResult struct {
	Query  RAGSubQuery
	Result retrievalknowledge.RetrievalResult
	Error  error
}
