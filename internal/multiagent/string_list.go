package multiagent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type StringList []string

func (s *StringList) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		*s = []string{}
		return nil
	}

	var values []string
	if err := json.Unmarshal(trimmed, &values); err == nil {
		*s = StringList(compactStringList(values))
		return nil
	}

	var single string
	if err := json.Unmarshal(trimmed, &single); err == nil {
		single = strings.TrimSpace(single)
		if single == "" {
			*s = []string{}
			return nil
		}
		*s = []string{single}
		return nil
	}

	var raw any
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return fmt.Errorf("string list field must be a JSON string array, string, or null: %w", err)
	}
	return fmt.Errorf("string list field must be a JSON string array, string, or null, got %T", raw)
}

func (s StringList) MarshalJSON() ([]byte, error) {
	return json.Marshal([]string(s))
}

func compactStringList(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}
