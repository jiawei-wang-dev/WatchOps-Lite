package control

import "time"

type Config struct {
	MaxIterations               int
	MaxToolCalls                int
	MaxConsecutiveToolFailures  int
	TotalExecutionTimeout       time.Duration
	EnableJSONRepairOnce        bool
	EnableRepeatedToolDetection bool
}

func DefaultConfig() Config {
	return Config{
		MaxIterations:               6,
		MaxToolCalls:                12,
		MaxConsecutiveToolFailures:  3,
		TotalExecutionTimeout:       30 * time.Second,
		EnableJSONRepairOnce:        true,
		EnableRepeatedToolDetection: true,
	}
}

func Normalize(config Config) Config {
	defaults := DefaultConfig()
	if config.MaxIterations <= 0 {
		config.MaxIterations = defaults.MaxIterations
	}
	if config.MaxToolCalls <= 0 {
		config.MaxToolCalls = defaults.MaxToolCalls
	}
	if config.MaxConsecutiveToolFailures <= 0 {
		config.MaxConsecutiveToolFailures = defaults.MaxConsecutiveToolFailures
	}
	if config.TotalExecutionTimeout <= 0 {
		config.TotalExecutionTimeout = defaults.TotalExecutionTimeout
	}
	return config
}

func IsZero(config Config) bool {
	return config.MaxIterations == 0 &&
		config.MaxToolCalls == 0 &&
		config.MaxConsecutiveToolFailures == 0 &&
		config.TotalExecutionTimeout == 0 &&
		!config.EnableJSONRepairOnce &&
		!config.EnableRepeatedToolDetection
}
