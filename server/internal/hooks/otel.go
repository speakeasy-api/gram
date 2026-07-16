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
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
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

	sessions := extractSessionMetadata(payload)

	// Resolve and attribute each session before writing the raw OTEL log rows so
	// those rows can be stamped with the account attribution (provider,
	// external_org_id, account_type, device_id). Build a per-session lookup the
	// ClickHouse writer consumes. Resolve each distinct user once per payload: a
	// re-batching collector can repeat the same identity across sessions, so
	// memoize on the normalized email (the same key resolveUserByEmail queries
	// with) to issue the minimal set of database lookups.
	attributionBySession := make(map[string]SessionMetadata, len(sessions))
	userIDByEmail := make(map[string]string)
	for i := range sessions {
		session := sessions[i]

		// Attribution — email resolution, classification, and the user_accounts /
		// device_owners upserts — only needs to run once per session, not on every
		// OTEL export batch (Claude exports every few seconds, which would re-issue
		// the identical writes for the session's whole lifetime). If an earlier
		// batch already attributed this session, reuse the cached result: the hot
		// ingest path then does no Postgres work and only the cheap per-row
		// ClickHouse stamping below runs each batch.
		//
		// Exception: re-attribute when this batch carries an identity field the
		// cached attribution lacks. A collector can split a session's records so a
		// later batch is the first to carry the work email or provider org id;
		// short-circuiting there would freeze a session first seen without an email
		// as personal and never persist the late-arriving email / external_org_id
		// or teach the device bridge (DNO-360).
		//
		// A company-credential session (no provider account UUID) never gets a
		// UserAccountID — there is no account entity to persist — so for those
		// sessions the fast path keys on the resolved AccountType instead, or they
		// would re-attribute on every batch for their lifetime. A UUID-bearing
		// session must still present a persisted UserAccountID: attribution stamps
		// AccountType before the user_accounts upsert, so keying on AccountType
		// alone would freeze a session whose entity persistence transiently failed
		// (classified but never persisted, linked, or billing-resolved) instead of
		// retrying on the next batch.
		var cached SessionMetadata
		if err := s.cache.Get(ctx, sessionCacheKey(session.SessionID), &cached); err == nil &&
			(cached.UserAccountID != "" || (cached.AccountType != "" && cached.ExternalAccountUUID == "")) &&
			!sessionEnrichesAttribution(session, cached) {
			attributionBySession[session.SessionID] = cached
			continue
		}

		// Merge this batch's identity over anything an earlier (incomplete) batch
		// cached, so a session split across batches attributes on the union of its
		// identity rather than only the fields this single batch happened to carry.
		userEmail := conv.Default(session.UserEmail, cached.UserEmail)
		userID := ""
		if userEmail != "" {
			lookup := conv.NormalizeEmail(userEmail)
			id, ok := userIDByEmail[lookup]
			if !ok {
				id = s.resolveUserByEmail(ctx, userEmail, orgID)
				userIDByEmail[lookup] = id
			}
			userID = id
		}

		completeMetadata := SessionMetadata{
			SessionID:           session.SessionID,
			ServiceName:         conv.Default(session.ServiceName, cached.ServiceName),
			UserEmail:           userEmail,
			UserID:              userID,
			Provider:            providerAnthropic,
			ExternalOrgID:       conv.Default(session.ExternalOrgID, cached.ExternalOrgID),
			ExternalAccountUUID: conv.Default(session.ExternalAccountUUID, cached.ExternalAccountUUID),
			ExternalAccountID:   conv.Default(session.ExternalAccountID, cached.ExternalAccountID),
			DeviceID:            conv.Default(session.DeviceID, cached.DeviceID),
			AccountType:         "",
			BillingMode:         "",
			UserAccountID:       "",
			// On this path user.email is the account's own report, so it doubles
			// as the observed email consumers keep separate from actor identity.
			ObservedUserEmail: userEmail,
			GramOrgID:         orgID,
			ProjectID:         projectID,
		}

		sessionLogger := logger.With(
			attr.SlogServiceName(session.ServiceName),
			attr.SlogGenAIConversationID(session.SessionID),
			attr.SlogAuthUserEmail(session.UserEmail),
		)

		_, metadataErr := s.getSessionMetadata(ctx, completeMetadata.SessionID)

		// Attribute the account: classify team vs personal, link it to the
		// owning employee (directly for team accounts, via the device bridge for
		// personal ones), and persist the account entity. Failures are
		// non-fatal — session capture/enforcement must continue regardless.
		if err := s.attributeSession(ctx, &completeMetadata); err != nil {
			sessionLogger.WarnContext(ctx, "failed to attribute AI account for session",
				attr.SlogEvent("account_attribution_failed"),
				attr.SlogError(err),
			)
		}

		attributionBySession[completeMetadata.SessionID] = completeMetadata

		// A session's chat row is created once, on its first persisted message.
		// When that message beat this attribution (hooks and OTEL race at session
		// start), the chat exists without its account link and no later hook
		// revisits it — so backfill the link here. Fill-once in SQL; a no-op when
		// the chat does not exist yet or is already linked. At most one write per
		// attributed session, since only the attribution path runs this.
		linkFailed := false
		if completeMetadata.UserAccountID != "" {
			if _, err := s.repo.LinkChatUserAccount(ctx, repo.LinkChatUserAccountParams{
				UserAccountID: conv.StringToNullUUID(completeMetadata.UserAccountID),
				ID:            sessionIDToUUID(completeMetadata.SessionID),
				ProjectID:     *authCtx.ProjectID,
			}); err != nil {
				linkFailed = true
				sessionLogger.ErrorContext(ctx, "failed to backfill chat account link",
					attr.SlogEvent("chat_account_link_backfill_failed"),
					attr.SlogError(err),
				)
			}
		}

		// Cache only after the backfill: the once-per-session fast path above
		// keys on the cached UserAccountID (or, for a company-credential session
		// with no account entity, the resolved AccountType), so caching an
		// attribution whose backfill just failed would freeze the chat unlinked
		// forever. Skipping
		// the write keeps this batch's row stamping (attributionBySession) and
		// lets the next batch re-attribute and retry the link. Process each
		// session independently so a single cache failure does not abort
		// flushing the remaining sessions in the batch.
		if !linkFailed {
			if err := s.cache.Set(ctx, sessionCacheKey(completeMetadata.SessionID), completeMetadata, 24*time.Hour); err != nil {
				sessionLogger.ErrorContext(ctx, "Failed to store session metadata",
					attr.SlogEvent("claude_logs_cache_set_failed"),
					attr.SlogError(err),
				)
			}
		}
		if metadataErr != nil {
			entries, err := s.getCachedMCPList(ctx, completeMetadata.SessionID)
			if err == nil {
				s.upsertShadowMCPInventoryURLs(ctx, completeMetadata.GramOrgID, completeMetadata.ProjectID, completeMetadata.SessionID, entries)
			} else {
				sessionLogger.WarnContext(ctx, "failed to read cached MCP list for shadow inventory capture",
					attr.SlogEvent("claude_otel_mcp_list_cache_miss"),
					attr.SlogError(err),
				)
			}
		}

		s.flushPendingHooks(ctx, completeMetadata.SessionID, &completeMetadata)

		sessionLogger.InfoContext(ctx, "Stored session metadata",
			attr.SlogEvent("session_validated"),
		)
	}

	s.writeClaudeOTELLogsToClickHouse(ctx, payload, orgID, projectID, attributionBySession)

	if len(sessions) == 0 {
		logger.WarnContext(ctx, "claude OTEL logs payload contained no session ID",
			attr.SlogEvent("claude_logs_no_session"),
		)
	}

	return nil
}

// sessionEnrichesAttribution reports whether this OTEL batch carries an identity
// field the cached attribution is missing. Claude normally emits every identity
// attribute on a session's first batch, but an OpenTelemetry Collector can
// re-batch records so a later batch is the first to carry e.g. the work email or
// provider org id. When that happens the once-per-session fast path must yield so
// re-running attribution can reclassify a session first seen as personal into
// team, persist the late-arriving email / external_org_id, and teach the device
// bridge. In the common case the batch only repeats identity already captured and
// this returns false, preserving the fast path. Note this keys on raw field
// presence, not email resolution: a batch that merely repeats an already-seen
// email whose membership changed does not re-trigger (that heals on the next
// session, by design).
func sessionEnrichesAttribution(incoming claudeLogMetadata, cached SessionMetadata) bool {
	return (incoming.UserEmail != "" && cached.UserEmail == "") ||
		(incoming.ExternalOrgID != "" && cached.ExternalOrgID == "") ||
		(incoming.ExternalAccountUUID != "" && cached.ExternalAccountUUID == "") ||
		(incoming.ExternalAccountID != "" && cached.ExternalAccountID == "") ||
		(incoming.DeviceID != "" && cached.DeviceID == "")
}

type claudeLogMetadata struct {
	SessionID           string
	ServiceName         string
	UserEmail           string
	ExternalOrgID       string
	ExternalAccountUUID string
	ExternalAccountID   string
	DeviceID            string
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
						SessionID:           data.SessionID,
						ServiceName:         serviceName,
						UserEmail:           "",
						ExternalOrgID:       "",
						ExternalAccountUUID: "",
						ExternalAccountID:   "",
						DeviceID:            "",
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
				if data.ExternalOrgID != "" {
					meta.ExternalOrgID = data.ExternalOrgID
				}
				if data.ExternalAccountUUID != "" {
					meta.ExternalAccountUUID = data.ExternalAccountUUID
				}
				if data.ExternalAccountID != "" {
					meta.ExternalAccountID = data.ExternalAccountID
				}
				if data.DeviceID != "" {
					meta.DeviceID = data.DeviceID
				}
				if meta.ServiceName == "" && serviceName != "" {
					meta.ServiceName = serviceName
				}
			}
		}
	}

	return ordered
}

func (s *Service) writeClaudeOTELLogsToClickHouse(ctx context.Context, payload *gen.LogsPayload, orgID string, projectID string, attributionBySession map[string]SessionMetadata) {
	if s.telemetryLogger == nil || payload == nil {
		return
	}

	parsedProjectID, err := uuid.Parse(projectID)
	if err != nil {
		s.logger.ErrorContext(ctx, "invalid project ID for Claude OTEL logs", attr.SlogError(err))
		return
	}

	params := make([]telemetry.LogParams, 0)
	stagedParams := make([]telemetry.LogParams, 0)
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
				sessionID := stringAttr(logAttrs, attribute.Key("session.id"))
				sessionMeta := attributionBySession[sessionID]
				stampAccountAttribution(logAttrs, sessionMeta)
				if shouldTriggerClaudePromptCorrelation(logAttrs) {
					correlationSessionIDs[sessionID] = struct{}{}
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

				// Attribute usage to the owning employee. For a team session the
				// resolved Gram user id and the email agree; for a personal
				// session the email won't resolve but the device bridge may have
				// supplied the owner's user id, so personal usage rolls up under
				// the employee while account_type=personal preserves the split.
				userInfo := telemetry.UserInfoByEmail(stringAttr(logAttrs, attr.UserEmailKey))
				if sessionMeta.UserID != "" {
					userInfo = telemetry.UserInfoByIDAndEmail(sessionMeta.UserID, sessionMeta.UserEmail)
				}

				timestamp, observedTimestamp := otelLogTimestamps(logRecord)
				logParams := telemetry.WithOTELMetadata(telemetry.LogParams{
					Timestamp:  timestamp,
					ToolInfo:   claudeOTELLogToolInfo(orgID, parsedProjectID.String()),
					UserInfo:   userInfo,
					Attributes: logAttrs,
				}, observedTimestamp, resourceAttrs)

				// Claude redacts user-configured MCP server/tool names to
				// "custom" on api_request rows. Those rows park in
				// telemetry_logs_staging until the transcript-derived
				// attribution for their request_id arrives via the
				// Stop/SubagentStop hooks (or the promotion timeout passes);
				// the scheduled sweep then rewrites the names and inserts
				// into telemetry_logs so attribute_metrics_summaries_mv
				// aggregates the true attribution. The sweep scans staging
				// per project and joins tuples per request id, so even
				// sessionless redacted rows are reachable. Everything else
				// writes through.
				if isRedactedClaudeAPIRequest(logAttrs) {
					stagedParams = append(stagedParams, logParams)
					continue
				}
				params = append(params, logParams)
			}
		}
	}

	if err := s.telemetryLogger.LogBulk(ctx, params); err != nil {
		s.logger.ErrorContext(ctx, "failed to write Claude OTEL logs to ClickHouse", attr.SlogError(err))
		return
	}
	if err := s.telemetryLogger.LogBulkStaging(ctx, stagedParams); err != nil {
		s.logger.ErrorContext(ctx, "failed to write staged Claude OTEL logs to ClickHouse", attr.SlogError(err))
		return
	}
	for sessionID := range correlationSessionIDs {
		s.scheduleClaudePromptCorrelation(ctx, parsedProjectID, sessionIDToUUID(sessionID), sessionID)
	}
}

// isRedactedClaudeAPIRequest reports whether this OTEL log row is a Claude
// api_request whose inline MCP attribution Claude redacted to "custom" —
// exactly the rows the staging/promotion path exists for.
func isRedactedClaudeAPIRequest(logAttrs map[attr.Key]any) bool {
	if stringAttr(logAttrs, attribute.Key("mcp_server.name")) != "custom" {
		return false
	}
	return stringAttr(logAttrs, attribute.Key("event.name")) == "api_request" ||
		stringAttr(logAttrs, attr.LogBodyKey) == "claude_code.api_request"
}

func shouldTriggerClaudePromptCorrelation(logAttrs map[attr.Key]any) bool {
	return stringAttr(logAttrs, attribute.Key("event.name")) == "user_prompt"
}

func (s *Service) scheduleClaudePromptCorrelation(ctx context.Context, projectID uuid.UUID, chatID uuid.UUID, sessionID string) {
	workflowCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if _, err := background.ExecuteCorrelateClaudePromptsWorkflow(workflowCtx, s.temporalEnv, background.CorrelateClaudePromptsParams{
		ProjectID:              projectID,
		ChatID:                 chatID,
		SessionID:              sessionID,
		AfterMessageSeq:        0,
		AfterEventSequence:     0,
		AfterEventTimeUnixNano: 0,
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
	SessionID           string
	UserEmail           string
	ExternalOrgID       string
	ExternalAccountUUID string
	ExternalAccountID   string
	DeviceID            string
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
		SessionID:           "",
		UserEmail:           "",
		ExternalOrgID:       "",
		ExternalAccountUUID: "",
		ExternalAccountID:   "",
		DeviceID:            "",
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
			data.ExternalOrgID = value
		case "user.account_uuid":
			data.ExternalAccountUUID = value
		case "user.account_id":
			data.ExternalAccountID = value
		case "user.id":
			data.DeviceID = value
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
