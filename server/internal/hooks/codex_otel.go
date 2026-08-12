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
	// codexServiceName is the stem of the OTEL resource service.name family
	// the Codex clients report (see isCodexServiceName). The name varies by
	// mode and does NOT use one separator convention — observed against
	// the shipped 0.146 build: codex_cli_rs (interactive), codex_exec
	// (headless `codex exec`, what CI and scripted runs use), codex_tui,
	// codex_mcp, and codex-app-server (Codex mode in the unified ChatGPT
	// desktop app, hyphenated). Matching a single name did not drop the other
	// modes: their payloads fell through to the Claude path and were
	// persisted as claude-code:otel:logs rows with Claude's hook source and
	// attribution, so Codex traffic silently inflated Claude surfaces (and,
	// keyed on the wrong URN, was never metered as Codex usage). Matching the
	// whole family means a new mode routes correctly on arrival rather than
	// being discovered later as mislabeled data; Claude reports
	// "claude-code"-style names, so this cannot collide.
	codexServiceName = "codex"
	// codexOTELLogsURN types a raw Codex OTEL log row, mirroring the
	// "claude-code:otel:logs" convention.
	codexOTELLogsURN = "codex:otel:logs"
	// codexOTELMetricsURN types a raw Codex OTEL metric data point row.
	codexOTELMetricsURN = "codex:otel:metrics"
)

// isCodexServiceName reports whether an OTEL resource service.name belongs to
// the Codex client family (see codexServiceName): "codex" itself, or "codex"
// followed by a mode suffix on either separator. Requiring the separator is
// what keeps an unrelated name that merely starts with those letters —
// "codexish-tool" — from matching.
func isCodexServiceName(serviceName string) bool {
	suffix, ok := strings.CutPrefix(serviceName, codexServiceName)
	if !ok {
		return false
	}
	return suffix == "" || suffix[0] == '_' || suffix[0] == '-'
}

// splitCodexLogsPayload partitions a logs payload by resource service.name.
// Routing must be per resource, not per payload: an OpenTelemetry Collector
// fanning in several clients can re-batch Claude and Codex resources into one
// export, and an any-resource match would hand the whole batch to one writer —
// stamping Claude records as Codex rows (or vice versa) and skipping the
// Claude session/attribution chain entirely. Either return value is nil when
// that side has no resources, so a single-client payload allocates nothing.
func splitCodexLogsPayload(payload *gen.LogsPayload) (codex, claude *gen.LogsPayload) {
	if payload == nil {
		return nil, nil
	}
	var codexLogs, claudeLogs []*gen.OTELResourceLog
	for _, rl := range payload.ResourceLogs {
		if rl == nil {
			continue
		}
		if isCodexServiceName(extractResourceAttribute(rl.Resource, "service.name")) {
			codexLogs = append(codexLogs, rl)
			continue
		}
		claudeLogs = append(claudeLogs, rl)
	}
	// Both halves keep the envelope's auth fields: the split is a routing
	// concern and must not strip credentials a downstream writer may read.
	if len(codexLogs) > 0 {
		codex = &gen.LogsPayload{
			ResourceLogs:     codexLogs,
			ApikeyToken:      payload.ApikeyToken,
			ProjectSlugInput: payload.ProjectSlugInput,
		}
	}
	if len(claudeLogs) > 0 {
		claude = &gen.LogsPayload{
			ResourceLogs:     claudeLogs,
			ApikeyToken:      payload.ApikeyToken,
			ProjectSlugInput: payload.ProjectSlugInput,
		}
	}
	return codex, claude
}

// splitCodexMetricsPayload is splitCodexLogsPayload's metrics twin.
func splitCodexMetricsPayload(payload *gen.MetricsPayload) (codex, claude *gen.MetricsPayload) {
	if payload == nil {
		return nil, nil
	}
	var codexMetrics, claudeMetrics []*gen.OTELResourceMetrics
	for _, rm := range payload.ResourceMetrics {
		if rm == nil {
			continue
		}
		if isCodexServiceName(extractResourceAttribute(rm.Resource, "service.name")) {
			codexMetrics = append(codexMetrics, rm)
			continue
		}
		claudeMetrics = append(claudeMetrics, rm)
	}
	if len(codexMetrics) > 0 {
		codex = &gen.MetricsPayload{
			ResourceMetrics:  codexMetrics,
			ApikeyToken:      payload.ApikeyToken,
			ProjectSlugInput: payload.ProjectSlugInput,
		}
	}
	if len(claudeMetrics) > 0 {
		claude = &gen.MetricsPayload{
			ResourceMetrics:  claudeMetrics,
			ApikeyToken:      payload.ApikeyToken,
			ProjectSlugInput: payload.ProjectSlugInput,
		}
	}
	return codex, claude
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
// Codex OTEL payloads carry no account identity beyond user.email, so account
// attribution here is email-based (see classifyAccount): each conversation is
// attributed once per payload (memoized, with the session cache as the
// cross-payload fast path) and its rows are stamped with account_type /
// billing_mode so the cost surfaces can classify Codex spend (DNO-734).
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
	// same user, so resolve each distinct email once per payload. Session
	// attribution is likewise memoized per conversation id.
	emailToUserID := make(map[string]string)
	attributionBySession := make(map[string]SessionMetadata)
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

				userInfo, email, userID := s.codexOTELUserInfo(ctx, logAttrs, emailToUserID, orgID)
				sessionMeta := s.codexOTELSessionAttribution(ctx, attributionBySession, codexOTELIdentity{
					SessionID: stringAttr(logAttrs, attr.GenAIConversationIDKey),
					Email:     email,
					UserID:    userID,
					OrgID:     orgID,
					ProjectID: projectID,
				})
				stampAccountAttribution(logAttrs, sessionMeta)

				timestamp, observedTimestamp := otelLogTimestamps(logRecord)
				params = append(params, telemetry.WithOTELMetadata(telemetry.LogParams{
					Timestamp:  timestamp,
					ToolInfo:   toolInfo,
					UserInfo:   userInfo,
					Attributes: logAttrs,
				}, observedTimestamp, resourceAttrs))
			}
		}
	}

	if err := s.telemetryLogger.LogBulk(ctx, params); err != nil {
		s.logger.ErrorContext(ctx, "failed to write Codex OTEL logs to ClickHouse", attr.SlogError(err))
	}
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
	attributionBySession := make(map[string]SessionMetadata)
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

					// Stamp the same account attribution as the logs stream so
					// a session's rows classify consistently across both.
					userInfo, email, userID := s.codexOTELUserInfo(ctx, attrs, emailToUserID, orgID)
					sessionMeta := s.codexOTELSessionAttribution(ctx, attributionBySession, codexOTELIdentity{
						SessionID: stringAttr(attrs, attr.GenAIConversationIDKey),
						Email:     email,
						UserID:    userID,
						OrgID:     orgID,
						ProjectID: projectID,
					})
					stampAccountAttribution(attrs, sessionMeta)

					params = append(params, telemetry.WithOTELMetadata(telemetry.LogParams{
						Timestamp:  timestamp,
						ToolInfo:   toolInfo,
						UserInfo:   userInfo,
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
// The resolved email and user id are returned alongside the UserInfo so the
// session-attribution path can reuse them without a second resolution.
func (s *Service) codexOTELUserInfo(ctx context.Context, attrs map[attr.Key]any, emailToUserID map[string]string, orgID string) (telemetry.UserInfo, string, string) {
	email := strings.TrimSpace(stringAttr(attrs, attr.UserEmailKey))
	if email == "" {
		return telemetry.UserInfoByEmail(""), "", ""
	}

	lookup := conv.NormalizeEmail(email)
	userID, seen := emailToUserID[lookup]
	if !seen {
		userID = s.resolveUserByEmail(ctx, email, orgID)
		emailToUserID[lookup] = userID
	}
	if userID == "" {
		return telemetry.UserInfoByEmail(email), email, ""
	}
	return telemetry.UserInfoByIDAndEmail(userID, email), email, userID
}

// sameCodexIdentity reports whether an incoming record's email brings no
// identity a prior attribution lacks: either the record carries none, or it
// matches the attributed email (case-insensitively — Codex surfaces report
// case-variant emails, e.g. "Dev@Example.com" in the compliance feed).
func sameCodexIdentity(attributedEmail, incomingEmail string) bool {
	return incomingEmail == "" ||
		conv.NormalizeEmail(attributedEmail) == conv.NormalizeEmail(incomingEmail)
}

// codexOTELIdentity carries the per-record identity inputs for session
// attribution on the Codex OTEL stream.
type codexOTELIdentity struct {
	SessionID string
	Email     string
	UserID    string
	OrgID     string
	ProjectID string
}

// codexOTELSessionAttribution returns the account attribution for a Codex OTEL
// record's session, computing it at most once per (payload, session). The
// session cache is the cross-payload fast path — Codex exports every few
// seconds and its identity (the email alone) rarely changes mid-session — and
// is re-seeded after a fresh classification so the legacy hook path and the
// ingest adapter inherit the same attribution. Billing mode is frozen with the
// classification for the cache lifetime (a declaration made mid-session is
// picked up on the next session), matching the Claude path's attribute-once
// semantics. A record without a conversation id stays email-attributed only:
// the zero metadata stamps nothing.
func (s *Service) codexOTELSessionAttribution(ctx context.Context, memo map[string]SessionMetadata, id codexOTELIdentity) SessionMetadata {
	var none SessionMetadata
	if id.SessionID == "" {
		return none
	}
	if meta, ok := memo[id.SessionID]; ok && sameCodexIdentity(meta.UserEmail, id.Email) {
		return meta
	}

	var cached SessionMetadata
	if err := s.cache.Get(ctx, sessionCacheKey(id.SessionID), &cached); err != nil ||
		cached.ServiceName != "Codex" || cached.GramOrgID != id.OrgID || cached.ProjectID != id.ProjectID {
		cached = none
	}
	// Reuse a prior classification when this record brings no identity the
	// cache lacks; recompute when the email is new or the entry predates
	// classification (empty AccountType).
	if cached.AccountType != "" && sameCodexIdentity(cached.UserEmail, id.Email) {
		memo[id.SessionID] = cached
		return cached
	}

	userEmail, userID := id.Email, id.UserID
	if userEmail == "" {
		userEmail, userID = cached.UserEmail, cached.UserID
	}
	meta := SessionMetadata{
		SessionID:           id.SessionID,
		ServiceName:         "Codex",
		UserEmail:           userEmail,
		UserID:              userID,
		Provider:            providerOpenAI,
		ExternalOrgID:       "",
		ExternalAccountUUID: "",
		ExternalAccountID:   "",
		DeviceID:            "",
		Hostname:            cached.Hostname,
		AccountType:         "",
		BillingMode:         "",
		UserAccountID:       "",
		// user.email is the authenticated ChatGPT account's own report, so it
		// doubles as the observed email kept separate from actor identity.
		ObservedUserEmail: userEmail,
		GramOrgID:         id.OrgID,
		ProjectID:         id.ProjectID,
	}
	if err := s.attributeSession(ctx, &meta); err != nil {
		s.logger.WarnContext(ctx, "failed to attribute AI account for Codex session",
			attr.SlogEvent("account_attribution_failed"),
			attr.SlogError(err),
			attr.SlogGenAIConversationID(id.SessionID),
		)
		// Leave the session unclassified rather than half-attributed:
		// attributeSession stamps AccountType before the step that failed,
		// and every fast path keys on AccountType alone — caching the half
		// state would freeze an empty billing mode for the full TTL.
		meta.AccountType = ""
		meta.BillingMode = ""
	}
	memo[id.SessionID] = meta
	// A failed attribution is cached with empty AccountType — the fast path
	// above rejects it, so the next payload retries.
	if err := s.cache.Set(ctx, sessionCacheKey(id.SessionID), meta, 24*time.Hour); err != nil {
		s.logger.WarnContext(ctx, "failed to cache Codex session metadata",
			attr.SlogError(err),
			attr.SlogGenAIConversationID(id.SessionID),
		)
	}
	return meta
}
