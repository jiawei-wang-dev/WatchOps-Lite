package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAgentEvalDemoRunsOnlyCaseCreatedByCurrentDemo(t *testing.T) {
	const currentCaseID = "eval_current_demo"
	runRequests := make(chan map[string]any, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/eval/cases", func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodPost {
			_, _ = writer.Write([]byte(
				`{"case_id":"` + currentCaseID + `","status":"created"}`,
			))
			return
		}
		_, _ = writer.Write([]byte(`{"cases":[]}`))
	})
	mux.HandleFunc("/api/v1/eval/runs", func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode eval run request: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		runRequests <- body
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte(
			`{"run_id":"run_current_demo","case_type":"bad_case",` +
				`"status":"completed","total":1,"passed":1,"failed":0}`,
		))
	})
	mux.HandleFunc("/api/v1/eval/runs/run_current_demo/results", func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(
			`{"results":[{"case_id":"` + currentCaseID + `"}]}`,
		))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	stateDir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(stateDir, "feedback-response.json"),
		[]byte(`{"feedback_id":"feedback_current_demo"}`),
		0o600,
	); err != nil {
		t.Fatalf("write feedback response: %v", err)
	}
	root := transportTestRepoRoot(t)
	runDemoScript(t, filepath.Join(root, "scripts/demo_eval_case.sh"), server.URL, stateDir)
	output := runDemoScript(
		t,
		filepath.Join(root, "scripts/demo_eval_run.sh"),
		server.URL,
		stateDir,
	)

	request := <-runRequests
	if request["case_type"] != "bad_case" || request["limit"] != float64(1) {
		t.Fatalf("eval run request = %#v", request)
	}
	if !strings.Contains(
		output,
		"Verified Agent eval scope: current case "+currentCaseID+" only.",
	) {
		t.Fatalf("eval script output:\n%s", output)
	}
}

func TestAgentEvalDemoReportsRequestTimeout(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/eval/runs", func(
		_ http.ResponseWriter,
		_ *http.Request,
	) {
		time.Sleep(2 * time.Second)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	stateDir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(stateDir, "eval-case-response.json"),
		[]byte(`{"case_id":"eval_timeout_demo"}`),
		0o600,
	); err != nil {
		t.Fatalf("write eval case response: %v", err)
	}
	script := filepath.Join(
		transportTestRepoRoot(t),
		"scripts/demo_eval_run.sh",
	)
	command := exec.Command("bash", script)
	command.Env = append(
		os.Environ(),
		"WATCHOPS_API_BASE_URL="+server.URL,
		"WATCHOPS_DEMO_STATE_DIR="+stateDir,
		"WATCHOPS_AGENT_EVAL_TIMEOUT_SECONDS=1",
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("eval script unexpectedly succeeded:\n%s", output)
	}
	if !strings.Contains(
		string(output),
		"Agent eval request failed (curl exit 28).",
	) || !strings.Contains(string(output), "allowed 1s") {
		t.Fatalf("timeout output:\n%s", output)
	}
}

func runDemoScript(
	t *testing.T,
	script string,
	apiBaseURL string,
	stateDir string,
) string {
	t.Helper()
	command := exec.Command("bash", script)
	command.Env = append(
		os.Environ(),
		"WATCHOPS_API_BASE_URL="+apiBaseURL,
		"WATCHOPS_DEMO_STATE_DIR="+stateDir,
		"WATCHOPS_AGENT_EVAL_TIMEOUT_SECONDS=5",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s", script, err, output)
	}
	return string(output)
}

func transportTestRepoRoot(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "../../.."))
}
