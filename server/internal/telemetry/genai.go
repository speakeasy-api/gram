package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"go.opentelemetry.io/otel/attribute"
)

// GenAI operation type values.
// These follow the OpenTelemetry GenAI semantic conventions:
// https://opentelemetry.io/docs/specs/semconv/gen-ai/
const (
	GenAIOperationChat        = "chat"
	GenAIOperationExecuteTool = "execute_tool"
)

// ResourceAttributeKeys defines which attribute keys should be stored as resource attributes
// in telemetry logs. Resource attributes describe the entity producing telemetry (service,
// deployment, project) rather than the specific operation.
// Based on OTel semantic conventions: https://opentelemetry.io/docs/specs/semconv/resource/
var ResourceAttributeKeys = map[attribute.Key]struct{}{
	attr.ServiceNameKey:    {},
	attr.DeploymentIDKey:   {},
	attr.ServiceVersionKey: {},
}

// EmitTelemetryLog emits a telemetry log to ClickHouse asynchronously.
// All data is passed via the attrs map. Known keys are extracted to dedicated columns
// (but remain in the attributes JSON). Remaining keys are partitioned into resource
// vs span attributes based on ResourceAttributeKeys.
func EmitTelemetryLog(
	ctx context.Context,
	logger *slog.Logger,
	provider ToolMetricsProvider,
	attrs map[string]any,
) {
	if provider == nil {
		return
	}

	go func() {
		logCtx := context.WithoutCancel(ctx)

		logParams, err := buildTelemetryLogParams(attrs)
		if err != nil {
			logger.ErrorContext(logCtx,
				"failed to build telemetry log params",
				attr.SlogError(err),
			)
			return
		}

		if err := provider.InsertTelemetryLog(logCtx, logParams); err != nil {
			logger.ErrorContext(logCtx,
				"failed to emit telemetry log to ClickHouse",
				attr.SlogError(err),
				attr.SlogResourceURN(logParams.GramURN),
			)
			return
		}

		logger.DebugContext(logCtx,
			"emitted telemetry log",
			attr.SlogResourceURN(logParams.GramURN),
			attr.SlogProjectID(logParams.GramProjectID),
		)
	}()
}

// buildTelemetryLogParams constructs InsertTelemetryLogParams from attributes.
func buildTelemetryLogParams(attrs map[string]any) (repo.InsertTelemetryLogParams, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return repo.InsertTelemetryLogParams{}, fmt.Errorf("generate telemetry log id: %w", err)
	}

	now := time.Now()
	timeUnixNano := now.UnixNano()

	// Extract column values (but keep them in attrs for JSON)
	projectID := getString(attrs, attr.ProjectIDKey)
	deploymentID := getStringPtr(attrs, attr.DeploymentIDKey)
	functionID := getStringPtr(attrs, attr.DeploymentFunctionsIDKey)
	serviceVersion := getStringPtr(attrs, attr.ServiceVersionKey)
	urn := getString(attrs, attr.ResourceURNKey)
	traceID := getStringPtr(attrs, attr.TraceIDKey)
	spanID := getStringPtr(attrs, attr.SpanIDKey)
	severityText := getStringPtr(attrs, attr.LogSeverityKey)
	body := getString(attrs, attr.LogBodyKey)
	httpMethod := getStringPtr(attrs, attr.HTTPRequestMethodKey)
	httpStatusCode := getInt32Ptr(attrs, attr.HTTPResponseStatusCodeKey)
	httpRoute := getStringPtr(attrs, attr.HTTPRouteKey)
	httpServerURL := getStringPtr(attrs, attr.URLFullKey)

	if severityText == nil {
		defaultSeverity := "INFO"
		severityText = &defaultSeverity
	}

	spanAttrs, resourceAttrs := parseAttributes(attrs)

	attrsJSON := "{}"
	if len(spanAttrs) > 0 {
		b, err := json.Marshal(spanAttrs)
		if err != nil {
			return repo.InsertTelemetryLogParams{}, fmt.Errorf("marshal attributes: %w", err)
		}
		attrsJSON = string(b)
	}

	// Marshal resource attributes to JSON
	resAttrsJSON := "{}"
	if len(resourceAttrs) > 0 {
		b, err := json.Marshal(resourceAttrs)
		if err != nil {
			return repo.InsertTelemetryLogParams{}, fmt.Errorf("marshal resource attributes: %w", err)
		}
		resAttrsJSON = string(b)
	}

	return repo.InsertTelemetryLogParams{
		ID:                     id.String(),
		TimeUnixNano:           timeUnixNano,
		ObservedTimeUnixNano:   timeUnixNano,
		SeverityText:           severityText,
		Body:                   body,
		TraceID:                traceID,
		SpanID:                 spanID,
		Attributes:             attrsJSON,
		ResourceAttributes:     resAttrsJSON,
		GramProjectID:          projectID,
		GramDeploymentID:       deploymentID,
		GramFunctionID:         functionID,
		GramURN:                urn,
		ServiceName:            "gram-server",
		ServiceVersion:         serviceVersion,
		HTTPRequestMethod:      httpMethod,
		HTTPResponseStatusCode: httpStatusCode,
		HTTPRoute:              httpRoute,
		HTTPServerURL:          httpServerURL,
	}, nil
}

// getString gets a string value from attrs without removing it.
func getString(attrs map[string]any, key attribute.Key) string {
	if v, ok := attrs[string(key)]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getStringPtr gets a string pointer from attrs without removing it.
func getStringPtr(attrs map[string]any, key attribute.Key) *string {
	s := getString(attrs, key)
	if s == "" {
		return nil
	}
	return &s
}

// getInt32Ptr gets an int32 pointer from attrs without removing it.
func getInt32Ptr(attrs map[string]any, key attribute.Key) *int32 {
	if v, ok := attrs[string(key)]; ok {
		switch n := v.(type) {
		case int:
			i := int32(n) //nolint:gosec // Column values are bounded
			return &i
		case int32:
			return &n
		case int64:
			i := int32(n) //nolint:gosec // Column values are bounded
			return &i
		case float64:
			i := int32(n)
			return &i
		}
	}
	return nil
}

// parseAttributes splits attributes into resource and span attributes
// based on ResourceAttributeKeys.
func parseAttributes(attrs map[string]any) (spanAttrs, resourceAttrs map[string]any) {
	spanAttrs = make(map[string]any)
	resourceAttrs = make(map[string]any)

	for k, v := range attrs {
		if _, ok := ResourceAttributeKeys[attribute.Key(k)]; ok {
			resourceAttrs[k] = v
		} else {
			spanAttrs[k] = v
		}

		if k == string(attr.GenAIRequestModelKey) {
			spanAttrs[string(attr.GenAIProviderNameKey)] = inferProvider(v.(string))
		}
	}

	return spanAttrs, resourceAttrs
}

// inferProvider infers the provider from the model name.
// OpenRouter model names are typically in the format "provider/model-name".
func inferProvider(model string) string {
	if model == "" {
		return "unknown"
	}

	// OpenRouter uses format like "openai/gpt-4o" or "anthropic/claude-3-opus"
	if idx := strings.Index(model, "/"); idx > 0 {
		return model[:idx]
	}

	// Fallback heuristics for direct model names
	lowerModel := strings.ToLower(model)
	switch {
	case strings.HasPrefix(lowerModel, "gpt-"):
		return "openai"
	case strings.HasPrefix(lowerModel, "claude-"):
		return "anthropic"
	case strings.HasPrefix(lowerModel, "gemini-"):
		return "google"
	case strings.HasPrefix(lowerModel, "mistral-"):
		return "mistral"
	default:
		return "openrouter"
	}
}
