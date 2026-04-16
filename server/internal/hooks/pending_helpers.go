package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/telemetry"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
)

// bufferHook stores a hook payload in Redis for later processing using atomic RPUSH
func (s *Service) bufferHook(ctx context.Context, sessionID string, payload *gen.ClaudeHookPayload) error {
	// Use atomic RPUSH operation to append to the list
	// This eliminates the race condition from read-modify-write
	ttl := 5 * time.Minute // TTL for buffered hooks. This is very generous. Could be lower since this can trigger through an unauthenticated endpoint.
	if err := s.cache.ListAppend(ctx, hookPendingCacheKey(sessionID), payload, ttl); err != nil {
		return fmt.Errorf("append hook to list: %w", err)
	}

	s.logger.DebugContext(ctx, "Buffered hook in Redis",
		attr.SlogEvent("hook_buffered"),
	)

	return nil
}

// persistToolCallEvent writes a hook event to ClickHouse with full session context
func (s *Service) persistToolCallEvent(ctx context.Context, payload *gen.ClaudeHookPayload, metadata *SessionMetadata) error {
	attrs := s.buildTelemetryAttributesWithMetadata(ctx, payload, metadata)
	toolName, ok := attrs[attr.ToolNameKey].(string) //  Make sure this comes from here so that we get the parsed tool name
	if !ok {
		return fmt.Errorf("tool name not found in attributes")
	}

	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		return fmt.Errorf("invalid project ID in session metadata: %w", err)
	}

	// Build ToolInfo
	toolInfo := telemetry.ToolInfo{
		Name:           toolName,
		OrganizationID: metadata.GramOrgID,
		ProjectID:      projectID.String(),
		ID:             "",
		URN:            "",
		DeploymentID:   "",
		FunctionID:     nil,
	}

	if s.telemetryLogger != nil {
		s.telemetryLogger.Log(ctx, telemetry.LogParams{
			Timestamp:  time.Now(),
			ToolInfo:   toolInfo,
			Attributes: attrs,
		})

		s.logger.DebugContext(ctx, "Wrote hook to ClickHouse with metadata",
			attr.SlogEvent("hook_written"),
		)
	}

	if payload.HookEventName == "PreToolUse" {
		if err := s.writeToolCallRequestToPG(ctx, payload, metadata); err != nil {
			return fmt.Errorf("write tool call request to PG: %w", err)
		}
	}
	if payload.HookEventName == "PostToolUse" || payload.HookEventName == "PostToolUseFailure" {
		if err := s.writeToolCallResultToPG(ctx, payload, metadata); err != nil {
			return fmt.Errorf("write tool call result to PG: %w", err)
		}
	}

	return nil
}

// buildTelemetryAttributesWithMetadata creates attributes for a hook event with session metadata
func (s *Service) buildTelemetryAttributesWithMetadata(ctx context.Context, payload *gen.ClaudeHookPayload, metadata *SessionMetadata) map[attr.Key]any {
	toolName := ""
	if payload.ToolName != nil {
		toolName = *payload.ToolName
	}

	hookSource := "claude"
	if metadata.ServiceName != "" {
		hookSource = metadata.ServiceName
	}

	attrs := map[attr.Key]any{
		attr.EventSourceKey:    string(telemetry.EventSourceHook),
		attr.ToolNameKey:       toolName,
		attr.HookEventKey:      payload.HookEventName,
		attr.SpanIDKey:         generateSpanID(),
		attr.TraceIDKey:        generateTraceID(),
		attr.LogBodyKey:        fmt.Sprintf("Tool: %s, Hook: %s", toolName, payload.HookEventName),
		attr.UserEmailKey:      metadata.UserEmail,
		attr.ProjectIDKey:      metadata.ProjectID,
		attr.OrganizationIDKey: metadata.GramOrgID,
		attr.HookSourceKey:     hookSource,
	}

	if payload.Error != nil {
		attrs[attr.HookErrorKey] = payload.Error
	}

	if payload.IsInterrupt != nil {
		attrs[attr.HookIsInterruptKey] = *payload.IsInterrupt
	}

	// Parse MCP tool names
	if strings.HasPrefix(toolName, "mcp__") {
		parts := strings.SplitN(toolName, "__", 3)
		if len(parts) == 3 {
			attrs[attr.ToolCallSourceKey] = parts[1]
			attrs[attr.ToolNameKey] = parts[2]
		}
	}

	// Hash toolUseID to create trace ID if available
	if payload.ToolUseID != nil && *payload.ToolUseID != "" {
		attrs[attr.TraceIDKey] = hashToolCallIDToTraceID(*payload.ToolUseID)
	}
	if payload.SessionID != nil {
		attrs[attr.GenAIConversationIDKey] = *payload.SessionID
	}
	if payload.ToolUseID != nil {
		attrs[attr.GenAIToolCallIDKey] = *payload.ToolUseID
	}

	// Stringify ToolInput and ToolResponse to prevent JSON path explosion in ClickHouse
	// When these are stored as nested objects, ClickHouse auto-unflattens dotted keys
	// which creates an explosion of attribute keys in the attributes JSON column
	if payload.ToolInput != nil {
		if jsonBytes, err := json.Marshal(payload.ToolInput); err == nil {
			attrs[attr.GenAIToolCallArgumentsKey] = string(jsonBytes)
		} else {
			s.logger.WarnContext(ctx, "Failed to marshal ToolInput", attr.SlogError(err))
		}
	}
	if payload.ToolResponse != nil {
		if jsonBytes, err := json.Marshal(payload.ToolResponse); err == nil {
			attrs[attr.GenAIToolCallResultKey] = string(jsonBytes)
		} else {
			s.logger.WarnContext(ctx, "Failed to marshal ToolResponse", attr.SlogError(err))
		}
	}

	return attrs
}

// MetricDataPoint represents a single metric aggregated across all data points for a model+session
type MetricDataPoint struct {
	SessionID           string
	Model               string
	UserEmail           string
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	Cost                float64
	TimestampNano       int64
}

// writeMetricsToClickHouse writes Claude Code metrics to ClickHouse telemetry_logs
func (s *Service) writeMetricsToClickHouse(ctx context.Context, payload *gen.MetricsPayload, orgID string, projectID string) {
	if s.telemetryLogger == nil {
		return
	}

	parsedProjectID, err := uuid.Parse(projectID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Invalid project ID for metrics", attr.SlogError(err))
		return
	}

	// Extract metrics from payload
	metrics, err := extractMetricsForClickHouse(payload)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to extract metrics", attr.SlogError(err))
		return
	}

	// Write each metric data point as a separate log entry
	for _, m := range metrics {
		urn := "claude-code:usage:metrics"

		attrs := map[attr.Key]any{
			attr.LogBodyKey:        "Claude Code usage metrics",
			attr.ProjectIDKey:      projectID,
			attr.OrganizationIDKey: orgID,
			attr.ResourceURNKey:    urn,
		}

		// Only include non-zero values
		if m.InputTokens > 0 {
			attrs[attr.GenAIUsageInputTokensKey] = m.InputTokens
		}
		if m.OutputTokens > 0 {
			attrs[attr.GenAIUsageOutputTokensKey] = m.OutputTokens
		}
		if m.CacheReadTokens > 0 {
			attrs[attr.GenAIUsageCacheReadInputTokensKey] = m.CacheReadTokens
		}
		if m.CacheCreationTokens > 0 {
			attrs[attr.GenAIUsageCacheCreationInputTokensKey] = m.CacheCreationTokens
		}
		if m.Cost > 0 {
			attrs[attr.GenAIUsageCostKey] = m.Cost
		}
		if m.Model != "" {
			attrs[attr.GenAIResponseModelKey] = m.Model
		}
		if m.UserEmail != "" {
			attrs[attr.UserEmailKey] = m.UserEmail
		}
		if m.SessionID != "" {
			attrs[attr.GenAIConversationIDKey] = m.SessionID
		}

		toolInfo := telemetry.ToolInfo{
			Name:           "claude-code",
			OrganizationID: orgID,
			ProjectID:      parsedProjectID.String(),
			ID:             "",
			URN:            urn,
			DeploymentID:   "",
			FunctionID:     nil,
		}

		s.telemetryLogger.Log(ctx, telemetry.LogParams{
			Timestamp:  time.Unix(0, m.TimestampNano),
			ToolInfo:   toolInfo,
			Attributes: attrs,
		})
	}

	s.logger.DebugContext(ctx, "Wrote Claude Code metrics to ClickHouse",
		attr.SlogEvent("metrics_written"),
		slog.Int("metric_count", len(metrics)),
	)
}

// extractMetricsForClickHouse converts OTEL metrics payload into aggregated metric data points
// Groups by session ID and model, aggregating all token/cost values
func extractMetricsForClickHouse(payload *gen.MetricsPayload) ([]MetricDataPoint, error) {
	// Map key: sessionID + model
	aggregated := make(map[string]*MetricDataPoint)

	if payload.ResourceMetrics == nil {
		return nil, nil
	}

	for _, resourceMetric := range payload.ResourceMetrics {
		if resourceMetric == nil || resourceMetric.ScopeMetrics == nil {
			continue
		}

		for _, scopeMetric := range resourceMetric.ScopeMetrics {
			if scopeMetric == nil || scopeMetric.Metrics == nil {
				continue
			}

			for _, metric := range scopeMetric.Metrics {
				if metric == nil || metric.Name == nil || metric.Sum == nil {
					continue
				}

				// Validate aggregation temporality is DELTA (1)
				// DELTA = 1 means each data point represents change since last export
				// CUMULATIVE = 2 would require different handling to avoid double-counting
				if metric.Sum.AggregationTemporality == nil || *metric.Sum.AggregationTemporality != 1 {
					temporality := "nil"
					if metric.Sum.AggregationTemporality != nil {
						temporality = fmt.Sprintf("%d", *metric.Sum.AggregationTemporality)
					}
					return nil, fmt.Errorf("unsupported aggregation temporality %s for metric %s (expected 1 for DELTA)", temporality, *metric.Name)
				}

				metricName := *metric.Name

				for _, dataPoint := range metric.Sum.DataPoints {
					if dataPoint == nil {
						continue
					}

					// Extract attributes
					sessionID := extractAttributeString(dataPoint.Attributes, "session.id")
					model := extractAttributeString(dataPoint.Attributes, "model")
					userEmail := extractAttributeString(dataPoint.Attributes, "user.email")
					metricType := extractAttributeString(dataPoint.Attributes, "type")

					// Create key for aggregation
					key := sessionID + "|" + model

					// Get or create aggregated entry
					if aggregated[key] == nil {
						aggregated[key] = &MetricDataPoint{
							SessionID: sessionID,
							Model:     model,
							UserEmail: userEmail,
						}
					}

					entry := aggregated[key]

					// Get the value
					value := float64(0)
					if dataPoint.AsDouble != nil {
						value = *dataPoint.AsDouble
					} else if dataPoint.AsInt != nil {
						value = float64(*dataPoint.AsInt)
					}

					// Update timestamp to latest
					if dataPoint.TimeUnixNano != nil {
						nanoStr := *dataPoint.TimeUnixNano
						// Parse the string to int64
						var nano int64
						fmt.Sscanf(nanoStr, "%d", &nano)
						if nano > entry.TimestampNano {
							entry.TimestampNano = nano
						}
					}

					// Aggregate based on metric name and type
					switch metricName {
					case "claude_code.cost.usage":
						entry.Cost += value
					case "claude_code.token.usage":
						switch metricType {
						case "input":
							entry.InputTokens += int64(value)
						case "output":
							entry.OutputTokens += int64(value)
						case "cacheRead":
							entry.CacheReadTokens += int64(value)
						case "cacheCreation":
							entry.CacheCreationTokens += int64(value)
						}
					}
				}
			}
		}
	}

	// Convert map to slice
	result := make([]MetricDataPoint, 0, len(aggregated))
	for _, dp := range aggregated {
		result = append(result, *dp)
	}

	return result, nil
}

// flushPendingHooks retrieves all buffered hooks for a session and writes them to ClickHouse.
// Conversation events (UserPromptSubmit, Stop) are written to PostgreSQL.
func (s *Service) flushPendingHooks(ctx context.Context, sessionID string, metadata *SessionMetadata) {
	// Use LRANGE to get all payloads from the list atomically
	var payloads []gen.ClaudeHookPayload
	key := hookPendingCacheKey(sessionID)

	if err := s.cache.ListRange(ctx, key, 0, -1, &payloads); err != nil {
		s.logger.DebugContext(ctx, "No pending hooks to flush or error reading list", attr.SlogError(err))
		return
	}

	if len(payloads) == 0 {
		return
	}

	for i := range payloads {
		s.persistHook(ctx, &payloads[i], metadata)
	}

	s.logger.InfoContext(ctx, fmt.Sprintf("Flushed %d pending hooks", len(payloads)))

	// Delete the list after successful processing
	if err := s.cache.Delete(ctx, key); err != nil {
		s.logger.ErrorContext(ctx, "Failed to delete hook buffer", attr.SlogError(err))
	}
}
