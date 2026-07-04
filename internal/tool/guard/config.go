package guard

import "time"

type Config struct {
	AllowedTools     []string
	MaxWindow        time.Duration
	MaxLogLimit      int
	MaxKnowledgeTopK int
	MaxGenericLimit  int
	MaxTopologyDepth int
	MaxServiceLength int
}

func DefaultConfig() Config {
	return Config{
		// The allowlist keeps the Agent in a read-only observability posture even
		// if a future prompt tries to route into an unsupported action.
		AllowedTools: []string{
			"query_metrics",
			"query_logs",
			"query_traces",
			"search_knowledge",
			"query_alerts",
			"get_service_topology",
		},
		MaxWindow:        24 * time.Hour,
		MaxLogLimit:      100,
		MaxKnowledgeTopK: 10,
		MaxGenericLimit:  100,
		MaxTopologyDepth: 3,
		MaxServiceLength: 128,
	}
}

func Normalize(config Config) Config {
	defaults := DefaultConfig()
	if len(config.AllowedTools) == 0 {
		config.AllowedTools = defaults.AllowedTools
	}
	if config.MaxWindow <= 0 {
		config.MaxWindow = defaults.MaxWindow
	}
	if config.MaxLogLimit <= 0 {
		config.MaxLogLimit = defaults.MaxLogLimit
	}
	if config.MaxKnowledgeTopK <= 0 {
		config.MaxKnowledgeTopK = defaults.MaxKnowledgeTopK
	}
	if config.MaxGenericLimit <= 0 {
		config.MaxGenericLimit = defaults.MaxGenericLimit
	}
	if config.MaxTopologyDepth <= 0 {
		config.MaxTopologyDepth = defaults.MaxTopologyDepth
	}
	if config.MaxServiceLength <= 0 {
		config.MaxServiceLength = defaults.MaxServiceLength
	}
	return config
}
