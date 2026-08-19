package control

import "time"

type Config struct {
	MaxIterations               int
	MaxToolCalls                int
	MaxRepeatedToolCalls        int
	MaxRetries                  int
	MaxConsecutiveToolFailures  int
	TotalExecutionTimeout       time.Duration
	EnableJSONRepairOnce        bool
	EnableRepeatedToolDetection bool
}

func DefaultConfig() Config {
	return Config{
		MaxIterations:               6,
		MaxToolCalls:                12,
		MaxRepeatedToolCalls:        2,
		MaxRetries:                  1,
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
	if config.MaxRepeatedToolCalls <= 0 {
		config.MaxRepeatedToolCalls = defaults.MaxRepeatedToolCalls
	}
	if config.MaxRetries <= 0 {
		config.MaxRetries = defaults.MaxRetries
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
		config.MaxRepeatedToolCalls == 0 &&
		config.MaxRetries == 0 &&
		config.MaxConsecutiveToolFailures == 0 &&
		config.TotalExecutionTimeout == 0 &&
		!config.EnableJSONRepairOnce &&
		!config.EnableRepeatedToolDetection
}
