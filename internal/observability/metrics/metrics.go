package metrics

import (
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Collector struct {
	registry                 *prometheus.Registry
	httpRequests             *prometheus.CounterVec
	httpDuration             *prometheus.HistogramVec
	chatRequests             *prometheus.CounterVec
	chatDuration             prometheus.Histogram
	toolCalls                *prometheus.CounterVec
	toolDuration             *prometheus.HistogramVec
	toolErrors               *prometheus.CounterVec
	ragSearchDuration        *prometheus.HistogramVec
	sessionMemoryUnavailable prometheus.Counter
	agentFallback            *prometheus.CounterVec
	summaryFallback          *prometheus.CounterVec
	evalRuns                 *prometheus.CounterVec
}

var (
	defaultMu        sync.RWMutex
	defaultCollector *Collector
)

func New() *Collector {
	registry := prometheus.NewRegistry()
	collector := &Collector{
		registry: registry,
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "watchops_http_requests_total",
			Help: "Total number of HTTP requests handled by WatchOps-Lite.",
		}, []string{"method", "route", "status"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "watchops_http_request_duration_seconds",
			Help:    "WatchOps-Lite HTTP request duration in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
		chatRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "watchops_chat_requests_total",
			Help: "Total number of WatchOps-Lite chat requests.",
		}, []string{"status"}),
		chatDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "watchops_chat_duration_seconds",
			Help:    "WatchOps-Lite chat request duration in seconds.",
			Buckets: prometheus.DefBuckets,
		}),
		toolCalls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "watchops_tool_calls_total",
			Help: "Total number of WatchOps-Lite tool calls.",
		}, []string{"tool", "status"}),
		toolDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "watchops_tool_duration_seconds",
			Help:    "WatchOps-Lite tool call duration in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"tool"}),
		toolErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "watchops_tool_errors_total",
			Help: "Total number of WatchOps-Lite tool errors.",
		}, []string{"tool", "error_code"}),
		ragSearchDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "watchops_rag_search_duration_seconds",
			Help:    "WatchOps-Lite knowledge retrieval duration in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"mode"}),
		sessionMemoryUnavailable: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "watchops_session_memory_unavailable_total",
			Help: "Total number of unavailable session memory operations.",
		}),
		agentFallback: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "watchops_agent_fallback_total",
			Help: "Total number of Agent fallbacks.",
		}, []string{"reason"}),
		summaryFallback: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "watchops_summary_fallback_total",
			Help: "Total number of session summary fallbacks.",
		}, []string{"reason"}),
		evalRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "watchops_eval_runs_total",
			Help: "Total number of rule-based eval runs.",
		}, []string{"status"}),
	}
	registry.MustRegister(
		collector.httpRequests,
		collector.httpDuration,
		collector.chatRequests,
		collector.chatDuration,
		collector.toolCalls,
		collector.toolDuration,
		collector.toolErrors,
		collector.ragSearchDuration,
		collector.sessionMemoryUnavailable,
		collector.agentFallback,
		collector.summaryFallback,
		collector.evalRuns,
	)
	return collector
}

func (c *Collector) Handler() http.Handler {
	return promhttp.HandlerFor(c.registry, promhttp.HandlerOpts{})
}

func SetDefault(collector *Collector) {
	defaultMu.Lock()
	defaultCollector = collector
	defaultMu.Unlock()
}

func current() *Collector {
	defaultMu.RLock()
	collector := defaultCollector
	defaultMu.RUnlock()
	return collector
}

func ObserveHTTPRequest(method, route, status string, duration time.Duration) {
	if collector := current(); collector != nil {
		collector.httpRequests.WithLabelValues(method, route, status).Inc()
		collector.httpDuration.WithLabelValues(method, route).Observe(duration.Seconds())
	}
}

func ObserveChat(success bool, duration time.Duration) {
	if collector := current(); collector != nil {
		status := "success"
		if !success {
			status = "error"
		}
		collector.chatRequests.WithLabelValues(status).Inc()
		collector.chatDuration.Observe(duration.Seconds())
	}
}

func ObserveTool(tool, errorCode string, duration time.Duration) {
	if collector := current(); collector != nil {
		status := "success"
		if errorCode != "" {
			status = "error"
			collector.toolErrors.WithLabelValues(tool, errorCode).Inc()
		}
		collector.toolCalls.WithLabelValues(tool, status).Inc()
		collector.toolDuration.WithLabelValues(tool).Observe(duration.Seconds())
	}
}

func ObserveRAGSearch(mode string, duration time.Duration) {
	if collector := current(); collector != nil {
		collector.ragSearchDuration.WithLabelValues(mode).Observe(duration.Seconds())
	}
}

func IncSessionMemoryUnavailable() {
	if collector := current(); collector != nil {
		collector.sessionMemoryUnavailable.Inc()
	}
}

func IncAgentFallback(reason string) {
	if collector := current(); collector != nil {
		collector.agentFallback.WithLabelValues(reason).Inc()
	}
}

func IncSummaryFallback(reason string) {
	if collector := current(); collector != nil {
		collector.summaryFallback.WithLabelValues(reason).Inc()
	}
}

func IncEvalRun(status string) {
	if collector := current(); collector != nil {
		collector.evalRuns.WithLabelValues(status).Inc()
	}
}
