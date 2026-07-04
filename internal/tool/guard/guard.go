package guard

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/jiawei-wang-dev/WatchOps-Lite/internal/observability"
	toolruntime "github.com/jiawei-wang-dev/WatchOps-Lite/internal/tool/runtime"
	"go.opentelemetry.io/otel/attribute"
)

const validationErrorType = "validation_error"

var (
	servicePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	traceIDPattern = regexp.MustCompile(`^[A-Fa-f0-9]{16}$|^[A-Fa-f0-9]{32}$`)
)

type Guard struct {
	config  Config
	allowed map[string]struct{}
}

type ValidationResult struct {
	Allowed bool
	Error   *toolruntime.ToolError
}

func New(config Config) *Guard {
	config = Normalize(config)
	allowed := make(map[string]struct{}, len(config.AllowedTools))
	for _, toolName := range config.AllowedTools {
		toolName = strings.TrimSpace(toolName)
		if toolName != "" {
			allowed[toolName] = struct{}{}
		}
	}
	return &Guard{config: config, allowed: allowed}
}

func Default() *Guard {
	return New(DefaultConfig())
}

func (g *Guard) Validate(ctx context.Context, toolName string, input any) ValidationResult {
	ctx, span := observability.StartSpan(
		ctx,
		"tool.guard.validate",
		attribute.String("tool.name", toolName),
	)
	defer span.End()
	result := g.validate(toolName, input)
	validationResult := "allowed"
	if !result.Allowed {
		validationResult = "rejected"
		span.SetAttributes(attribute.String("validation.error_type", validationErrorType))
		observability.MarkError(span, "Tool guard validation failed")
	}
	span.SetAttributes(attribute.String("validation.result", validationResult))
	return result
}

func (g *Guard) validate(toolName string, input any) ValidationResult {
	toolName = strings.TrimSpace(toolName)
	if _, ok := g.allowed[toolName]; !ok {
		return rejected(toolName, "unknown or unsupported tool", map[string]any{
			"error_type": validationErrorType,
		})
	}
	if err := g.validateFields(toolName, input); err != nil {
		return rejected(toolName, err.message, map[string]any{
			"error_type": validationErrorType,
			"field":      err.field,
		})
	}
	return ValidationResult{Allowed: true}
}

type validationError struct {
	field   string
	message string
}

func (g *Guard) validateFields(toolName string, input any) *validationError {
	value := reflect.Indirect(reflect.ValueOf(input))
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return &validationError{field: "input", message: "tool input must be a structured object"}
	}

	// Parameter validation is intentionally shallow and generic: it blocks
	// unbounded queries and malformed identifiers without coupling the guard to
	// every tool's business schema.
	if service, ok := stringField(value, "Service"); ok {
		if err := g.validateService(service); err != nil {
			return err
		}
	}
	if traceID, ok := stringField(value, "TraceID"); ok && strings.TrimSpace(traceID) != "" {
		if !traceIDPattern.MatchString(strings.TrimSpace(traceID)) {
			return &validationError{field: "trace_id", message: "trace_id is invalid"}
		}
	}
	if severity, ok := stringField(value, "Severity"); ok && strings.TrimSpace(severity) != "" {
		switch strings.ToLower(strings.TrimSpace(severity)) {
		case "info", "warning", "critical", "page", "unknown":
		default:
			return &validationError{field: "severity", message: "severity is invalid"}
		}
	}
	if window, ok := stringField(value, "Window"); ok && strings.TrimSpace(window) != "" {
		duration, err := time.ParseDuration(strings.TrimSpace(window))
		if err != nil {
			return &validationError{field: "window", message: "window must be a duration"}
		}
		if duration <= 0 || duration > g.config.MaxWindow {
			return &validationError{field: "window", message: "window exceeds the allowed range"}
		}
	}
	if timeRange, ok := field(value, "TimeRange"); ok {
		if err := g.validateTimeRange(timeRange); err != nil {
			return err
		}
	}
	if topK, ok := intField(value, "TopK"); ok {
		if topK < 1 || topK > g.config.MaxKnowledgeTopK {
			return &validationError{field: "top_k", message: "top_k exceeds the allowed range"}
		}
	}
	if limit, ok := intField(value, "Limit"); ok {
		maxLimit := g.config.MaxGenericLimit
		if toolName == "query_logs" {
			maxLimit = g.config.MaxLogLimit
		}
		if limit < 1 || limit > maxLimit {
			return &validationError{field: "limit", message: "limit exceeds the allowed range"}
		}
	}
	if depth, ok := intField(value, "Depth"); ok {
		if depth < 0 || depth > g.config.MaxTopologyDepth {
			return &validationError{field: "depth", message: "depth exceeds the allowed range"}
		}
	}
	return nil
}

func (g *Guard) validateService(service string) *validationError {
	service = strings.TrimSpace(service)
	if service == "" {
		return &validationError{field: "service", message: "service is required"}
	}
	if len(service) > g.config.MaxServiceLength || !servicePattern.MatchString(service) {
		return &validationError{field: "service", message: "service contains unsupported characters"}
	}
	return nil
}

func (g *Guard) validateTimeRange(value reflect.Value) *validationError {
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return nil
	}
	fromValue, okFrom := stringField(value, "From")
	toValue, okTo := stringField(value, "To")
	if !okFrom || !okTo || strings.TrimSpace(fromValue) == "" || strings.TrimSpace(toValue) == "" {
		return nil
	}
	from, err := time.Parse(time.RFC3339, strings.TrimSpace(fromValue))
	if err != nil {
		return &validationError{field: "time_range", message: "from must be RFC3339"}
	}
	to, err := time.Parse(time.RFC3339, strings.TrimSpace(toValue))
	if err != nil {
		return &validationError{field: "time_range", message: "to must be RFC3339"}
	}
	if to.Before(from) {
		return &validationError{field: "time_range", message: "to must not be before from"}
	}
	if to.Sub(from) > g.config.MaxWindow {
		return &validationError{field: "time_range", message: "time window exceeds the allowed range"}
	}
	return nil
}

func rejected(toolName string, message string, details map[string]any) ValidationResult {
	return ValidationResult{
		Allowed: false,
		Error: toolruntime.NewToolError(
			toolruntime.ErrorCodeInvalidArgument,
			toolName,
			message,
			false,
			details,
		),
	}
}

func field(value reflect.Value, name string) (reflect.Value, bool) {
	current := value.FieldByName(name)
	return current, current.IsValid()
}

func stringField(value reflect.Value, name string) (string, bool) {
	current, ok := field(value, name)
	if !ok || current.Kind() != reflect.String {
		return "", false
	}
	return current.String(), true
}

func intField(value reflect.Value, name string) (int, bool) {
	current, ok := field(value, name)
	if !ok {
		return 0, false
	}
	switch current.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return int(current.Int()), true
	default:
		return 0, false
	}
}

func (r ValidationResult) ErrorString() string {
	if r.Error == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s", r.Error.Code, r.Error.Message)
}
