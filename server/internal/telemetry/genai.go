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
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"go.opentelemetry.io/otel/attribute"
)

// GenAI operation type values.
// These follow the OpenTelemetry GenAI semantic conventions:
// https://opentelemetry.io/docs/specs/semconv/gen-ai/
const (
	GenAIOperationChat        = "chat"
	GenAIOperationExecuteTool = "execute_tool"
	serviceName               = "gram-server"
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
	attrs map[attr.Key]any,
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

		if err := provider.InsertTelemetryLog(logCtx, *logParams); err != nil {
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
func buildTelemetryLogParams(attrs map[attr.Key]any) (*repo.InsertTelemetryLogParams, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate telemetry log id")
	}

	observedTimeUnixNano := time.Now().UnixNano()
	attrs[attr.ObservedTimeUnixNanoKey] = observedTimeUnixNano

	// If time is present in attrs, use that; otherwise use the observed time
	var timeUnixNano int64
	if tun, ok := attrs[attr.TimeUnixNanoKey]; ok {
		timeUnixNano = getInt64(tun)
	} else {
		timeUnixNano = observedTimeUnixNano
		attrs[attr.TimeUnixNanoKey] = observedTimeUnixNano
	}

	// Default severity to INFO if not provided
	severityText := getStringPtr(attrs, attr.LogSeverityKey)
	if severityText == nil {
		defaultSeverity := "INFO"
		severityText = &defaultSeverity
	}

	// Manually add service name, as it's always going to be gram server
	attrs[attr.ServiceNameKey] = serviceName

	spanAttrs, resourceAttrs, err := parseAttributes(attrs)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "parse log attributes")
	}

	return &repo.InsertTelemetryLogParams{
		ID:                     id.String(),
		TimeUnixNano:           timeUnixNano,
		ObservedTimeUnixNano:   observedTimeUnixNano,
		SeverityText:           severityText,
		Body:                   getString(attrs, attr.LogBodyKey),
		TraceID:                getStringPtr(attrs, attr.TraceIDKey),
		SpanID:                 getStringPtr(attrs, attr.SpanIDKey),
		Attributes:             spanAttrs,
		ResourceAttributes:     resourceAttrs,
		GramProjectID:          getString(attrs, attr.ProjectIDKey),
		GramDeploymentID:       getStringPtr(attrs, attr.DeploymentIDKey),
		GramFunctionID:         getStringPtr(attrs, attr.FunctionIDKey),
		GramURN:                getString(attrs, attr.ResourceURNKey),
		ServiceName:            serviceName,
		ServiceVersion:         getStringPtr(attrs, attr.ServiceVersionKey),
		HTTPRequestMethod:      getStringPtr(attrs, attr.HTTPRequestMethodKey),
		HTTPResponseStatusCode: getInt32Ptr(attrs, attr.HTTPResponseStatusCodeKey),
		HTTPRoute:              getStringPtr(attrs, attr.HTTPRouteKey),
		HTTPServerURL:          getStringPtr(attrs, attr.URLFullKey),
	}, nil
}

// parseAttributes splits attributes into resource and span attributes
// based on ResourceAttributeKeys.
func parseAttributes(attrs map[attr.Key]any) (spanAttrsJSON, resourceAttrsJSON string, err error) {
	spanAttrs := make(map[attr.Key]any)
	resourceAttrs := make(map[attr.Key]any)

	for k, v := range attrs {
		if k == attr.GenAIRequestModelKey {
			if model, ok := v.(string); ok {
				spanAttrs[attr.GenAIProviderNameKey] = inferProvider(model)
				continue
			}
		}

		if _, ok := ResourceAttributeKeys[attribute.Key(k)]; ok {
			resourceAttrs[k] = v
			continue
		}

		spanAttrs[k] = v
	}

	attrsJSON := "{}"
	if len(spanAttrs) > 0 {
		b, err := json.Marshal(spanAttrs)
		if err != nil {
			return "", "", fmt.Errorf("marshal attributes: %w", err)
		}
		attrsJSON = string(b)
	}

	// Marshal resource attributes to JSON
	resAttrsJSON := "{}"
	if len(resourceAttrs) > 0 {
		b, err := json.Marshal(resourceAttrs)
		if err != nil {
			return "", "", fmt.Errorf("marshal resource attributes: %w", err)
		}
		resAttrsJSON = string(b)
	}

	return attrsJSON, resAttrsJSON, nil
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

// getInt64 converts a value to int64, handling various numeric types.
func getInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}

// getString gets a string value from attrs without removing it.
func getString(attrs map[attr.Key]any, key attribute.Key) string {
	if v, ok := attrs[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getStringPtr gets a string pointer from attrs without removing it.
func getStringPtr(attrs map[attr.Key]any, key attribute.Key) *string {
	s := getString(attrs, key)
	if s == "" {
		return nil
	}
	return &s
}

// getInt32Ptr gets an int32 pointer from attrs without removing it.
func getInt32Ptr(attrs map[attr.Key]any, key attribute.Key) *int32 {
	if v, ok := attrs[key]; ok {
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
