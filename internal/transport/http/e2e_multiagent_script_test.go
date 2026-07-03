package httptransport

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMultiAgentDemoScriptChecksJSONSSEAndSingleCompatibility(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		switch request.URL.Path {
		case "/healthz":
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte(`{"status":"ok"}`))
		case "/api/v1/chat/multi-agent":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(multiAgentScriptFixture()))
		case "/api/v1/chat/multi-agent/stream":
			writer.Header().Set("Content-Type", "text/event-stream")
			for _, event := range []string{
				"multi_agent_started",
				"agent_step_started",
				"agent_step_completed",
				"evidence_collected",
				"synthesis_started",
				"final_answer",
				"multi_agent_completed",
			} {
				_, _ = fmt.Fprintf(
					writer,
					"event: %s\ndata: {}\n\n",
					event,
				)
			}
		case "/api/v1/chat":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(
				`{"request_id":"single","answer":{},"tool_runs":[]}`,
			))
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)

	root := transportTestRepoRoot(t)
	stateDir := t.TempDir()
	command := exec.Command(
		"bash",
		filepath.Join(root, "scripts/e2e_multi_agent_check.sh"),
		"--lang",
		"en",
	)
	command.Dir = root
	command.Env = append(
		os.Environ(),
		"WATCHOPS_API_BASE_URL="+server.URL,
		"WATCHOPS_DEMO_STATE_DIR="+stateDir,
		"WATCHOPS_MULTI_AGENT_TIMEOUT_SECONDS=5",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run multi-agent demo script: %v\n%s", err, output)
	}
	for _, expected := range []string{
		"Verified bounded Multi-Agent response",
		"Verified Multi-Agent SSE lifecycle",
		"Verified existing Single-Agent endpoint",
		"Multi-Agent demo check passed (en)",
	} {
		if !strings.Contains(string(output), expected) {
			t.Fatalf("script output does not contain %q:\n%s", expected, output)
		}
	}
}

func multiAgentScriptFixture() string {
	return `{
		"request_id":"multi",
		"session_id":"session",
		"mode":"multi_agent",
		"answer":{
			"conclusion":[{"text":"bounded conclusion","evidence_ids":["ev-1"]}],
			"evidence":[{"id":"ev-1","source_type":"logs","content":"timeout"}],
			"inferences":[],
			"recommendations":[{"text":"inspect dependency","evidence_ids":["ev-1"]}],
			"limitations":[]
		},
		"agent_steps":[
			{"role":"triage","status":"completed"},
			{"role":"evidence","status":"completed"},
			{"role":"knowledge","status":"completed"},
			{"role":"synthesis","status":"completed"}
		],
		"evidence":[{"id":"ev-1","source_type":"logs","content":"timeout"}],
		"tool_runs":[],
		"trace_id":"trace-1",
		"metadata":{}
	}`
}
