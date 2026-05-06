package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

// claudeRequestDecoder is a custom decoder that handles both JSON and form-urlencoded content types
func claudeRequestDecoder(r *http.Request) goahttp.Decoder {
	contentType := r.Header.Get("Content-Type")

	if strings.Contains(contentType, "application/x-www-form-urlencoded") {
		return &formDecoder{r: r}
	}

	return goahttp.RequestDecoder(r)
}

// formDecoder implements goahttp.Decoder for form-urlencoded data
type formDecoder struct {
	r *http.Request
}

func (d *formDecoder) Decode(v any) error {
	body, err := io.ReadAll(d.r.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	values, err := url.ParseQuery(string(body))
	if err != nil {
		return fmt.Errorf("parse query: %w", err)
	}

	// Convert form values to JSON string and then unmarshal
	// This works because the form keys match the JSON field names
	jsonData := make(map[string]any)
	for key, vals := range values {
		if len(vals) > 0 {
			// Try to unmarshal as JSON if the value looks like JSON
			var parsed any
			if err := json.Unmarshal([]byte(vals[0]), &parsed); err == nil {
				jsonData[key] = parsed
			} else {
				jsonData[key] = vals[0]
			}
		}
	}

	// Marshal back to JSON and unmarshal into the target struct
	jsonBytes, err := json.Marshal(jsonData)
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}

	if err := json.Unmarshal(jsonBytes, v); err != nil {
		return fmt.Errorf("unmarshal json: %w", err)
	}
	return nil
}

// Logs handles authenticated OTEL logs data from Claude Code
func (s *Service) Logs(ctx context.Context, payload *gen.LogsPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.E(oops.CodeUnauthorized, nil, "unauthorized")
	}

	claudeMetadata := extractSessionMetadata(payload)
	if claudeMetadata.SessionID == "" {
		s.logger.WarnContext(ctx, "Logs payload contained no session ID")
		return nil
	}

	completeMetadata := SessionMetadata{
		SessionID:   claudeMetadata.SessionID,
		ServiceName: claudeMetadata.ServiceName,
		UserEmail:   claudeMetadata.UserEmail,
		ClaudeOrgID: claudeMetadata.ClaudeOrgID,
		GramOrgID:   authCtx.ActiveOrganizationID,
		ProjectID:   authCtx.ProjectID.String(),
	}

	if err := s.cache.Set(ctx, sessionCacheKey(completeMetadata.SessionID), completeMetadata, 24*time.Hour); err != nil {
		s.logger.ErrorContext(ctx, "Failed to store session metadata", attr.SlogError(err))
	}

	s.flushPendingHooks(ctx, completeMetadata.SessionID, &completeMetadata)

	s.logger.InfoContext(ctx, "Stored session metadata",
		attr.SlogEvent("session_validated"),
	)

	return nil
}

// Metrics handles authenticated OTEL metrics data from Claude Code
func (s *Service) Metrics(ctx context.Context, payload *gen.MetricsPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.E(oops.CodeUnauthorized, nil, "unauthorized")
	}

	s.logger.InfoContext(ctx, "Received Claude token metrics",
		attr.SlogEvent("claude_metrics"),
		attr.SlogValueAny(map[string]any{
			"organization_id": authCtx.ActiveOrganizationID,
			"project_id":      authCtx.ProjectID.String(),
		}),
	)

	// Write metrics to ClickHouse
	s.writeMetricsToClickHouse(ctx, payload, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	return nil
}

type claudeLogMetadata struct {
	SessionID   string
	ServiceName string
	UserEmail   string
	ClaudeOrgID string
}

func extractSessionMetadata(payload *gen.LogsPayload) claudeLogMetadata {
	metadata := claudeLogMetadata{
		SessionID:   "",
		ServiceName: "",
		UserEmail:   "",
		ClaudeOrgID: "",
	}

	// Iterate through all resource logs
	for _, resourceLog := range payload.ResourceLogs {
		if resourceLog == nil {
			continue
		}

		// Extract service name from resource attributes
		metadata.ServiceName = extractResourceAttribute(resourceLog.Resource, "service.name")

		// Iterate through all scope logs
		for _, scopeLog := range resourceLog.ScopeLogs {
			if scopeLog == nil {
				continue
			}

			// Iterate through all log records
			for _, logRecord := range scopeLog.LogRecords {
				if logRecord == nil {
					continue
				}

				// Extract session data
				data := extractLogData(logRecord)

				if data.SessionID == "" {
					continue
				}

				// Store session metadata in Redis
				metadata.SessionID = data.SessionID
				metadata.UserEmail = data.UserEmail
				metadata.ClaudeOrgID = data.ClaudeOrgID
			}
		}
	}

	return metadata
}

// Claude is the unified endpoint for all Claude Code hook events.
func (s *Service) Claude(ctx context.Context, payload *gen.ClaudePayload) (*gen.ClaudeHookResult, error) {
	s.logger.InfoContext(ctx, fmt.Sprintf("🪝 HOOK Claude: %s", payload.HookEventName),
		attr.SlogEvent("claude_hook"),
		attr.SlogValueAny(map[string]any{
			"hookEventName": payload.HookEventName,
			"toolName":      payload.ToolName,
			"sessionID":     payload.SessionID,
		}),
	)

	if hasOptionalPluginAuth(payload) {
		// Auth is optional. Returning a 401 on failure deadlocks the client:
		// send_hook.sh maps any non-2xx to "block all tool calls", but
		// recovering (e.g. `gram login`) requires Bash, which the hook just
		// blocked. On failure we leave ctx unchanged and fall through to the
		// same path a no-headers request takes — recordHook buffers the event
		// in Redis, and the OTEL Logs endpoint flushes it once the session is
		// validated. Policies that need auth context degrade gracefully.
		if authedCtx, err := s.authorizePluginRequest(ctx, *payload.ApikeyToken, *payload.ProjectSlugInput); err != nil {
			s.logger.WarnContext(ctx, "plugin auth failed on claude hook; falling back to OTEL-buffered path",
				attr.SlogEvent("claude_hook_auth_failed"),
				attr.SlogError(err),
			)
		} else {
			ctx = authedCtx
		}
	}

	s.recordHook(ctx, payload)

	// Route to appropriate handler based on hook type
	switch payload.HookEventName {
	case "SessionStart":
		return s.handleSessionStart(ctx, payload)
	case "PreToolUse":
		return s.handlePreToolUse(ctx, payload)
	case "PostToolUse":
		return s.handlePostToolUse(ctx, payload)
	case "PostToolUseFailure":
		return s.handlePostToolUseFailure(ctx, payload)
	case "UserPromptSubmit":
		return s.handleUserPromptSubmit(ctx, payload)
	case "Stop":
		return s.handleStop(ctx, payload)
	case "SessionEnd":
		return s.handleSessionEnd(ctx, payload)
	case "Notification":
		return s.handleNotification(ctx, payload)
	default:
		s.logger.ErrorContext(ctx, fmt.Sprintf("Unknown hook event: %s", payload.HookEventName))
		return makeHookResult(payload.HookEventName), nil
	}
}

func (s *Service) handleSessionStart(ctx context.Context, payload *gen.ClaudePayload) (*gen.ClaudeHookResult, error) {
	// Always allow sessions to start
	continueVal := true
	result := makeHookResult(payload.HookEventName)
	result.Continue = &continueVal
	return result, nil
}

// hasOptionalPluginAuth returns true when the Claude request carries both
// the Gram-Key and Gram-Project headers, which signals plugin-driven
// attribution and triggers explicit auth in Claude().
func hasOptionalPluginAuth(payload *gen.ClaudePayload) bool {
	return payload.ApikeyToken != nil && *payload.ApikeyToken != "" &&
		payload.ProjectSlugInput != nil && *payload.ProjectSlugInput != ""
}

// authorizePluginRequest validates the API key and project slug supplied
// by a plugin-driven Claude request. Returns the auth-populated context
// on success, or a 401 on either failure (the request explicitly tried
// to authenticate, so we don't silently fall back to OTEL on bad creds).
func (s *Service) authorizePluginRequest(ctx context.Context, key, projectSlug string) (context.Context, error) {
	keyScheme := &security.APIKeyScheme{
		Name:           constants.KeySecurityScheme,
		Scopes:         []string{"consumer", "producer", "chat", "hooks"},
		RequiredScopes: []string{"hooks"},
	}
	ctx, err := s.auth.Authorize(ctx, key, keyScheme)
	if err != nil {
		return ctx, err
	}
	projectScheme := &security.APIKeyScheme{
		Name:           constants.ProjectSlugSecuritySchema,
		Scopes:         []string{},
		RequiredScopes: []string{"hooks"},
	}
	return s.auth.Authorize(ctx, projectSlug, projectScheme)
}

func (s *Service) recordHook(ctx context.Context, payload *gen.ClaudePayload) {
	if payload.SessionID == nil || *payload.SessionID == "" {
		s.logger.WarnContext(ctx, "Tool event called without session ID")
		return
	}

	sessionID := *payload.SessionID

	// Both plugin-authenticated and OTEL-only requests go through the same
	// Redis-buffered flow: persist when session metadata is in the cache,
	// buffer otherwise so flushPendingHooks can re-persist with full
	// attribution once /rpc/hooks.otel.logs lands. Claude hook payloads
	// don't carry user.email, so even plugin requests would land with an
	// empty user_email if persisted synchronously on a cache miss — Cursor
	// avoids this because its payload includes UserEmail directly.
	metadata, err := s.getSessionMetadata(ctx, sessionID)
	if err == nil {
		s.persistHook(ctx, payload, &metadata)
	} else {
		if err := s.bufferHook(ctx, sessionID, payload); err != nil {
			s.logger.ErrorContext(ctx, "Failed to buffer hook", attr.SlogError(err))
		}
	}
}

func (s *Service) persistHook(ctx context.Context, payload *gen.ClaudePayload, metadata *SessionMetadata) {
	if isConversationEvent(payload.HookEventName) {
		if err := s.persistConversationEvent(ctx, payload, metadata); err != nil {
			s.logger.ErrorContext(ctx, "Failed to persist conversation event", attr.SlogError(err))
		}
	} else {
		if err := s.persistToolCallEvent(ctx, payload, metadata); err != nil {
			s.logger.ErrorContext(ctx, "Failed to persist tool call event", attr.SlogError(err))
		}
	}
}

func (s *Service) getSessionMetadata(ctx context.Context, sessionID string) (SessionMetadata, error) {
	var metadata SessionMetadata
	err := s.cache.Get(ctx, sessionCacheKey(sessionID), &metadata)
	if err != nil {
		return SessionMetadata{}, fmt.Errorf("get session metadata: %w", err)
	}
	return metadata, nil
}

func (s *Service) handlePreToolUse(ctx context.Context, payload *gen.ClaudePayload) (*gen.ClaudeHookResult, error) {
	if s.riskScanner != nil && payload.SessionID != nil {
		if scanResult := s.scanClaudeForEnforcement(ctx, payload); scanResult != nil {
			result := makeHookResult(payload.HookEventName)
			output, _ := result.HookSpecificOutput.(*HookSpecificOutput)
			deny := "deny"
			auditReason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			userReason := renderUserBlockReason(scanResult.UserMessage, auditReason)
			// systemMessage renders as a warning in the user's terminal;
			// permissionDecisionReason is what Claude itself sees and may quote
			// back to the user. Send the same self-branded message in both so
			// the user sees feedback regardless of how Claude chooses to render
			// the deny — matches the shadow-MCP guard deny path below.
			result.SystemMessage = &userReason
			if output != nil {
				output.PermissionDecision = &deny
				output.PermissionDecisionReason = &userReason
			}
			// Surface the block reason on the trace summary so the dashboard
			// shows why the call was denied. Always store the technical reason
			// — the user_message override is for the agent-facing response only.
			if metadata, err := s.getSessionMetadata(ctx, *payload.SessionID); err == nil {
				s.writeClaudeBlockToClickHouse(ctx, payload, &metadata, auditReason)
			}
			return result, nil
		}
	}

	allow := "allow"
	deny := "deny"
	result := makeHookResult(payload.HookEventName)
	output, _ := result.HookSpecificOutput.(*HookSpecificOutput)

	// Only Gram-hosted (non-local) tool calls carry the x-gram-toolset-id
	// property. In Claude Code, MCP-routed tools are identified by the
	// "mcp__<server>__<tool>" name convention; native tools (Read, Edit, Bash,
	// ...) are skipped.
	rawToolName := ""
	if payload.ToolName != nil {
		rawToolName = *payload.ToolName
	}
	mcpToolName, isMCP := claudeMCPToolName(rawToolName)
	if !isMCP {
		if output != nil {
			output.PermissionDecision = &allow
		}
		return result, nil
	}

	sessionID := ""
	if payload.SessionID != nil {
		sessionID = *payload.SessionID
	}
	if sessionID == "" {
		// No session yet to derive org from — fall back to allow rather than
		// breaking the tool call. Hook event will still be buffered.
		if output != nil {
			output.PermissionDecision = &allow
		}
		return result, nil
	}
	// Plugin path: when the request authenticated via Gram-Key + Gram-Project,
	// org/project come from the auth context — same pattern as recordHook.
	// This lets the shadow-MCP guard run on the very first PreToolUse of a
	// session, before OTEL Logs has had a chance to seed Redis. Redis is still
	// consulted to enrich UserEmail / ServiceName / ClaudeOrgID for the
	// downstream ClickHouse row, but absence of cached fields is non-fatal.
	var metadata SessionMetadata
	if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil && authCtx.ProjectID != nil {
		metadata = SessionMetadata{
			SessionID:   sessionID,
			ServiceName: "",
			UserEmail:   "",
			ClaudeOrgID: "",
			GramOrgID:   authCtx.ActiveOrganizationID,
			ProjectID:   authCtx.ProjectID.String(),
		}
		if cached, err := s.getSessionMetadata(ctx, sessionID); err == nil {
			metadata.ServiceName = cached.ServiceName
			metadata.UserEmail = cached.UserEmail
			metadata.ClaudeOrgID = cached.ClaudeOrgID
		}
	} else {
		var err error
		metadata, err = s.getSessionMetadata(ctx, sessionID)
		if err != nil {
			// OTEL path with no cached metadata yet — allow this call; the
			// buffered hook will be re-persisted once metadata arrives.
			s.logger.WarnContext(ctx, "claude PreToolUse fired before session metadata available; allowing tool call",
				attr.SlogEvent("claude_hook_pretooluse_no_metadata"),
				attr.SlogError(err),
			)
			if output != nil {
				output.PermissionDecision = &allow
			}
			return result, nil
		}
	}

	policy := s.lookupShadowMCPBlockingPolicy(ctx, metadata.ProjectID)
	if policy == nil {
		if output != nil {
			output.PermissionDecision = &allow
		}
		return result, nil
	}

	detail, denied := s.shadowMCPClient.ValidateToolsetCall(ctx, payload.ToolInput, mcpToolName, metadata.GramOrgID)
	if denied {
		s.logger.InfoContext(ctx, "denying claude tool call: failed gram toolset validation",
			attr.SlogEvent("claude_hook_denied"),
			attr.SlogValueAny(map[string]any{
				"hookEventName": payload.HookEventName,
				"toolName":      rawToolName,
				"reason":        detail,
				"policyID":      policy.ID,
				"policyName":    policy.Name,
			}),
		)
		auditReason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", policy.Name, detail)
		userReason := renderUserBlockReason(policy.UserMessage, auditReason)
		// Record a companion ClickHouse entry with gram.hook.block_reason set
		// so the trace_summaries materialized view can flag this trace as
		// blocked. Shares the original PreToolUse trace_id (derived from
		// tool_use_id) so both rows aggregate into the same trace summary.
		// Always store the technical reason — the user_message override
		// is for the agent-facing response only.
		s.writeClaudeBlockToClickHouse(ctx, payload, &metadata, auditReason)

		// systemMessage renders as a warning in the user's terminal;
		// permissionDecisionReason is what Claude itself sees and may quote
		// back to the user, so we send the same self-branded message in both.
		result.SystemMessage = &userReason
		if output != nil {
			output.PermissionDecision = &deny
			output.PermissionDecisionReason = &userReason
		}
		return result, nil
	}

	if output != nil {
		output.PermissionDecision = &allow
	}
	return result, nil
}

// claudeMCPToolName returns the bare tool name and true if rawName follows the
// "mcp__<server>__<tool>" convention used by Claude Code for MCP-routed tools.
// Returns ("", false) for native Claude Code tools (Read, Edit, Bash, etc.).
func claudeMCPToolName(rawName string) (string, bool) {
	if !strings.HasPrefix(rawName, "mcp__") {
		return "", false
	}
	parts := strings.SplitN(rawName, "__", 3)
	if len(parts) != 3 || parts[2] == "" {
		return "", false
	}
	return parts[2], true
}

func (s *Service) handlePostToolUse(ctx context.Context, payload *gen.ClaudePayload) (*gen.ClaudeHookResult, error) {
	return makeHookResult(payload.HookEventName), nil
}

// writeClaudeBlockToClickHouse writes a companion ClickHouse log entry for a
// Claude PreToolUse call that the shadow-MCP guard denied. It reuses
// buildTelemetryAttributesWithMetadata so the new row shares the same trace_id
// (derived from tool_use_id) as the original PreToolUse log, and adds
// gram.hook.block_reason. trace_summaries_mv aggregates with max(), so the
// trace will surface as blocked regardless of which row arrives first.
func (s *Service) writeClaudeBlockToClickHouse(ctx context.Context, payload *gen.ClaudePayload, metadata *SessionMetadata, reason string) {
	if s.telemetryLogger == nil || reason == "" {
		return
	}

	attrs := s.buildTelemetryAttributesWithMetadata(ctx, payload, metadata)
	attrs[attr.HookBlockReasonKey] = reason
	toolName, _ := attrs[attr.ToolNameKey].(string)

	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		s.logger.WarnContext(ctx, "invalid project ID for Claude block log", attr.SlogError(err))
		return
	}

	s.telemetryLogger.Log(ctx, telemetry.LogParams{
		Timestamp: time.Now(),
		ToolInfo: telemetry.ToolInfo{
			Name:           toolName,
			OrganizationID: metadata.GramOrgID,
			ProjectID:      projectID.String(),
			ID:             "",
			URN:            "",
			DeploymentID:   "",
			FunctionID:     nil,
		},
		Attributes: attrs,
	})
}

func (s *Service) handlePostToolUseFailure(ctx context.Context, payload *gen.ClaudePayload) (*gen.ClaudeHookResult, error) {
	return makeHookResult(payload.HookEventName), nil
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
		if attr.Key == key && attr.Value != nil && attr.Value.StringValue != nil {
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

	if logRecord.Attributes == nil {
		return data
	}

	for _, attr := range logRecord.Attributes {
		if attr.Value == nil {
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
		if attr.Key == key && attr.Value != nil && attr.Value.StringValue != nil {
			return *attr.Value.StringValue
		}
	}

	return ""
}
