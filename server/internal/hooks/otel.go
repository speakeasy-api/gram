package hooks

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

const claudeOTELLogsURN = "claude-code:otel:logs"

// Logs handles authenticated OTEL logs data from Claude Code.
func (s *Service) Logs(ctx context.Context, payload *gen.LogsPayload) error {
	logger := s.logger.With(
		attr.SlogHookSource("claude"),
		attr.SlogHookEvent("Logs"),
	)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		// Auth middleware should have rejected this already; log here so a
		// stray unauthenticated request is still visible per source/event
		// when filtering hook traffic in Datadog.
		logger.WarnContext(ctx, "rejected unauthorized claude OTEL logs request",
			attr.SlogEvent("claude_logs_unauthorized"),
		)
		return oops.E(oops.CodeUnauthorized, nil, "unauthorized")
	}

	orgID := authCtx.ActiveOrganizationID
	projectID := authCtx.ProjectID.String()
	logger = logger.With(
		attr.SlogOrganizationID(orgID),
		attr.SlogProjectID(projectID),
	)

	// Codex reports token usage on its OTEL logs stream (codex.sse_event /
	// response.completed) rather than as metrics like Claude Code. Route those
	// payloads to the usage writer; they carry no Claude session to seed.
	if isCodexLogsPayload(payload) {
		s.writeCodexUsageToClickHouse(ctx, payload, orgID, projectID)
		return nil
	}

	s.writeClaudeOTELLogsToClickHouse(ctx, payload, orgID, projectID)

	sessions := extractSessionMetadata(payload)
	if len(sessions) == 0 {
		logger.WarnContext(ctx, "claude OTEL logs payload contained no session ID",
			attr.SlogEvent("claude_logs_no_session"),
		)
		return nil
	}

	// Resolve each distinct user once per payload. A re-batching collector can
	// repeat the same identity across sessions, so memoize on the normalized
	// email (the same key resolveUserByEmail queries with) to issue the minimal
	// set of database lookups.
	userIDByEmail := make(map[string]string)
	for i := range sessions {
		session := sessions[i]

		userID := ""
		if session.UserEmail != "" {
			lookup := conv.NormalizeEmail(session.UserEmail)
			id, ok := userIDByEmail[lookup]
			if !ok {
				id = s.resolveUserByEmail(ctx, session.UserEmail, orgID)
				userIDByEmail[lookup] = id
			}
			userID = id
		}

		completeMetadata := SessionMetadata{
			SessionID:   session.SessionID,
			ServiceName: session.ServiceName,
			UserEmail:   session.UserEmail,
			UserID:      userID,
			ClaudeOrgID: session.ClaudeOrgID,
			GramOrgID:   orgID,
			ProjectID:   projectID,
		}

		sessionLogger := logger.With(
			attr.SlogServiceName(session.ServiceName),
			attr.SlogGenAIConversationID(session.SessionID),
			attr.SlogAuthUserEmail(session.UserEmail),
		)

		// Process each session independently so a single cache failure does not
		// abort flushing the remaining sessions in the batch.
		if err := s.cache.Set(ctx, sessionCacheKey(completeMetadata.SessionID), completeMetadata, 24*time.Hour); err != nil {
			sessionLogger.ErrorContext(ctx, "Failed to store session metadata",
				attr.SlogEvent("claude_logs_cache_set_failed"),
				attr.SlogError(err),
			)
		}

		s.flushPendingHooks(ctx, completeMetadata.SessionID, &completeMetadata)

		sessionLogger.InfoContext(ctx, "Stored session metadata",
			attr.SlogEvent("session_validated"),
		)
	}

	return nil
}

type claudeLogMetadata struct {
	SessionID   string
	ServiceName string
	UserEmail   string
	ClaudeOrgID string
}

// extractSessionMetadata partitions an OTLP logs payload into one metadata
// entry per distinct session.id, in first-seen order. A single Claude Code CLI
// process emits one session per payload, but an OpenTelemetry Collector or
// gateway can re-batch records from many sessions into one export. Keying by
// session keeps each session's identity isolated so the caller never seeds one
// session with another session's user.email / organization.id.
func extractSessionMetadata(payload *gen.LogsPayload) []claudeLogMetadata {
	if payload == nil {
		return nil
	}

	ordered := make([]claudeLogMetadata, 0)
	indexBySession := make(map[string]int)

	for _, resourceLog := range payload.ResourceLogs {
		if resourceLog == nil {
			continue
		}

		serviceName := extractResourceAttribute(resourceLog.Resource, "service.name")

		for _, scopeLog := range resourceLog.ScopeLogs {
			if scopeLog == nil {
				continue
			}

			for _, logRecord := range scopeLog.LogRecords {
				if logRecord == nil {
					continue
				}

				data := extractLogData(logRecord)
				if data.SessionID == "" {
					continue
				}

				idx, ok := indexBySession[data.SessionID]
				if !ok {
					ordered = append(ordered, claudeLogMetadata{
						SessionID:   data.SessionID,
						ServiceName: serviceName,
						UserEmail:   "",
						ClaudeOrgID: "",
					})
					indexBySession[data.SessionID] = len(ordered) - 1
					idx = len(ordered) - 1
				}

				// Claude Code batches many log records per session, but
				// user.email / organization.id only ride on some event types
				// (e.g. api_request, not tool events). Assign only non-empty
				// values so a later emailless record in the batch does not
				// clobber a value already extracted from an earlier record for
				// the same session. ServiceName uses first-non-empty wins in
				// case a re-batched session spans resources with differing
				// service.name values.
				meta := &ordered[idx]
				if data.UserEmail != "" {
					meta.UserEmail = data.UserEmail
				}
				if data.ClaudeOrgID != "" {
					meta.ClaudeOrgID = data.ClaudeOrgID
				}
				if meta.ServiceName == "" && serviceName != "" {
					meta.ServiceName = serviceName
				}
			}
		}
	}

	return ordered
}

func (s *Service) writeClaudeOTELLogsToClickHouse(ctx context.Context, payload *gen.LogsPayload, orgID string, projectID string) {
	if s.telemetryLogger == nil || payload == nil {
		return
	}

	parsedProjectID, err := uuid.Parse(projectID)
	if err != nil {
		s.logger.ErrorContext(ctx, "invalid project ID for Claude OTEL logs", attr.SlogError(err))
		return
	}

	params := make([]telemetry.LogParams, 0)
	correlationSessionIDs := make(map[string]struct{})
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
				normalizeClaudeLogAttributes(logAttrs)

				logAttrs[attr.EventSourceKey] = string(telemetry.EventSourceHook)
				logAttrs[attr.ProjectIDKey] = projectID
				logAttrs[attr.OrganizationIDKey] = orgID
				logAttrs[attr.ResourceURNKey] = claudeOTELLogsURN
				logAttrs[attr.HookSourceKey] = "claude-code"
				if shouldTriggerClaudePromptCorrelation(logAttrs) {
					correlationSessionIDs[stringAttr(logAttrs, attribute.Key("session.id"))] = struct{}{}
				}

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
					ToolInfo:   claudeOTELLogToolInfo(orgID, parsedProjectID.String()),
					UserInfo:   telemetry.UserInfoByEmail(stringAttr(logAttrs, attr.UserEmailKey)),
					Attributes: logAttrs,
				}, observedTimestamp, resourceAttrs))
			}
		}
	}

	if err := s.telemetryLogger.LogBulk(ctx, params); err != nil {
		s.logger.ErrorContext(ctx, "failed to write Claude OTEL logs to ClickHouse", attr.SlogError(err))
		return
	}
	for sessionID := range correlationSessionIDs {
		s.scheduleClaudePromptCorrelation(ctx, parsedProjectID, sessionIDToUUID(sessionID), sessionID)
	}
}

func shouldTriggerClaudePromptCorrelation(logAttrs map[attr.Key]any) bool {
	return stringAttr(logAttrs, attribute.Key("event.name")) == "user_prompt"
}

func (s *Service) scheduleClaudePromptCorrelation(ctx context.Context, projectID uuid.UUID, chatID uuid.UUID, sessionID string) {
	workflowCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if _, err := background.ExecuteCorrelateClaudePromptsWorkflow(workflowCtx, s.temporalEnv, background.CorrelateClaudePromptsParams{
		ProjectID: projectID,
		ChatID:    chatID,
		SessionID: sessionID,
	}); err != nil {
		s.logger.WarnContext(ctx, "failed to schedule Claude prompt correlation",
			attr.SlogError(err),
			attr.SlogGenAIConversationID(sessionID),
			attr.SlogProjectID(projectID.String()),
		)
	}
}

func claudeOTELLogToolInfo(orgID string, projectID string) telemetry.ToolInfo {
	return telemetry.ToolInfo{
		Name:           "claude-code",
		OrganizationID: orgID,
		ProjectID:      projectID,
		ID:             "",
		URN:            claudeOTELLogsURN,
		DeploymentID:   "",
		FunctionID:     nil,
	}
}

func normalizeClaudeLogAttributes(attrs map[attr.Key]any) {
	if sessionID := stringAttr(attrs, attribute.Key("session.id")); sessionID != "" {
		attrs[attr.GenAIConversationIDKey] = sessionID
	}
	if model := stringAttr(attrs, attribute.Key("model")); model != "" {
		attrs[attr.GenAIResponseModelKey] = model
	}
	if traceID := firstStringAttr(attrs, attribute.Key("trace_id"), attribute.Key("traceId")); traceID != "" {
		attrs[attr.TraceIDKey] = traceID
	}
	if spanID := firstStringAttr(attrs, attribute.Key("span_id"), attribute.Key("spanId")); spanID != "" {
		attrs[attr.SpanIDKey] = spanID
	}
}

// OTELLogData contains extracted data from an OTEL log record
type OTELLogData struct {
	SessionID   string
	UserEmail   string
	ClaudeOrgID string
}

// extractResourceAttribute extracts a specific attribute from OTEL resource
func extractResourceAttribute(resource *gen.OTELResource, key string) string {
	if resource == nil || resource.Attributes == nil {
		return ""
	}
	for _, attr := range resource.Attributes {
		if attr == nil || attr.Value == nil || attr.Value.StringValue == nil {
			continue
		}
		if attr.Key == key {
			return *attr.Value.StringValue
		}
	}
	return ""
}

// extractLogData extracts session data from an OTEL log record
func extractLogData(logRecord *gen.OTELLogRecord) OTELLogData {
	data := OTELLogData{
		SessionID:   "",
		UserEmail:   "",
		ClaudeOrgID: "",
	}

	if logRecord == nil || logRecord.Attributes == nil {
		return data
	}

	for _, attr := range logRecord.Attributes {
		if attr == nil || attr.Value == nil {
			continue
		}

		var value string
		if attr.Value.StringValue != nil {
			value = *attr.Value.StringValue
		}

		switch attr.Key {
		case "session.id":
			data.SessionID = value
		case "user.email":
			data.UserEmail = value
		case "organization.id":
			data.ClaudeOrgID = value
		}
	}

	return data
}

// extractAttributeString extracts a string attribute value by key
func extractAttributeString(attributes []*gen.OTELAttribute, key string) string {
	if attributes == nil {
		return ""
	}

	for _, attr := range attributes {
		if attr == nil || attr.Value == nil || attr.Value.StringValue == nil {
			continue
		}
		if attr.Key == key {
			return *attr.Value.StringValue
		}
	}

	return ""
}

func resourceAttributesMap(resource *gen.OTELResource) map[attr.Key]any {
	attrs := make(map[attr.Key]any)
	if resource == nil {
		return attrs
	}
	for _, a := range resource.Attributes {
		if a == nil || a.Value == nil {
			continue
		}
		if value, ok := otelAttributeValue(a.Value); ok {
			attrs[attribute.Key(a.Key)] = value
		}
	}
	if resource.DroppedAttributesCount != nil {
		attrs[attribute.Key("otel.resource.dropped_attributes_count")] = *resource.DroppedAttributesCount
	}
	return attrs
}

func logAttributesMap(attributes []*gen.OTELAttribute) map[attr.Key]any {
	attrs := make(map[attr.Key]any)
	for _, a := range attributes {
		if a == nil || a.Value == nil {
			continue
		}
		if value, ok := otelAttributeValue(a.Value); ok {
			attrs[attribute.Key(a.Key)] = value
		}
	}
	return attrs
}

func otelAttributeValue(value *gen.OTELAttributeValue) (any, bool) {
	switch {
	case value.StringValue != nil:
		return *value.StringValue, true
	case value.IntValue != nil:
		if n, ok := parseLooseInt64(value.IntValue); ok {
			return n, true
		}
		return value.IntValue, true
	case value.BoolValue != nil:
		return *value.BoolValue, true
	case value.DoubleValue != nil:
		return *value.DoubleValue, true
	case value.ArrayValue != nil:
		return value.ArrayValue, true
	case value.KvlistValue != nil:
		return value.KvlistValue, true
	case value.BytesValue != nil:
		return *value.BytesValue, true
	default:
		return nil, false
	}
}

func otelLogBody(logRecord *gen.OTELLogRecord) string {
	if logRecord == nil || logRecord.Body == nil || logRecord.Body.StringValue == nil {
		return ""
	}
	return *logRecord.Body.StringValue
}

func otelLogTimestamps(logRecord *gen.OTELLogRecord) (time.Time, time.Time) {
	now := time.Now()
	observed := now
	if logRecord != nil && logRecord.ObservedTimeUnixNano != nil {
		if n, ok := parseUnixNanoString(*logRecord.ObservedTimeUnixNano); ok {
			observed = time.Unix(0, n)
		}
	}

	timestamp := observed
	if logRecord != nil && logRecord.TimeUnixNano != nil {
		if n, ok := parseUnixNanoString(*logRecord.TimeUnixNano); ok {
			timestamp = time.Unix(0, n)
		}
	}
	return timestamp, observed
}

func parseUnixNanoString(raw string) (int64, bool) {
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func stringAttr(attrs map[attr.Key]any, key attribute.Key) string {
	if v, ok := attrs[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func firstStringAttr(attrs map[attr.Key]any, keys ...attribute.Key) string {
	for _, key := range keys {
		if value := stringAttr(attrs, key); value != "" {
			return value
		}
	}
	return ""
}

func stringPtrVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
