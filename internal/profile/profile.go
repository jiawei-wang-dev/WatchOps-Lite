package profile

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrInvalidArgument = errors.New("invalid user profile")
	ErrNotFound        = errors.New("user profile not found")
	ErrUnavailable     = errors.New("user profile store unavailable")
)

const (
	maxServices        = 10
	maxPreferences     = 5
	maxContextLineSize = 240
)

type Profile struct {
	UserID         string         `json:"user_id"`
	DisplayName    string         `json:"display_name"`
	DefaultService string         `json:"default_service"`
	Services       []string       `json:"services"`
	Timezone       string         `json:"timezone"`
	Preferences    map[string]any `json:"preferences"`
	Metadata       map[string]any `json:"metadata"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

func (p Profile) Validate() error {
	if strings.TrimSpace(p.UserID) == "" {
		return fmt.Errorf("%w: user_id is required", ErrInvalidArgument)
	}
	if len(p.UserID) > 128 || len(p.DisplayName) > 128 ||
		len(p.DefaultService) > 128 || len(p.Timezone) > 64 {
		return fmt.Errorf("%w: profile field exceeds size limit", ErrInvalidArgument)
	}
	if len(p.Services) > maxServices {
		return fmt.Errorf("%w: too many services", ErrInvalidArgument)
	}
	return nil
}

func ContextLines(value Profile) []string {
	lines := make([]string, 0, 4)
	if service := bounded(value.DefaultService, 128); service != "" {
		lines = append(lines, "default_service="+service)
	}
	services := boundedServices(value.Services)
	if len(services) > 0 {
		lines = append(
			lines,
			bounded("services="+strings.Join(services, ","), maxContextLineSize),
		)
	}
	if timezone := bounded(value.Timezone, 64); timezone != "" {
		lines = append(lines, "timezone="+timezone)
	}
	if preferences := preferenceSummary(value.Preferences); preferences != "" {
		lines = append(lines, "preferences="+preferences)
	}
	return lines
}

func boundedServices(values []string) []string {
	result := make([]string, 0, min(len(values), maxServices))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = bounded(value, 128)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
		if len(result) == maxServices {
			break
		}
	}
	return result
}

func preferenceSummary(values map[string]any) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, min(len(keys), maxPreferences))
	for _, originalKey := range keys {
		if len(parts) == maxPreferences {
			break
		}
		key := bounded(originalKey, 48)
		if key == "" {
			continue
		}
		var value string
		switch typed := values[originalKey].(type) {
		case string:
			value = bounded(typed, 80)
		case bool, float64, float32, int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64:
			value = fmt.Sprint(typed)
		default:
			continue
		}
		if value != "" {
			parts = append(parts, key+":"+value)
		}
	}
	return bounded(strings.Join(parts, ","), maxContextLineSize)
}

func bounded(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}
