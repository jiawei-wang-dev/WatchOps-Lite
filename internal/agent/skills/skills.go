// Package skills names small, business-level diagnostic routines.
//
// A Skill describes why an on-call investigation would use one or more tools.
// It does not register tools, select tools for Eino, or introduce another
// execution engine.
package skills

import (
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/knowledge"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/logs"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/metrics"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/traces"
)

type Skill interface {
	Name() string
	Description() string
	ToolNames() []string
}

type Definition struct {
	name        string
	description string
	toolNames   []string
}

func (s Definition) Name() string {
	return s.name
}

func (s Definition) Description() string {
	return s.description
}

func (s Definition) ToolNames() []string {
	return append([]string(nil), s.toolNames...)
}

func MetricInspectionSkill() Skill {
	return Definition{
		name:        "metric_inspection",
		description: "Inspect service rates, errors, saturation, and latency signals before forming an incident hypothesis.",
		toolNames:   []string{metrics.Name},
	}
}

func LogInvestigationSkill() Skill {
	return Definition{
		name:        "log_investigation",
		description: "Inspect bounded service logs for errors, timeouts, and request-level details.",
		toolNames:   []string{logs.Name},
	}
}

func TraceInspectionSkill() Skill {
	return Definition{
		name:        "trace_inspection",
		description: "Inspect distributed traces to locate slow or failed operations and dependency paths.",
		toolNames:   []string{traces.Name},
	}
}

func RunbookLookupSkill() Skill {
	return Definition{
		name:        "runbook_lookup",
		description: "Retrieve operational procedures and mitigation guidance from the knowledge base.",
		toolNames:   []string{knowledge.Name},
	}
}

func CheckoutIncidentDiagnosisSkill() Skill {
	return Definition{
		name:        "checkout_incident_diagnosis",
		description: "Describe the checkout investigation sequence from service signals to logs, traces, and runbook guidance.",
		toolNames: []string{
			metrics.Name,
			logs.Name,
			traces.Name,
			knowledge.Name,
		},
	}
}
