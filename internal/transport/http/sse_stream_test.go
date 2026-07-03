package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	applicationchat "github.com/jiawei-wang-dev/WatchOps-Lite/internal/application/chat"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/multiagent"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

type concurrentStreamExecutor struct{}

type concurrentMultiAgentStreamExecutor struct{}

func (concurrentStreamExecutor) Execute(
	_ context.Context,
	command applicationchat.Command,
) (applicationchat.Result, error) {
	return concurrentStreamResult(command), nil
}

func (concurrentStreamExecutor) Stream(
	_ context.Context,
	command applicationchat.Command,
	emit applicationchat.StreamEmitter,
) (applicationchat.Result, error) {
	var wait sync.WaitGroup
	for index := 0; index < 128; index++ {
		wait.Add(1)
		go func(value int) {
			defer wait.Done()
			emit(applicationchat.StreamEvent{
				Type: "graph_node_started",
				Data: map[string]any{
					"node":  "parallel_branch_" + strconv.Itoa(value),
					"index": value,
				},
			})
		}(index)
	}
	wait.Wait()
	return concurrentStreamResult(command), nil
}

func concurrentStreamResult(command applicationchat.Command) applicationchat.Result {
	return applicationchat.Result{
		RequestID: command.RequestID,
		SessionID: command.SessionID,
		Agent: agenteino.AgentOutput{
			Conclusions:     []agenteino.Conclusion{},
			Evidence:        nil,
			Inferences:      []agenteino.Inference{},
			Recommendations: []agenteino.Recommendation{},
			Limitations:     []agenteino.Limitation{},
			ToolRuns:        []agenteino.ToolRun{},
			Metadata:        map[string]any{},
		},
	}
}

func (concurrentMultiAgentStreamExecutor) Execute(
	_ context.Context,
	command multiagent.Command,
) (multiagent.Result, error) {
	return concurrentMultiAgentResult(command), nil
}

func (concurrentMultiAgentStreamExecutor) Stream(
	_ context.Context,
	command multiagent.Command,
	emit multiagent.StreamEmitter,
) (multiagent.Result, error) {
	emit(multiagent.StreamEvent{
		Type: "multi_agent_started",
		Data: map[string]any{"request_id": command.RequestID},
	})
	var wait sync.WaitGroup
	for index := 0; index < 128; index++ {
		wait.Add(1)
		go func(value int) {
			defer wait.Done()
			emit(multiagent.StreamEvent{
				Type: "agent_step_completed",
				Data: map[string]any{
					"agent_role": "evidence",
					"index":      value,
				},
			})
		}(index)
	}
	wait.Wait()
	return concurrentMultiAgentResult(command), nil
}

func concurrentMultiAgentResult(command multiagent.Command) multiagent.Result {
	evidence := common.EvidenceItem{
		ID:         "metric-1",
		SourceType: "metrics",
		SourceName: "prometheus",
		Content:    "checkout error rate is elevated",
	}
	return multiagent.Result{
		RequestID: command.RequestID,
		SessionID: command.SessionID,
		Output: multiagent.MultiAgentResult{
			Steps:    []multiagent.AgentStep{},
			Evidence: []common.EvidenceItem{evidence},
			ToolRuns: []agenteino.ToolRun{},
			FinalAnswer: agenteino.AgentOutput{
				Conclusions: []agenteino.Conclusion{{
					Text:        "checkout error rate is elevated",
					EvidenceIDs: []string{"metric-1"},
				}},
				Evidence:        []common.EvidenceItem{evidence},
				Inferences:      []agenteino.Inference{},
				Recommendations: []agenteino.Recommendation{},
				Limitations:     []agenteino.Limitation{},
				ToolRuns:        []agenteino.ToolRun{},
				Metadata:        map[string]any{},
			},
			Metadata: map[string]any{
				"agent_mode":   "multi_agent",
				"orchestrator": "eino_graph",
			},
		},
	}
}

func TestSSESerializesConcurrentEventsOverRealHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	router := NewRouter(
		logger,
		"watchops-lite",
		RouterDependencies{Chat: concurrentStreamExecutor{}},
	)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	request := httptest.NewRequest(
		http.MethodPost,
		server.URL+"/api/v1/chat/stream",
		bytes.NewBufferString(`{
			"session_id":"parallel-stream",
			"message":"inspect checkout",
			"time_context":{
				"from":"2026-07-03T05:00:00Z",
				"to":"2026-07-03T05:20:00Z"
			}
		}`),
	)
	request.RequestURI = ""
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")

	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("stream request failed: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read chunked SSE response: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	if !strings.HasPrefix(
		response.Header.Get("Content-Type"),
		"text/event-stream",
	) {
		t.Fatalf("Content-Type = %q", response.Header.Get("Content-Type"))
	}
	if value := response.Header.Get("Content-Length"); value != "" {
		t.Fatalf("Content-Length = %q, want absent", value)
	}
	if response.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf(
			"X-Accel-Buffering = %q",
			response.Header.Get("X-Accel-Buffering"),
		)
	}

	events := parseSSEFrames(t, string(body))
	if events["graph_node_started"] != 128 ||
		events["final_answer"] != 1 ||
		events["workflow_completed"] != 1 {
		t.Fatalf("event counts = %#v", events)
	}
	finalIndex := strings.Index(string(body), "event: final_answer")
	completedIndex := strings.Index(string(body), "event: workflow_completed")
	if finalIndex < 0 || completedIndex <= finalIndex {
		t.Fatalf("invalid terminal event order:\n%s", body)
	}
}

func TestMultiAgentSSESerializesConcurrentEventsAndFinalOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	router := NewRouter(
		logger,
		"watchops-lite",
		RouterDependencies{
			MultiAgent: concurrentMultiAgentStreamExecutor{},
		},
	)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	request := httptest.NewRequest(
		http.MethodPost,
		server.URL+"/api/v1/chat/multi-agent/stream",
		bytes.NewBufferString(`{
			"session_id":"parallel-multi-stream",
			"message":"inspect checkout",
			"time_context":{
				"from":"2026-07-03T05:00:00Z",
				"to":"2026-07-03T05:20:00Z"
			}
		}`),
	)
	request.RequestURI = ""
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")

	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("stream request failed: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read chunked SSE response: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	events := parseSSEFrames(t, string(body))
	if events["multi_agent_started"] != 1 ||
		events["agent_step_completed"] != 128 ||
		events["final_answer"] != 1 ||
		events["multi_agent_completed"] != 1 {
		t.Fatalf("event counts = %#v", events)
	}
	finalIndex := strings.Index(string(body), "event: final_answer\n")
	completedIndex := strings.Index(
		string(body),
		"event: multi_agent_completed\n",
	)
	if finalIndex < 0 || completedIndex < 0 || finalIndex >= completedIndex {
		t.Fatalf("final/completed ordering is invalid")
	}
}

func parseSSEFrames(t *testing.T, body string) map[string]int {
	t.Helper()
	body = strings.ReplaceAll(body, "\r\n", "\n")
	frames := strings.Split(strings.TrimSpace(body), "\n\n")
	result := map[string]int{}
	for _, frame := range frames {
		lines := strings.Split(frame, "\n")
		if len(lines) != 2 ||
			!strings.HasPrefix(lines[0], "event: ") ||
			!strings.HasPrefix(lines[1], "data: ") {
			t.Fatalf("invalid SSE frame %q", frame)
		}
		eventType := strings.TrimPrefix(lines[0], "event: ")
		var payload map[string]any
		if err := json.Unmarshal(
			[]byte(strings.TrimPrefix(lines[1], "data: ")),
			&payload,
		); err != nil {
			t.Fatalf("invalid SSE data JSON: %v", err)
		}
		result[eventType]++
	}
	return result
}

func TestE2EScriptDetectsCompletedSavedSSE(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "../../.."))
	script := filepath.Join(root, "scripts/e2e_demo_check.sh")
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name: "completed",
			content: "event: final_answer\ndata: {}\n\n" +
				"event: workflow_completed\ndata: {}\n\n",
		},
		{
			name:    "missing completion",
			content: "event: final_answer\ndata: {}\n\n",
			wantErr: true,
		},
		{
			name: "wrong order",
			content: "event: workflow_completed\ndata: {}\n\n" +
				"event: final_answer\ndata: {}\n\n",
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			streamPath := filepath.Join(t.TempDir(), "stream.sse")
			if err := os.WriteFile(
				streamPath,
				[]byte(test.content),
				0o600,
			); err != nil {
				t.Fatalf("write stream fixture: %v", err)
			}
			command := exec.Command("bash", script)
			command.Env = append(
				os.Environ(),
				"WATCHOPS_E2E_CHECK_SSE_FILE="+streamPath,
			)
			err := command.Run()
			if (err != nil) != test.wantErr {
				t.Fatalf("script error = %v, wantErr=%v", err, test.wantErr)
			}
		})
	}
}
