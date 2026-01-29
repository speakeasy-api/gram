package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"go.opentelemetry.io/otel/attribute"
)

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

type LogParams struct {
	Timestamp  time.Time
	ToolInfo   ToolInfo
	Attributes map[attr.Key]any
}

func (s *Service) CreateLog(params LogParams) {
	ctx := context.Background()

	enabled, err := s.logsEnabled(ctx, params.ToolInfo.OrganizationID)
	if err != nil || !enabled {
		return
	}

	logParams, err := buildTelemetryLogParams(params)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to build telemetry log params", attr.SlogError(err))
		return
	}

	if err := s.chRepo.InsertTelemetryLog(ctx, *logParams); err != nil {
		s.logger.ErrorContext(ctx, "failed to insert telemetry log", attr.SlogError(err))
	}
}

// buildTelemetryLogParams constructs InsertTelemetryLogParams from attributes.
func buildTelemetryLogParams(params LogParams) (*repo.InsertTelemetryLogParams, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate telemetry log id")
	}

	// we want the core tool info data to also be added as attributes to our
	// attributes object
	allAttrs := make(map[attr.Key]any)
	maps.Copy(allAttrs, params.Attributes)
	maps.Copy(allAttrs, params.ToolInfo.AsAttributes())

	observedTimeUnixNano := time.Now().UnixNano()
	allAttrs[attr.ObservedTimeUnixNanoKey] = observedTimeUnixNano
	allAttrs[attr.TimeUnixNanoKey] = params.Timestamp.UnixNano()

	// Manually add service name, as it's always going to be gram server
	allAttrs[attr.ServiceNameKey] = serviceName

	spanAttrs, resourceAttrs, err := parseAttributes(allAttrs)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "parse log attributes")
	}

	var deploymentID *string
	if params.ToolInfo.DeploymentID != "" {
		deploymentID = &params.ToolInfo.DeploymentID
	}

	return &repo.InsertTelemetryLogParams{
		ID:                   id.String(),
		TimeUnixNano:         params.Timestamp.UnixNano(),
		ObservedTimeUnixNano: observedTimeUnixNano,
		SeverityText:         getSeverityText(allAttrs),
		Body:                 getString(allAttrs, attr.LogBodyKey),
		TraceID:              getStringPtr(allAttrs, attr.TraceIDKey),
		SpanID:               getStringPtr(allAttrs, attr.SpanIDKey),
		Attributes:           spanAttrs,
		ResourceAttributes:   resourceAttrs,
		GramProjectID:        params.ToolInfo.ProjectID,
		GramDeploymentID:     deploymentID,
		GramFunctionID:       params.ToolInfo.FunctionID,
		GramURN:              params.ToolInfo.URN,
		ServiceName:    serviceName,
		ServiceVersion: getStringPtr(allAttrs, attr.ServiceVersionKey),
	}, nil
}

// parseAttributes splits attributes into resource and span attributes
// based on ResourceAttributeKeys, and returns their json string representation.
func parseAttributes(attrs map[attr.Key]any) (spanAttrsJSON, resourceAttrsJSON string, err error) {
	spanAttrs := make(map[attr.Key]any)
	resourceAttrs := make(map[attr.Key]any)

	for k, v := range attrs {
		// if there's an attribute related to a Gen AI request we want
		// to infer the model provider for insights
		if k == attr.GenAIRequestModelKey {
			if model, ok := v.(string); ok {
				spanAttrs[attr.GenAIProviderNameKey] = inferProvider(model)
				continue
			}
		}

		if _, ok := ResourceAttributeKeys[k]; ok {
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

func getSeverityText(attrs map[attr.Key]any) *string {
	defaultSeverity := "INFO"

	severityText := getStringPtr(attrs, attr.LogSeverityKey)
	if severityText != nil {
		return severityText
	}

	code, ok := attrs[attr.HTTPResponseStatusCodeKey].(int64)
	if !ok {
		return &defaultSeverity
	}

	switch {
	case code >= 500:
		errorText := "ERROR"
		return &errorText
	case code >= 400:
		warnText := "WARN"
		return &warnText
	default:
		infoText := "INFO"
		return &infoText
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
