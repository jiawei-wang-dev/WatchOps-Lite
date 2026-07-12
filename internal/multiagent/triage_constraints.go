package multiagent

import (
	"encoding/json"
	"sort"
	"strings"
)

type TriageConstraints struct {
	AllowedServices      []string `json:"allowed_services"`
	DetectedServices     []string `json:"detected_services"`
	AllowedIncidentTypes []string `json:"allowed_incident_types"`
	RequestedLanguage    string   `json:"requested_language"`
}

func BuildTriageConstraints(input Input, fallback TriagePlan) TriageConstraints {
	language := normalizeRequestedLanguage(metadataString(input.Metadata, "requested_language"))
	if language == "" {
		language = normalizeRequestedLanguage(metadataString(input.Metadata, "language"))
	}
	if language == "" {
		language = normalizeRequestedLanguage(fallback.Language)
	}
	if language == "" {
		language = detectRequestedLanguage(input.Message)
	}
	services := supportedServices()
	if service := strings.ToLower(strings.TrimSpace(fallback.Service)); isSupportedService(service) {
		services = append(services, service)
	}
	detectedServices := detectedSupportedServices(input.Message)
	services = append(services, detectedServices...)
	if len(detectedServices) == 0 {
		if service, ok := detectService(input.Message); ok && isSupportedService(service) {
			services = append(services, service)
			detectedServices = append(detectedServices, service)
		}
	}
	return TriageConstraints{
		AllowedServices:      stableUniqueStrings(services),
		DetectedServices:     stableUniqueStrings(detectedServices),
		AllowedIncidentTypes: supportedIncidentTypes(),
		RequestedLanguage:    language,
	}
}

func detectedSupportedServices(message string) []string {
	query := strings.ToLower(message)
	result := []string{}
	for _, service := range supportedServices() {
		if strings.Contains(query, service) {
			result = append(result, service)
		}
	}
	return stableUniqueStrings(result)
}

func supportedIncidentTypes() []string {
	return []string{
		IncidentHighErrorRate,
		IncidentLatency,
		"timeout",
		"dependency_failure",
		IncidentUnknown,
	}
}

func stableUniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func normalizeRequestedLanguage(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "zh", "zh-cn", "zh_cn", "chinese", "simplified_chinese":
		return "zh-CN"
	case "en", "en-us", "en_us", "english":
		return "en-US"
	case "ko", "ko-kr", "ko_kr", "korean":
		return "ko-KR"
	case "ja", "ja-jp", "ja_jp", "japanese":
		return "ja-JP"
	default:
		return ""
	}
}

func detectRequestedLanguage(value string) string {
	if detectLanguage(value) == "zh" {
		return "zh-CN"
	}
	return "en-US"
}

func legacyLanguage(value string) string {
	if strings.HasPrefix(normalizeRequestedLanguage(value), "zh") {
		return "zh"
	}
	return "en"
}

func languageInstruction(language string) string {
	switch normalizeRequestedLanguage(language) {
	case "zh-CN":
		return "All user-visible natural-language content must be Simplified Chinese. Keep service names, metric names, log fields, trace/span IDs, API paths, commands, code, config keys, evidence IDs, and model names unchanged."
	case "ko-KR":
		return "All user-visible natural-language content must be Korean. Keep service names, metric names, log fields, trace/span IDs, API paths, commands, code, config keys, evidence IDs, and model names unchanged."
	case "ja-JP":
		return "All user-visible natural-language content must be Japanese. Keep service names, metric names, log fields, trace/span IDs, API paths, commands, code, config keys, evidence IDs, and model names unchanged."
	default:
		return "All user-visible natural-language content must be English. Keep service names, metric names, log fields, trace/span IDs, API paths, commands, code, config keys, evidence IDs, and model names unchanged."
	}
}

func jsonStringList(values []string) string {
	encoded, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}
