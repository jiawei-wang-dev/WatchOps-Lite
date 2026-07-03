package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/multiagent"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/transport/http/dto"
)

type multiAgentExecutorStub struct{}

func (multiAgentExecutorStub) Execute(
	_ context.Context,
	command multiagent.Command,
) (multiagent.Result, error) {
	evidence := common.EvidenceItem{
		ID:         "metric-1",
		SourceType: "metrics",
		SourceName: "prometheus",
		Content:    "checkout error rate is elevated",
	}
	return multiagent.Result{
		RequestID: command.RequestID,
		SessionID: command.SessionID,
		TraceID:   "trace-1",
		Output: multiagent.MultiAgentResult{
			Steps: []multiagent.AgentStep{{
				Role:        multiagent.AgentRoleTriage,
				Name:        "Triage Agent",
				Status:      multiagent.AgentStepCompleted,
				EvidenceIDs: []string{},
			}},
			Evidence: []common.EvidenceItem{evidence},
			ToolRuns: []agenteino.ToolRun{{
				Tool:          "query_metrics",
				Success:       true,
				EvidenceCount: 1,
			}},
			FinalAnswer: agenteino.AgentOutput{
				Conclusions: []agenteino.Conclusion{{
					Text:        "checkout error rate is elevated",
					EvidenceIDs: []string{"metric-1"},
				}},
				Evidence: []common.EvidenceItem{evidence},
				Metadata: map[string]any{},
			},
			Metadata: map[string]any{
				"agent_mode":   "multi_agent",
				"orchestrator": "eino_graph",
			},
		},
	}, nil
}

func TestMultiAgentHandleReturnsExistingAnswerSchemaAndSteps(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/chat/multi-agent", func(c *gin.Context) {
		c.Set("request_id", "req-multi")
		NewMultiAgent(multiAgentExecutorStub{}).Handle(c)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/chat/multi-agent",
		bytes.NewBufferString(`{
			"session_id":"multi-session",
			"message":"inspect checkout",
			"time_context":{
				"from":"2026-07-03T00:00:00Z",
				"to":"2026-07-03T00:20:00Z"
			}
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body)
	}
	var response dto.MultiAgentResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Mode != "multi_agent" ||
		response.Metadata["orchestrator"] != "eino_graph" ||
		len(response.AgentSteps) != 1 ||
		len(response.Answer.Conclusion) != 1 ||
		len(response.Evidence) != 1 {
		t.Fatalf("response = %#v", response)
	}
}

func TestMultiAgentHandleRejectsInvalidRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/chat/multi-agent",
		bytes.NewBufferString(`{}`),
	)
	context.Request.Header.Set("Content-Type", "application/json")

	NewMultiAgent(multiAgentExecutorStub{}).Handle(context)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}
