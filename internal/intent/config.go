package intent

import "time"

type Config struct {
	Enabled          bool
	Mode             string
	LLMEnabled       bool
	Timeout          time.Duration
	MinLLMConfidence float64
	EmitStreamEvents bool
}

func DefaultConfig() Config {
	return Config{
		Enabled:          true,
		Mode:             "hybrid",
		LLMEnabled:       false,
		Timeout:          3 * time.Second,
		MinLLMConfidence: 0.55,
		EmitStreamEvents: true,
	}
}

func (c Config) Normalize() Config {
	defaults := DefaultConfig()
	if c.Mode == "" {
		c.Mode = defaults.Mode
	}
	if c.Timeout <= 0 {
		c.Timeout = defaults.Timeout
	}
	if c.MinLLMConfidence <= 0 {
		c.MinLLMConfidence = defaults.MinLLMConfidence
	}
	if c.MinLLMConfidence > 1 {
		c.MinLLMConfidence = 1
	}
	return c
}
