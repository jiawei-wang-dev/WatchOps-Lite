package harness

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

type BadCase struct {
	Suite     string    `json:"suite,omitempty"`
	CaseID    string    `json:"case_id"`
	Stage     string    `json:"stage"`
	Expected  any       `json:"expected"`
	Actual    any       `json:"actual"`
	Reason    string    `json:"reason"`
	TraceID   string    `json:"trace_id,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

var validBadCaseStages = map[string]struct{}{
	"intent": {}, "slot": {}, "clarification": {}, "retrieval": {},
	"tool_selection": {}, "tool_arguments": {}, "evidence": {},
	"agent_execution": {}, "timeout": {},
}

func SortBadCases(cases []BadCase) {
	sort.SliceStable(cases, func(left, right int) bool {
		if cases[left].Stage != cases[right].Stage {
			return cases[left].Stage < cases[right].Stage
		}
		if cases[left].CaseID != cases[right].CaseID {
			return cases[left].CaseID < cases[right].CaseID
		}
		return cases[left].Reason < cases[right].Reason
	})
}

func LoadBadCases(reader io.Reader) ([]BadCase, error) {
	var cases []BadCase
	if err := json.NewDecoder(reader).Decode(&cases); err != nil {
		return nil, fmt.Errorf("decode bad cases: %w", err)
	}
	for index, item := range cases {
		if strings.TrimSpace(item.CaseID) == "" {
			return nil, fmt.Errorf("bad_case[%d].case_id is required", index)
		}
		if _, ok := validBadCaseStages[item.Stage]; !ok {
			return nil, fmt.Errorf("bad_case[%d] has invalid stage %q", index, item.Stage)
		}
	}
	SortBadCases(cases)
	return cases, nil
}

func ReplayCaseIDs(cases []BadCase) map[string]struct{} {
	result := make(map[string]struct{}, len(cases))
	for _, item := range cases {
		result[item.CaseID] = struct{}{}
	}
	return result
}
