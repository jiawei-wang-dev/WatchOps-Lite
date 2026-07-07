package processor

import (
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/evidence"
)

type Config struct {
	Enabled        bool
	CitationPrefix string
	Now            func() time.Time
}

func DefaultConfig() Config {
	return Config{
		Enabled:        true,
		CitationPrefix: "E",
		Now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

type ProcessedEvidence struct {
	CitationID string              `json:"citation_id"`
	ID         string              `json:"id"`
	Source     evidence.Source     `json:"source"`
	SourceName string              `json:"source_name,omitempty"`
	Title      string              `json:"title,omitempty"`
	Content    string              `json:"content"`
	ResourceID string              `json:"resource_id,omitempty"`
	TraceID    string              `json:"trace_id,omitempty"`
	Timestamp  *time.Time          `json:"timestamp,omitempty"`
	Score      EvidenceScore       `json:"score"`
	Metadata   map[string]any      `json:"metadata,omitempty"`
	TimeRange  *evidence.TimeRange `json:"time_range,omitempty"`
}

type EvidenceScore struct {
	Relevance  float64 `json:"relevance"`
	Severity   float64 `json:"severity"`
	Freshness  float64 `json:"freshness"`
	Confidence float64 `json:"confidence"`
	Final      float64 `json:"final"`
	Reason     string  `json:"reason,omitempty"`
}

type EvidenceGroup struct {
	Source evidence.Source     `json:"source"`
	Items  []ProcessedEvidence `json:"items"`
}

type EvidenceReport struct {
	Items    []ProcessedEvidence `json:"items"`
	Groups   []EvidenceGroup     `json:"groups"`
	Metadata map[string]any      `json:"metadata,omitempty"`
}

type Processor struct {
	config Config
}

func New(config Config) *Processor {
	defaults := DefaultConfig()
	if config.CitationPrefix == "" {
		config.CitationPrefix = defaults.CitationPrefix
	}
	if config.Now == nil {
		config.Now = defaults.Now
	}
	return &Processor{config: config}
}

func NewDefault() *Processor {
	return New(DefaultConfig())
}
