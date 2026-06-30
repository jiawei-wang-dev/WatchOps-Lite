package chat

import (
	"context"
	"errors"
	"testing"

	agenteino "github.com/jiawei-wang-dev/WatchOps-Lite/internal/agent/eino"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/common"
)

type fakeRunner struct {
	output agenteino.AgentOutput
	err    error
}

func (f fakeRunner) Run(context.Context, agenteino.AgentInput) (agenteino.AgentOutput, error) {
	return f.output, f.err
}

func TestServicePreservesRequestAndSessionIDs(t *testing.T) {
	service := NewService(fakeRunner{output: agenteino.AgentOutput{
		Limitations: []agenteino.Limitation{},
	}})

	result, err := service.Execute(context.Background(), Command{
		RequestID: "req-01",
		SessionID: "ses-01",
		Message:   "show checkout error rate",
		TimeContext: common.TimeRange{
			From: "2026-06-30T00:00:00Z",
			To:   "2026-06-30T00:20:00Z",
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.RequestID != "req-01" || result.SessionID != "ses-01" {
		t.Fatalf("result = %#v, want IDs preserved", result)
	}
}

func TestServiceValidatesNormalizedRequest(t *testing.T) {
	service := NewService(fakeRunner{})

	_, err := service.Execute(context.Background(), Command{
		SessionID: "",
		Message:   "show checkout error rate",
		TimeContext: common.TimeRange{
			From: "2026-06-30T00:00:00Z",
			To:   "2026-06-30T00:20:00Z",
		},
	})

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error = %v, want *ValidationError", err)
	}
	if validationErr.Field != "session_id" {
		t.Fatalf("field = %q, want session_id", validationErr.Field)
	}
}
