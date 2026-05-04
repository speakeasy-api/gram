package hooks

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
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
		var err error
		ctx, err = s.authorizePluginRequest(ctx, *payload.ApikeyToken, *payload.ProjectSlugInput)
		if err != nil {
			return nil, err
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
			reason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			// systemMessage renders as a warning in the user's terminal;
			// permissionDecisionReason is what Claude itself sees and may quote
			// back to the user. Send the same self-branded message in both so
			// the user sees feedback regardless of how Claude chooses to render
			// the deny — matches the shadow-MCP guard deny path below.
			result.SystemMessage = &reason
			if output != nil {
				output.PermissionDecision = &deny
				output.PermissionDecisionReason = &reason
			}
			// Surface the block reason on the trace summary so the dashboard
			// shows why the call was denied.
			if metadata, err := s.getSessionMetadata(ctx, *payload.SessionID); err == nil {
				s.writeClaudeBlockToClickHouse(ctx, payload, &metadata, reason)
			}
			return result, nil
		}
	}

	allow := "allow"
	deny := "deny"
	result := makeHookResult(payload.HookEventName)
	output, _ := result.HookSpecificOutput.(*HookSpecificOutput)

	rawToolName := ""
	if payload.ToolName != nil {
		rawToolName = *payload.ToolName
	}

	// Resolve session metadata once and reuse for the destructive checks
	// below. metadataOK is false when we cannot derive an org/project, in
	// which case we fall through to the existing allow path — destructive
	// enforcement requires at minimum an org id.
	metadata, metadataOK := s.resolveClaudePreToolUseMetadata(ctx, payload)

	// Content trigger — runs for every tool call (native Bash/Edit/Write and
	// MCP alike) so curated patterns like "rm -rf", "DROP TABLE", or "aws ec2
	// terminate-instances" are caught regardless of the tool name. Order matters:
	// runs before the MCP-only branch below so native tools that aren't routed
	// through Gram still get gated.
	if matched, ok := scanForDestructive(payload.ToolInput); ok && metadataOK {
		denyReason := fmt.Sprintf("matched destructive pattern: %s", matched.FullName())
		if blocked := s.applyDestructiveScopeDeny(ctx, payload, &metadata, rawToolName, denyReason, matched.FullName(), result, output, deny); blocked {
			return result, nil
		}
	}

	// Only Gram-hosted (non-local) tool calls carry the x-gram-toolset-id
	// property. In Claude Code, MCP-routed tools are identified by the
	// "mcp__<server>__<tool>" name convention; native tools (Read, Edit, Bash,
	// ...) are skipped.
	mcpToolName, isMCP := claudeMCPToolName(rawToolName)
	if !isMCP {
		if output != nil {
			output.PermissionDecision = &allow
		}
		return result, nil
	}

	if !metadataOK {
		// No session yet to derive org from — fall back to allow rather than
		// breaking the tool call. Hook event will still be buffered.
		if output != nil {
			output.PermissionDecision = &allow
		}
		return result, nil
	}

	policy := s.lookupShadowMCPBlockingPolicy(ctx, metadata.ProjectID)
	if policy == nil {
		// Annotation trigger still runs even when no shadow-MCP policy is
		// active: a destructive MCP tool with DestructiveHint=true requires
		// the scope regardless of toolset signing.
		if denied := s.maybeEnforceMCPAnnotation(ctx, payload, &metadata, rawToolName, mcpToolName, result, output, deny); denied {
			return result, nil
		}
		if output != nil {
			output.PermissionDecision = &allow
		}
		return result, nil
	}

	detail, denied, tool := s.shadowMCPClient.LookupToolsetCall(ctx, payload.ToolInput, mcpToolName, metadata.GramOrgID)
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
		reason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", policy.Name, detail)
		// Record a companion ClickHouse entry with gram.hook.block_reason set
		// so the trace_summaries materialized view can flag this trace as
		// blocked. Shares the original PreToolUse trace_id (derived from
		// tool_use_id) so both rows aggregate into the same trace summary.
		s.writeClaudeBlockToClickHouse(ctx, payload, &metadata, reason)

		// systemMessage renders as a warning in the user's terminal;
		// permissionDecisionReason is what Claude itself sees and may quote
		// back to the user, so we send the same self-branded message in both.
		result.SystemMessage = &reason
		if output != nil {
			output.PermissionDecision = &deny
			output.PermissionDecisionReason = &reason
		}
		return result, nil
	}

	// Annotation trigger: if shadow-MCP allowed but the tool is annotated
	// destructive, the destructive scope is still required.
	if tool != nil && tool.Annotations != nil && tool.Annotations.DestructiveHint != nil && *tool.Annotations.DestructiveHint {
		if denied := s.applyDestructiveScopeDeny(ctx, payload, &metadata, rawToolName, "tool annotated DestructiveHint=true", "", result, output, deny); denied {
			return result, nil
		}
	}

	if output != nil {
		output.PermissionDecision = &allow
	}
	return result, nil
}

// resolveClaudePreToolUseMetadata attempts to derive the org+project context
// for a Claude PreToolUse hook. Returns (metadata, true) when at least the
// org id is known, (zero, false) otherwise — callers fall back to allow when
// false. Plugin auth supplies org/project directly; OTEL-only sessions need
// the Redis cache to have been seeded by /rpc/hooks.otel.logs.
func (s *Service) resolveClaudePreToolUseMetadata(ctx context.Context, payload *gen.ClaudePayload) (SessionMetadata, bool) {
	sessionID := ""
	if payload.SessionID != nil {
		sessionID = *payload.SessionID
	}

	if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil && authCtx.ProjectID != nil {
		metadata := SessionMetadata{
			SessionID:   sessionID,
			ServiceName: "",
			UserEmail:   "",
			ClaudeOrgID: "",
			GramOrgID:   authCtx.ActiveOrganizationID,
			ProjectID:   authCtx.ProjectID.String(),
		}
		if sessionID != "" {
			if cached, err := s.getSessionMetadata(ctx, sessionID); err == nil {
				metadata.ServiceName = cached.ServiceName
				metadata.UserEmail = cached.UserEmail
				metadata.ClaudeOrgID = cached.ClaudeOrgID
			}
		}
		return metadata, true
	}

	if sessionID == "" {
		return SessionMetadata{}, false //nolint:exhaustruct // sentinel zero value; second return signals absence
	}

	metadata, err := s.getSessionMetadata(ctx, sessionID)
	if err != nil {
		s.logger.WarnContext(ctx, "claude PreToolUse fired before session metadata available; allowing tool call",
			attr.SlogEvent("claude_hook_pretooluse_no_metadata"),
			attr.SlogError(err),
		)
		return SessionMetadata{}, false //nolint:exhaustruct // sentinel zero value; second return signals absence
	}
	return metadata, true
}

// maybeEnforceMCPAnnotation runs the annotation trigger for an MCP tool
// reachable via the shadow-MCP toolset cache when there's no active blocking
// policy. Returns true when the call has been denied (and the result struct
// has been populated for return).
func (s *Service) maybeEnforceMCPAnnotation(ctx context.Context, payload *gen.ClaudePayload, metadata *SessionMetadata, rawToolName, mcpToolName string, result *gen.ClaudeHookResult, output *HookSpecificOutput, deny string) bool {
	_, lookupDenied, tool := s.shadowMCPClient.LookupToolsetCall(ctx, payload.ToolInput, mcpToolName, metadata.GramOrgID)
	if lookupDenied || tool == nil || tool.Annotations == nil || tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint {
		return false
	}
	return s.applyDestructiveScopeDeny(ctx, payload, metadata, rawToolName, "tool annotated DestructiveHint=true", "", result, output, deny)
}

// applyDestructiveScopeDeny evaluates the destructive scope check and, when
// the caller is denied, populates the result struct with a self-branded deny
// message, writes the companion ClickHouse block-reason row, and emits an
// audit event. Returns true when the call was denied. Returns false when the
// scope check passes or RBAC enforcement is skipped (non-enterprise org,
// flag off, no auth context).
func (s *Service) applyDestructiveScopeDeny(ctx context.Context, payload *gen.ClaudePayload, metadata *SessionMetadata, rawToolName, reasonContext, matchedPattern string, result *gen.ClaudeHookResult, output *HookSpecificOutput, deny string) bool {
	if s.authz == nil {
		return false
	}
	gramUserID := s.lookupGramUserIDByEmail(ctx, metadata.UserEmail)
	checkErr := s.authz.RequireForHookPrincipal(ctx, gramUserID, metadata.GramOrgID,
		authz.Check{Scope: authz.ScopeToolsExecuteDestructive, ResourceKind: "", ResourceID: metadata.GramOrgID, Dimensions: nil},
	)
	if checkErr == nil {
		return false
	}
	var oopsErr *oops.ShareableError
	if !errors.As(checkErr, &oopsErr) || oopsErr.Code != oops.CodeForbidden {
		// Unexpected auth-engine failure (e.g. DB error). Surface the failure
		// in logs, but fall through to allow so we don't accidentally break
		// every tool call when authz is misconfigured.
		s.logger.ErrorContext(ctx, "destructive scope check errored; falling through to allow",
			attr.SlogEvent("claude_hook_destructive_check_error"),
			attr.SlogError(checkErr),
		)
		return false
	}

	reason := fmt.Sprintf("Speakeasy blocked this destructive tool call: caller lacks the %s scope (%s)", string(authz.ScopeToolsExecuteDestructive), reasonContext)
	s.logger.InfoContext(ctx, "denying claude tool call: missing destructive scope",
		attr.SlogEvent("claude_hook_destructive_denied"),
		attr.SlogValueAny(map[string]any{
			"toolName":       rawToolName,
			"reasonContext":  reasonContext,
			"matchedPattern": matchedPattern,
		}),
	)
	s.writeClaudeBlockToClickHouse(ctx, payload, metadata, reason)

	s.recordDestructiveDenyAudit(ctx, metadata, gramUserID, rawToolName, reasonContext, matchedPattern)

	result.SystemMessage = &reason
	if output != nil {
		output.PermissionDecision = &deny
		output.PermissionDecisionReason = &reason
	}
	return true
}

// lookupGramUserIDByEmail resolves a Claude/Cursor session email to a Gram
// user ID. Returns "" when email is empty or no Gram user matches; the
// destructive check treats either case as Forbidden.
func (s *Service) lookupGramUserIDByEmail(ctx context.Context, email string) string {
	if email == "" || s.db == nil {
		return ""
	}
	user, err := usersrepo.New(s.db).GetUserByEmail(ctx, email)
	if err != nil {
		return ""
	}
	return user.ID
}

func (s *Service) recordDestructiveDenyAudit(ctx context.Context, metadata *SessionMetadata, gramUserID, toolName, reasonContext, matchedPattern string) {
	if s.db == nil {
		return
	}
	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		projectID = uuid.Nil
	}
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, gramUserID)
	if gramUserID == "" {
		// Anonymous attribution — record the email as the display name so
		// reviewers can correlate without a Gram user binding.
		actor = urn.Principal{Type: urn.PrincipalTypeUser, ID: ""}
	}
	displayName := metadata.UserEmail
	if err := audit.LogToolExecuteDestructiveDeny(ctx, s.db, audit.LogToolExecuteDestructiveDenyEvent{
		OrganizationID:   metadata.GramOrgID,
		ProjectID:        projectID,
		Actor:            actor,
		ActorDisplayName: &displayName,
		ActorSlug:        nil,
		ToolName:         toolName,
		Reason:           reasonContext,
		MatchedPattern:   matchedPattern,
	}); err != nil {
		s.logger.WarnContext(ctx, "failed to record destructive deny audit event",
			attr.SlogEvent("claude_hook_destructive_audit_error"),
			attr.SlogError(err),
		)
	}
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
