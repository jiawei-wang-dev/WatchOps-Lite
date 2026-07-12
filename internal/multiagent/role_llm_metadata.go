package multiagent

import "strings"

type roleLLMMetadataInput struct {
	Role           AgentRole
	Model          string
	Attempted      bool
	Success        bool
	Call           llmCallResult
	Fallback       bool
	FallbackReason string
	AnalysisMode   string
}

func roleLLMMetadata(input roleLLMMetadataInput) map[string]any {
	role := string(input.Role)
	analysisMode := strings.TrimSpace(input.AnalysisMode)
	if input.Success && input.Call.retryCount > 0 {
		analysisMode = "llm_repaired"
	}
	if analysisMode == "" {
		switch {
		case input.Success && input.Call.retryCount > 0:
			analysisMode = "llm_repaired"
		case input.Success:
			analysisMode = "llm"
		case input.Fallback:
			analysisMode = "fallback"
		default:
			analysisMode = "not_used"
		}
	}
	model := strings.TrimSpace(input.Model)
	errorCode := strings.TrimSpace(input.Call.errorCode)
	errorMessage := strings.TrimSpace(input.Call.errorMessage)
	timeoutMS := int64(0)
	if input.Call.timeout > 0 {
		timeoutMS = input.Call.timeout.Milliseconds()
	}
	primaryTimeoutMS := int64(0)
	if input.Call.primaryTimeout > 0 {
		primaryTimeoutMS = input.Call.primaryTimeout.Milliseconds()
	}
	repairTimeoutMS := int64(0)
	if input.Call.repairTimeout > 0 {
		repairTimeoutMS = input.Call.repairTimeout.Milliseconds()
	}
	metadata := map[string]any{
		"llm_attempted":                  input.Attempted,
		"llm_success":                    input.Success,
		"llm_elapsed_ms":                 input.Call.durationMS,
		"llm_timeout_ms":                 timeoutMS,
		"llm_retry_count":                input.Call.retryCount,
		"primary_llm_attempted":          input.Call.primaryAttempted,
		"primary_llm_success":            input.Call.primarySuccess,
		"primary_llm_elapsed_ms":         input.Call.primaryMS,
		"primary_llm_timeout_ms":         primaryTimeoutMS,
		"primary_error_code":             strings.TrimSpace(input.Call.primaryErrorCode),
		"primary_error_message":          strings.TrimSpace(input.Call.primaryErrorMessage),
		"repair_attempted":               input.Call.repairAttempted,
		"repair_success":                 input.Call.repairSuccess,
		"repair_llm_elapsed_ms":          input.Call.repairMS,
		"repair_llm_timeout_ms":          repairTimeoutMS,
		"repair_error_code":              strings.TrimSpace(input.Call.repairErrorCode),
		"repair_error_message":           strings.TrimSpace(input.Call.repairErrorMessage),
		"repair_skip_reason":             strings.TrimSpace(input.Call.repairSkipReason),
		"llm_primary_elapsed_ms":         input.Call.primaryMS,
		"llm_repair_elapsed_ms":          input.Call.repairMS,
		"recovery_attempted":             input.Call.retryCount > 0,
		"recovery_success":               input.Success && input.Call.retryCount > 0,
		"recovery_reason":                strings.TrimSpace(input.Call.recoveryReason),
		"llm_error_code":                 errorCode,
		"llm_error_message":              errorMessage,
		"fallback":                       input.Fallback,
		"fallback_reason":                strings.TrimSpace(input.FallbackReason),
		"model":                          model,
		"analysis_mode":                  analysisMode,
		role + "_llm_attempted":          input.Attempted,
		role + "_llm_used":               input.Success,
		role + "_llm_success":            input.Success,
		role + "_llm_duration_ms":        input.Call.durationMS,
		role + "_llm_elapsed_ms":         input.Call.durationMS,
		role + "_llm_timeout_ms":         timeoutMS,
		role + "_llm_retry_count":        input.Call.retryCount,
		role + "_primary_llm_attempted":  input.Call.primaryAttempted,
		role + "_primary_llm_success":    input.Call.primarySuccess,
		role + "_primary_llm_elapsed_ms": input.Call.primaryMS,
		role + "_primary_llm_timeout_ms": primaryTimeoutMS,
		role + "_primary_error_code":     strings.TrimSpace(input.Call.primaryErrorCode),
		role + "_primary_error_message":  strings.TrimSpace(input.Call.primaryErrorMessage),
		role + "_repair_attempted":       input.Call.repairAttempted,
		role + "_repair_success":         input.Call.repairSuccess,
		role + "_repair_llm_elapsed_ms":  input.Call.repairMS,
		role + "_repair_llm_timeout_ms":  repairTimeoutMS,
		role + "_repair_error_code":      strings.TrimSpace(input.Call.repairErrorCode),
		role + "_repair_error_message":   strings.TrimSpace(input.Call.repairErrorMessage),
		role + "_repair_skip_reason":     strings.TrimSpace(input.Call.repairSkipReason),
		role + "_llm_primary_elapsed_ms": input.Call.primaryMS,
		role + "_llm_repair_elapsed_ms":  input.Call.repairMS,
		role + "_recovery_attempted":     input.Call.retryCount > 0,
		role + "_recovery_success":       input.Success && input.Call.retryCount > 0,
		role + "_recovery_reason":        strings.TrimSpace(input.Call.recoveryReason),
		role + "_llm_error_code":         errorCode,
		role + "_llm_error_message":      errorMessage,
		role + "_model":                  model,
		role + "_fallback_used":          input.Fallback,
		role + "_fallback":               input.Fallback,
		role + "_fallback_reason":        strings.TrimSpace(input.FallbackReason),
		role + "_mode":                   analysisMode,
		role + "_analysis_mode":          analysisMode,
	}
	return metadata
}

func roleLLMNotConfiguredMetadata(role AgentRole, fallbackReason string) map[string]any {
	return roleLLMMetadata(roleLLMMetadataInput{
		Role:           role,
		Attempted:      false,
		Success:        false,
		Call:           llmCallResult{errorCode: "not_configured"},
		Fallback:       true,
		FallbackReason: fallbackReason,
		AnalysisMode:   "rule_based",
	})
}
