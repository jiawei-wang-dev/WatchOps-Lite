// Package skills names small, business-level diagnostic routines.
//
// A Skill describes why an on-call investigation would use one or more tools.
// It does not register tools, select tools for Eino, or introduce another
// execution engine.
package skills

import (
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/alerts"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/knowledge"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/logs"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/metrics"
	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/tools/topology"
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
		description: "use query_metrics to inspect error rate, latency, traffic, and saturation.",
		toolNames:   []string{metrics.Name},
	}
}

func LogInvestigationSkill() Skill {
	return Definition{
		name:        "log_investigation",
		description: "use query_logs to inspect errors, timeouts, request IDs, and exception messages.",
		toolNames:   []string{logs.Name},
	}
}

func TraceInspectionSkill() Skill {
	return Definition{
		name:        "trace_inspection",
		description: "use query_traces to inspect slow spans, dependency paths, and bottlenecks.",
		toolNames:   []string{traces.Name},
	}
}

func RunbookLookupSkill() Skill {
	return Definition{
		name:        "runbook_lookup",
		description: "use search_knowledge to retrieve known runbooks and mitigation steps.",
		toolNames:   []string{knowledge.Name},
	}
}

func CheckoutIncidentDiagnosisSkill() Skill {
	return Definition{
		name:        "checkout_incident_diagnosis",
		description: "for checkout incidents, usually inspect metrics, then logs, then traces, then runbook guidance; optionally use query_alerts and get_service_topology for alert and dependency context.",
		toolNames: []string{
			metrics.Name,
			logs.Name,
			traces.Name,
			knowledge.Name,
			alerts.Name,
			topology.Name,
		},
	}
}
