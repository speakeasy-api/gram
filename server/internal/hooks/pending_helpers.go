package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
)

// bufferHook stores a hook payload in Redis for later processing using atomic RPUSH
func (s *Service) bufferHook(ctx context.Context, sessionID string, payload *gen.ClaudePayload) error {
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

// resolveUserByEmail looks up a connected user by email within an org.
// Returns the user ID if found, or empty string if not found or if email is empty.
func (s *Service) resolveUserByEmail(ctx context.Context, email, orgID string) string {
	lookup := strings.ToLower(strings.TrimSpace(email))
	if lookup == "" {
		return ""
	}
	user, err := usersrepo.New(s.db).GetConnectedUserByEmail(ctx, usersrepo.GetConnectedUserByEmailParams{
		Email:          lookup,
		OrganizationID: orgID,
	})
	if err == nil {
		return user.ID
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		s.logger.WarnContext(ctx, "failed to resolve hook user by email",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
			attr.SlogAuthUserEmail(email),
		)
	}
	return ""
}

// persistToolCallEvent writes a hook event to ClickHouse with full session context
func (s *Service) persistToolCallEvent(ctx context.Context, payload *gen.ClaudePayload, metadata *SessionMetadata) error {
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
func (s *Service) buildTelemetryAttributesWithMetadata(ctx context.Context, payload *gen.ClaudePayload, metadata *SessionMetadata) map[attr.Key]any {
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
	if metadata.UserID == "" {
		metadata.UserID = s.resolveUserByEmail(ctx, metadata.UserEmail, metadata.GramOrgID)
	}
	if metadata.UserID != "" {
		attrs[attr.UserIDKey] = metadata.UserID
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

	// Annotate every MCP-routed tool call with the resolved server
	// identifier (HTTP/SSE URL, stdio command, or — when the snapshot
	// didn't resolve — the `mcp__<server>__` prefix). This runs on every
	// hook event regardless of policy state, because the offline risk
	// batch scanner reads it back by trace_id to populate
	// risk_results.match for shadow_mcp findings: the chat_message alone
	// only carries the tool name, which is too granular to allowlist on.
	// Best-effort — when the MCP list snapshot is missing the attribute
	// just isn't set, and the scanner falls back to its server-prefix
	// guess.
	if serverPrefix, _, isMCP := claudeMCPServerAndTool(toolName); isMCP && payload.SessionID != nil && *payload.SessionID != "" {
		if entries, err := s.getCachedMCPList(ctx, *payload.SessionID); err == nil {
			if v := resolvedMCPMatch(matchCachedMCPEntry(entries, serverPrefix), serverPrefix); v != "" {
				attrs[attr.MCPMatchKey] = v
			}
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

	// Resolve each unique email to a userID once before the loop so multiple
	// data points sharing the same email don't each trigger a DB round-trip.
	emailToUserID := make(map[string]string)
	for _, m := range metrics {
		email := strings.ToLower(strings.TrimSpace(m.UserEmail))
		if email == "" {
			continue
		}
		if _, seen := emailToUserID[email]; seen {
			continue
		}
		user, err := usersrepo.New(s.db).GetConnectedUserByEmail(ctx, usersrepo.GetConnectedUserByEmailParams{
			Email:          email,
			OrganizationID: orgID,
		})
		if err == nil {
			emailToUserID[email] = user.ID
		} else if !errors.Is(err, pgx.ErrNoRows) {
			s.logger.WarnContext(ctx, "failed to resolve hook user by email",
				attr.SlogError(err),
				attr.SlogOrganizationID(orgID),
				attr.SlogAuthUserEmail(m.UserEmail),
			)
		}
	}

	// Write each metric data point as a separate log entry
	for _, m := range metrics {
		urn := "claude-code:usage:metrics"

		attrs := map[attr.Key]any{
			attr.EventSourceKey:    string(telemetry.EventSourceHook),
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
			if userID := emailToUserID[strings.ToLower(strings.TrimSpace(m.UserEmail))]; userID != "" {
				attrs[attr.UserIDKey] = userID
			}
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

	s.logger.DebugContext(ctx, fmt.Sprintf("Wrote %d Claude Code metrics to ClickHouse", len(metrics)),
		attr.SlogEvent("metrics_written"),
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

				// Validate aggregation temporality is DELTA. Per OTLP/JSON, this
				// can arrive as a JSON number (1) or as the enum string form
				// ("AGGREGATION_TEMPORALITY_DELTA").
				if !isDeltaTemporality(metric.Sum.AggregationTemporality) {
					return nil, fmt.Errorf("unsupported aggregation temporality %v for metric %s (expected DELTA)", metric.Sum.AggregationTemporality, *metric.Name)
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
							SessionID:           sessionID,
							Model:               model,
							UserEmail:           userEmail,
							InputTokens:         0,
							OutputTokens:        0,
							CacheReadTokens:     0,
							CacheCreationTokens: 0,
							Cost:                0,
							TimestampNano:       0,
						}
					}

					entry := aggregated[key]

					// Get the value. asDouble is always a JSON number. asInt can
					// arrive as a JSON string ("12345", canonical OTLP/JSON) or a
					// raw number (12345, Claude Code's own exporter); parseLooseInt64
					// handles both shapes.
					value := float64(0)
					if dataPoint.AsDouble != nil {
						value = *dataPoint.AsDouble
					} else if dataPoint.AsInt != nil {
						if n, ok := parseLooseInt64(dataPoint.AsInt); ok {
							value = float64(n)
						}
					}

					// Update timestamp to latest
					if dataPoint.TimeUnixNano != nil {
						if nano, err := strconv.ParseInt(*dataPoint.TimeUnixNano, 10, 64); err == nil && nano > entry.TimestampNano {
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

// parseLooseInt64 coerces a value that arrived as `any` (because the OTLP
// shape was declared as Any in the goa design) into an int64. It accepts:
//   - JSON numbers (decoded as float64) — e.g. {"asInt": 12345}
//   - JSON strings of digits — e.g. {"asInt": "12345"} (canonical OTLP/JSON)
//   - encoding/json json.Number values, for callers that decode with UseNumber
//   - integer types, defensively
//
// Returns (0, false) on anything else, including non-integral floats.
func parseLooseInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case nil:
		return 0, false
	case float64:
		if t != float64(int64(t)) {
			return 0, false
		}
		return int64(t), true
	case float32:
		f := float64(t)
		if f != float64(int64(f)) {
			return 0, false
		}
		return int64(f), true
	case int:
		return int64(t), true
	case int32:
		return int64(t), true
	case int64:
		return t, true
	case json.Number:
		n, err := t.Int64()
		return n, err == nil
	case string:
		n, err := strconv.ParseInt(t, 10, 64)
		return n, err == nil
	}
	return 0, false
}

// isDeltaTemporality returns true if the value represents OTLP DELTA
// aggregation temporality. Accepts both the numeric form (1) and the protobuf
// enum string form ("AGGREGATION_TEMPORALITY_DELTA") that some OTLP/JSON
// emitters use.
func isDeltaTemporality(v any) bool {
	if s, ok := v.(string); ok && s == "AGGREGATION_TEMPORALITY_DELTA" {
		return true
	}
	n, ok := parseLooseInt64(v)
	return ok && n == 1
}

// flushPendingHooks retrieves all buffered hooks for a session and writes them to ClickHouse.
// Conversation events (UserPromptSubmit, Stop) are written to PostgreSQL.
func (s *Service) flushPendingHooks(ctx context.Context, sessionID string, metadata *SessionMetadata) {
	// Use LRANGE to get all payloads from the list atomically
	var payloads []gen.ClaudePayload
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
