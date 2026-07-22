package hooks

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

const (
	// codexServiceName is the OTEL resource service.name the Codex CLI reports.
	codexServiceName = "codex_cli_rs"
	// codexOTELLogsURN types a raw Codex OTEL log row, mirroring the
	// "claude-code:otel:logs" convention.
	codexOTELLogsURN = "codex:otel:logs"
	// codexOTELMetricsURN types a raw Codex OTEL metric data point row.
	codexOTELMetricsURN = "codex:otel:metrics"
)

// isCodexLogsPayload reports whether an OTLP logs payload originated from the
// Codex CLI, identified by its resource service.name. Claude Code reports a
// different service name and is handled by the session-seeding path instead.
func isCodexLogsPayload(payload *gen.LogsPayload) bool {
	if payload == nil {
		return false
	}
	for _, rl := range payload.ResourceLogs {
		if rl == nil {
			continue
		}
		if extractResourceAttribute(rl.Resource, "service.name") == codexServiceName {
			return true
		}
	}
	return false
}

// writeCodexOTELLogsToClickHouse persists every Codex OTEL log record as a raw
// telemetry row, mirroring the Claude raw-log stream (claude-code:otel:logs).
// The rows keep Codex's native attributes (event.name, event.kind, tool_name,
// token counts, ...) so the full event stream — conversation starts, API
// requests, tool decisions/results, prompts — is queryable like Claude's.
// This raw stream is the sole persisted form of Codex OTEL logs AND the sole
// Codex usage source: attribute_metrics_summaries_mv (and the session queries
// in server/internal/telemetry/repo/sessions.go) read token counts directly
// off token-bearing response.completed rows, replacing the deprecated derived
// codex:usage:metrics rows.
//
// Unlike the Claude path there is no session/account attribution here: Codex
// OTEL payloads carry no account identity for attributeSession to key on (see
// codexSessionMetadata), so rows are attributed by user.email only.
func (s *Service) writeCodexOTELLogsToClickHouse(ctx context.Context, payload *gen.LogsPayload, orgID string, projectID string) {
	if s.telemetryLogger == nil || payload == nil {
		return
	}

	parsedProjectID, err := uuid.Parse(projectID)
	if err != nil {
		s.logger.ErrorContext(ctx, "invalid project ID for Codex OTEL logs", attr.SlogError(err))
		return
	}

	toolInfo := telemetry.ToolInfo{
		Name:           "codex",
		OrganizationID: orgID,
		ProjectID:      parsedProjectID.String(),
		ID:             "",
		URN:            codexOTELLogsURN,
		DeploymentID:   "",
		FunctionID:     nil,
	}

	params := make([]telemetry.LogParams, 0)
	// Memoize email resolution: a Codex export batches many records for the
	// same user, so resolve each distinct email once per payload.
	emailToUserID := make(map[string]string)
	for _, resourceLog := range payload.ResourceLogs {
		if resourceLog == nil {
			continue
		}

		resourceAttrs := resourceAttributesMap(resourceLog.Resource)
		resourceServiceName := stringAttr(resourceAttrs, attr.ServiceNameKey)

		for _, scopeLog := range resourceLog.ScopeLogs {
			if scopeLog == nil {
				continue
			}
			for _, logRecord := range scopeLog.LogRecords {
				if logRecord == nil {
					continue
				}

				logAttrs := logAttributesMap(logRecord.Attributes)
				normalizeCodexLogAttributes(logAttrs)

				logAttrs[attr.EventSourceKey] = string(telemetry.EventSourceHook)
				logAttrs[attr.ProjectIDKey] = projectID
				logAttrs[attr.OrganizationIDKey] = orgID
				logAttrs[attr.ResourceURNKey] = codexOTELLogsURN
				logAttrs[attr.HookSourceKey] = "codex"
				logAttrs[attr.ProviderKey] = providerOpenAI

				if body := otelLogBody(logRecord); body != "" {
					logAttrs[attr.LogBodyKey] = body
				}
				if resourceServiceName != "" {
					logAttrs[attr.ServiceNameKey] = resourceServiceName
				}
				if scopeLog.Scope != nil {
					if scopeLog.Scope.Name != nil {
						logAttrs[attribute.Key("otel.scope.name")] = *scopeLog.Scope.Name
					}
					if scopeLog.Scope.Version != nil {
						logAttrs[attribute.Key("otel.scope.version")] = *scopeLog.Scope.Version
					}
				}
				if logRecord.DroppedAttributesCount != nil {
					logAttrs[attribute.Key("otel.log.dropped_attributes_count")] = *logRecord.DroppedAttributesCount
				}
				if traceID := stringPtrVal(logRecord.TraceID); traceID != "" {
					logAttrs[attr.TraceIDKey] = traceID
				}
				if spanID := stringPtrVal(logRecord.SpanID); spanID != "" {
					logAttrs[attr.SpanIDKey] = spanID
				}

				timestamp, observedTimestamp := otelLogTimestamps(logRecord)
				params = append(params, telemetry.WithOTELMetadata(telemetry.LogParams{
					Timestamp:  timestamp,
					ToolInfo:   toolInfo,
					UserInfo:   s.codexOTELUserInfo(ctx, logAttrs, emailToUserID, orgID),
					Attributes: logAttrs,
				}, observedTimestamp, resourceAttrs))
			}
		}
	}

	if err := s.telemetryLogger.LogBulk(ctx, params); err != nil {
		s.logger.ErrorContext(ctx, "failed to write Codex OTEL logs to ClickHouse", attr.SlogError(err))
	}
}

// isCodexMetricsPayload reports whether an OTLP metrics payload originated
// from the Codex CLI, identified by its resource service.name — the metrics
// twin of isCodexLogsPayload.
func isCodexMetricsPayload(payload *gen.MetricsPayload) bool {
	if payload == nil {
		return false
	}
	for _, rm := range payload.ResourceMetrics {
		if rm == nil {
			continue
		}
		if extractResourceAttribute(rm.Resource, "service.name") == codexServiceName {
			return true
		}
	}
	return false
}

// writeCodexMetricsToClickHouse persists each Codex Sum metric data point as a
// telemetry row under codex:otel:metrics. Codex metrics are event counters
// (e.g. codex.sse_event, codex.tool.call) rather than token usage — token
// counts ride on the raw logs stream — so unlike the Claude metrics extractor
// there is no gen_ai.usage.* aggregation here; the metric name, unit, and
// value are stored verbatim. Non-Sum metric kinds are skipped, matching the
// Claude extractor.
func (s *Service) writeCodexMetricsToClickHouse(ctx context.Context, payload *gen.MetricsPayload, orgID string, projectID string) {
	if s.telemetryLogger == nil || payload == nil {
		return
	}

	parsedProjectID, err := uuid.Parse(projectID)
	if err != nil {
		s.logger.ErrorContext(ctx, "invalid project ID for Codex metrics", attr.SlogError(err))
		return
	}

	toolInfo := telemetry.ToolInfo{
		Name:           "codex",
		OrganizationID: orgID,
		ProjectID:      parsedProjectID.String(),
		ID:             "",
		URN:            codexOTELMetricsURN,
		DeploymentID:   "",
		FunctionID:     nil,
	}

	params := make([]telemetry.LogParams, 0)
	emailToUserID := make(map[string]string)
	for _, resourceMetric := range payload.ResourceMetrics {
		if resourceMetric == nil {
			continue
		}

		resourceAttrs := resourceAttributesMap(resourceMetric.Resource)

		for _, scopeMetric := range resourceMetric.ScopeMetrics {
			if scopeMetric == nil {
				continue
			}
			for _, metric := range scopeMetric.Metrics {
				if metric == nil || metric.Name == nil || metric.Sum == nil {
					continue
				}
				for _, dataPoint := range metric.Sum.DataPoints {
					if dataPoint == nil {
						continue
					}

					attrs := logAttributesMap(dataPoint.Attributes)
					normalizeCodexLogAttributes(attrs)

					attrs[attr.EventSourceKey] = string(telemetry.EventSourceHook)
					attrs[attr.LogBodyKey] = *metric.Name
					attrs[attr.ProjectIDKey] = projectID
					attrs[attr.OrganizationIDKey] = orgID
					attrs[attr.ResourceURNKey] = codexOTELMetricsURN
					attrs[attr.HookSourceKey] = "codex"
					attrs[attr.ProviderKey] = providerOpenAI
					attrs[attr.MetricNameKey] = *metric.Name

					if metric.Unit != nil && *metric.Unit != "" {
						attrs[attribute.Key("metric.unit")] = *metric.Unit
					}
					// Preserve the source encoding: doubles stay doubles,
					// integers (string-encoded per OTLP/JSON or raw) become
					// int64. A data point whose value can't be parsed is
					// still recorded — the counter event itself is signal.
					if dataPoint.AsDouble != nil {
						attrs[attribute.Key("metric.value")] = *dataPoint.AsDouble
					} else if n, ok := parseLooseInt64(dataPoint.AsInt); ok {
						attrs[attribute.Key("metric.value")] = n
					}

					timestamp := s.now()
					if dataPoint.TimeUnixNano != nil {
						if n, ok := parseUnixNanoString(*dataPoint.TimeUnixNano); ok {
							timestamp = time.Unix(0, n)
						}
					}

					params = append(params, telemetry.WithOTELMetadata(telemetry.LogParams{
						Timestamp:  timestamp,
						ToolInfo:   toolInfo,
						UserInfo:   s.codexOTELUserInfo(ctx, attrs, emailToUserID, orgID),
						Attributes: attrs,
					}, timestamp, resourceAttrs))
				}
			}
		}
	}

	if err := s.telemetryLogger.LogBulk(ctx, params); err != nil {
		s.logger.ErrorContext(ctx, "failed to write Codex OTEL metrics to ClickHouse", attr.SlogError(err))
	}
}

// normalizeCodexLogAttributes maps Codex's native attribute names onto the
// canonical gen_ai.* keys so Codex rows join the same conversation and model
// dimensions as Claude/Cursor rows (conversation.id also stamps the
// gram_chat_id column via the telemetry logger).
func normalizeCodexLogAttributes(attrs map[attr.Key]any) {
	if conversationID := stringAttr(attrs, attribute.Key("conversation.id")); conversationID != "" {
		attrs[attr.GenAIConversationIDKey] = conversationID
	}
	if model := stringAttr(attrs, attribute.Key("model")); model != "" {
		attrs[attr.GenAIResponseModelKey] = model
	}
}

// codexOTELUserInfo attributes a row to the Gram user resolved from the
// record's user.email, memoizing lookups in emailToUserID across the payload.
func (s *Service) codexOTELUserInfo(ctx context.Context, attrs map[attr.Key]any, emailToUserID map[string]string, orgID string) telemetry.UserInfo {
	email := strings.TrimSpace(stringAttr(attrs, attr.UserEmailKey))
	if email == "" {
		return telemetry.UserInfoByEmail("")
	}

	lookup := conv.NormalizeEmail(email)
	userID, seen := emailToUserID[lookup]
	if !seen {
		userID = s.resolveUserByEmail(ctx, email, orgID)
		emailToUserID[lookup] = userID
	}
	if userID == "" {
		return telemetry.UserInfoByEmail(email)
	}
	return telemetry.UserInfoByIDAndEmail(userID, email)
}
