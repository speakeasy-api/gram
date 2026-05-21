package hooks

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

// decodeBodySampleLimit caps how many bytes of a failing request body get
// logged, so we don't dump megabytes of OTLP into the logs on every bad
// payload.
const decodeBodySampleLimit = 1024

const claudeShadowMCPMetadataUnavailableReason = "Speakeasy could not verify this MCP tool call because the Claude session metadata is not available yet. Try again after the session initializes."

// decoderFunc adapts a plain function to the goahttp.Decoder interface so we
// can capture per-request context (logger, raw body, headers) in a closure
// instead of via struct fields. This keeps containedctx happy and keeps the
// decoder factory readable.
type decoderFunc func(v any) error

func (f decoderFunc) Decode(v any) error { return f(v) }

// newHooksRequestDecoder returns a Goa request decoder factory that:
//   - transparently decompresses gzip-encoded request bodies (the OTel
//     Collector otlphttp exporter defaults to compression: gzip),
//   - dispatches to formDecoder for x-www-form-urlencoded bodies,
//   - falls through to the stock JSON decoder otherwise, and
//   - on decode failure, logs the content headers and a body sample so the
//     next opaque 400 is actually diagnosable.
func newHooksRequestDecoder(logger *slog.Logger) func(r *http.Request) goahttp.Decoder {
	return func(r *http.Request) goahttp.Decoder {
		ctx := r.Context()
		contentType := r.Header.Get("Content-Type")
		contentEncoding := r.Header.Get("Content-Encoding")

		raw, err := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if err != nil {
			wrapped := fmt.Errorf("read hooks request body: %w", err)
			return decoderFunc(func(_ any) error {
				logDecodeFailure(ctx, logger, wrapped, nil, contentType, contentEncoding)
				return wrapped
			})
		}

		body := raw
		if strings.EqualFold(contentEncoding, "gzip") {
			gz, gerr := gzip.NewReader(bytes.NewReader(raw))
			if gerr != nil {
				wrapped := fmt.Errorf("open gzip reader for hooks request: %w", gerr)
				return decoderFunc(func(_ any) error {
					logDecodeFailure(ctx, logger, wrapped, raw, contentType, contentEncoding)
					return wrapped
				})
			}
			decompressed, gerr := io.ReadAll(gz)
			_ = gz.Close()
			if gerr != nil {
				wrapped := fmt.Errorf("decompress gzip hooks request body: %w", gerr)
				return decoderFunc(func(_ any) error {
					logDecodeFailure(ctx, logger, wrapped, raw, contentType, contentEncoding)
					return wrapped
				})
			}
			body = decompressed
		}

		// Hand the buffered body off to the inner decoder via a fresh reader.
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		r.Header.Del("Content-Encoding")

		var inner goahttp.Decoder
		if strings.Contains(contentType, "application/x-www-form-urlencoded") {
			inner = &formDecoder{r: r}
		} else {
			inner = goahttp.RequestDecoder(r)
		}

		return decoderFunc(func(v any) error {
			if derr := inner.Decode(v); derr != nil {
				logDecodeFailure(ctx, logger, derr, body, contentType, contentEncoding)
				return fmt.Errorf("decode hooks request body: %w", derr)
			}
			return nil
		})
	}
}

func logDecodeFailure(ctx context.Context, logger *slog.Logger, err error, body []byte, contentType, contentEncoding string) {
	if logger == nil {
		return
	}
	sample := body
	if len(sample) > decodeBodySampleLimit {
		sample = sample[:decodeBodySampleLimit]
	}
	logger.WarnContext(ctx, "hooks request decode failed",
		attr.SlogError(err),
		attr.SlogValueAny(map[string]any{
			"content_type":     contentType,
			"content_encoding": contentEncoding,
			"body_len":         len(body),
			"body_sample":      string(sample),
		}),
	)
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
	claudeMetadata := extractSessionMetadata(payload)

	logger := s.logger.With(
		attr.SlogHookSource("claude"),
		attr.SlogHookEvent("Logs"),
		attr.SlogServiceName(claudeMetadata.ServiceName),
		attr.SlogGenAIConversationID(claudeMetadata.SessionID),
		attr.SlogAuthUserEmail(claudeMetadata.UserEmail),
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

	if claudeMetadata.SessionID == "" {
		logger.WarnContext(ctx, "claude OTEL logs payload contained no session ID",
			attr.SlogEvent("claude_logs_no_session"),
		)
		return nil
	}

	userID := s.resolveUserByEmail(ctx, claudeMetadata.UserEmail, orgID)

	completeMetadata := SessionMetadata{
		SessionID:   claudeMetadata.SessionID,
		ServiceName: claudeMetadata.ServiceName,
		UserEmail:   claudeMetadata.UserEmail,
		UserID:      userID,
		ClaudeOrgID: claudeMetadata.ClaudeOrgID,
		GramOrgID:   orgID,
		ProjectID:   projectID,
	}

	if err := s.cache.Set(ctx, sessionCacheKey(completeMetadata.SessionID), completeMetadata, 24*time.Hour); err != nil {
		logger.ErrorContext(ctx, "Failed to store session metadata",
			attr.SlogEvent("claude_logs_cache_set_failed"),
			attr.SlogError(err),
		)
	}

	s.flushPendingHooks(ctx, completeMetadata.SessionID, &completeMetadata)

	logger.InfoContext(ctx, "Stored session metadata",
		attr.SlogEvent("session_validated"),
	)

	return nil
}

// Metrics handles authenticated OTEL metrics data from Claude Code
func (s *Service) Metrics(ctx context.Context, payload *gen.MetricsPayload) error {
	logger := s.logger.With(
		attr.SlogHookSource("claude"),
		attr.SlogHookEvent("Metrics"),
	)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		logger.WarnContext(ctx, "rejected unauthorized claude OTEL metrics request",
			attr.SlogEvent("claude_metrics_unauthorized"),
		)
		return oops.E(oops.CodeUnauthorized, nil, "unauthorized")
	}

	orgID := authCtx.ActiveOrganizationID
	projectID := authCtx.ProjectID.String()

	logger.InfoContext(ctx, "Received Claude token metrics",
		attr.SlogEvent("claude_metrics"),
		attr.SlogOrganizationID(orgID),
		attr.SlogProjectID(projectID),
	)

	// Write metrics to ClickHouse
	s.writeMetricsToClickHouse(ctx, payload, orgID, projectID)

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
	// project_slug header may be set even when the API key isn't validated
	// yet on this optional-auth endpoint — log it as a hint up front.
	projectSlugHint := conv.PtrValOr(payload.ProjectSlugInput, "")
	hasPluginAuth := hasOptionalPluginAuth(payload)

	// service.name lives in cached SessionMetadata seeded by the OTEL Logs
	// endpoint. May be empty for the first hooks of a session (before OTEL
	// catches up) or for OTEL-disabled clients — non-fatal, log either way.
	serviceName := ""
	if sid := conv.PtrValOr(payload.SessionID, ""); sid != "" {
		if md, err := s.getSessionMetadata(ctx, sid); err == nil {
			serviceName = md.ServiceName
		}
	}

	logger := s.logger.With(
		attr.SlogHookSource("claude"),
		attr.SlogHookEvent(payload.HookEventName),
		attr.SlogServiceName(serviceName),
		attr.SlogToolName(conv.PtrValOr(payload.ToolName, "")),
		attr.SlogGenAIConversationID(conv.PtrValOr(payload.SessionID, "")),
		attr.SlogProjectSlug(projectSlugHint),
		attr.SlogHookHasPluginAuth(hasPluginAuth),
	)

	logger.InfoContext(ctx, "claude hook received",
		attr.SlogEvent("claude_hook"),
	)

	if hasPluginAuth {
		// Auth is optional. Returning a 401 on failure deadlocks the client:
		// send_hook.sh maps any non-2xx to "block all tool calls", but
		// recovering (e.g. `gram login`) requires Bash, which the hook just
		// blocked. On failure we leave ctx unchanged and fall through to the
		// same path a no-headers request takes — recordHook buffers the event
		// in Redis, and the OTEL Logs endpoint flushes it once the session is
		// validated. Policies that need auth context degrade gracefully.
		if authedCtx, err := s.authorizePluginRequest(ctx, conv.PtrValOr(payload.ApikeyToken, ""), projectSlugHint); err != nil {
			logger.WarnContext(ctx, "plugin auth failed on claude hook; falling back to OTEL-buffered path",
				attr.SlogEvent("claude_hook_auth_failed"),
				attr.SlogError(err),
			)
		} else {
			ctx = authedCtx
			logger = s.withAuthContext(ctx, logger)
			logger.InfoContext(ctx, "plugin auth ok on claude hook",
				attr.SlogEvent("claude_hook_auth_ok"),
			)
		}
	}

	s.recordHook(ctx, payload)

	// Route to appropriate handler based on hook type
	var (
		result *gen.ClaudeHookResult
		err    error
	)
	switch payload.HookEventName {
	case "SessionStart":
		result, err = s.handleSessionStart(ctx, payload)
	case "PreToolUse":
		result, err = s.handlePreToolUse(ctx, payload)
	case "PostToolUse":
		result, err = s.handlePostToolUse(ctx, payload)
	case "PostToolUseFailure":
		result, err = s.handlePostToolUseFailure(ctx, payload)
	case "UserPromptSubmit":
		result, err = s.handleUserPromptSubmit(ctx, payload)
	case "Stop":
		result, err = s.handleStop(ctx, payload)
	case "SessionEnd":
		result, err = s.handleSessionEnd(ctx, payload)
	case "Notification":
		result, err = s.handleNotification(ctx, payload)
	default:
		logger.ErrorContext(ctx, fmt.Sprintf("Unknown hook event: %s", payload.HookEventName))
		result = makeHookResult(payload.HookEventName)
	}

	return result, err
}

func (s *Service) handleSessionStart(ctx context.Context, payload *gen.ClaudePayload) (*gen.ClaudeHookResult, error) {
	s.captureMCPListSnapshot(ctx, payload)

	// Always allow sessions to start
	continueVal := true
	result := makeHookResult(payload.HookEventName)
	result.Continue = &continueVal
	return result, nil
}

// captureMCPListSnapshot parses the MCP inventory shipped by the
// SessionStart hook script and caches it under sessionMCPListCacheKey.
// SessionStart re-fires on startup/resume/clear/compact, so this re-syncs
// whenever Claude reloads the session — which is the closest thing to a
// config-change signal the CLI offers today.
//
// Two payload shapes are supported, set by the hook script depending on
// the execution environment it detected:
//   - additional_data.mcp_inventory_claude_code: raw text from
//     `claude mcp list`. Parsed by ParseClaudeMCPList.
//   - additional_data.mcp_inventory_cowork: structured array scraped from
//     .mcp.json files inside cowork (no `claude` CLI available). Parsed
//     by ParseCoworkMCPInventory.
func (s *Service) captureMCPListSnapshot(ctx context.Context, payload *gen.ClaudePayload) {
	if payload.SessionID == nil || *payload.SessionID == "" || payload.AdditionalData == nil {
		return
	}

	var entries []MCPServerEntry
	var variant string
	switch {
	case payload.AdditionalData["mcp_inventory_claude_code"] != nil:
		raw, ok := payload.AdditionalData["mcp_inventory_claude_code"].(string)
		if !ok || raw == "" {
			return
		}
		entries = ParseClaudeMCPList(raw)
		variant = agentVariantClaudeCode
	case payload.AdditionalData["mcp_inventory_cowork"] != nil:
		entries = ParseCoworkMCPInventory(payload.AdditionalData["mcp_inventory_cowork"])
		variant = agentVariantCowork
	default:
		return
	}

	key := sessionMCPListCacheKey(*payload.SessionID)
	if err := s.cache.Set(ctx, key, entries, sessionMCPListTTL); err != nil {
		s.logger.WarnContext(ctx, "failed to cache MCP list snapshot",
			attr.SlogEvent("claude_hook_mcp_list_cache_set_failed"),
			attr.SlogError(err),
		)
		return
	}

	variantKey := sessionAgentVariantCacheKey(*payload.SessionID)
	if err := s.cache.Set(ctx, variantKey, variant, sessionMCPListTTL); err != nil {
		s.logger.WarnContext(ctx, "failed to cache session agent variant",
			attr.SlogEvent("claude_hook_agent_variant_cache_set_failed"),
			attr.SlogError(err),
		)
	}
}

// refreshMCPListTTL extends the MCP list cache TTL for the session if the
// key exists. Called from recordHook on every Claude hook event so the
// snapshot survives as long as the session is active.
func (s *Service) refreshMCPListTTL(ctx context.Context, sessionID string) {
	if sessionID == "" {
		return
	}
	if err := s.cache.Expire(ctx, sessionMCPListCacheKey(sessionID), sessionMCPListTTL); err != nil {
		s.logger.WarnContext(ctx, "failed to refresh MCP list TTL",
			attr.SlogEvent("claude_hook_mcp_list_ttl_refresh_failed"),
			attr.SlogError(err),
		)
	}
	if err := s.cache.Expire(ctx, sessionAgentVariantCacheKey(sessionID), sessionMCPListTTL); err != nil {
		s.logger.WarnContext(ctx, "failed to refresh session agent variant TTL",
			attr.SlogEvent("claude_hook_agent_variant_ttl_refresh_failed"),
			attr.SlogError(err),
		)
	}
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
	logger := s.withAuthContext(ctx, s.logger.With(
		attr.SlogHookSource("claude"),
		attr.SlogHookEvent(payload.HookEventName),
	))

	if payload.SessionID == nil || *payload.SessionID == "" {
		logger.WarnContext(ctx, "Tool event called without session ID",
			attr.SlogEvent("claude_hook_no_session"),
		)
		return
	}

	// Persistence outlives the request: Claude Code may close the connection
	// the instant the hook returns (Stop especially), which would otherwise
	// cancel the in-flight INSERT and drop the chat message.
	ctx = context.WithoutCancel(ctx)

	sessionID := *payload.SessionID
	logger = logger.With(attr.SlogGenAIConversationID(sessionID))

	// Every hook event for this session is a heartbeat — extend the MCP
	// list snapshot TTL so it survives long-running sessions and only
	// expires after ~12h of true inactivity.
	s.refreshMCPListTTL(ctx, sessionID)

	// Both plugin-authenticated and OTEL-only requests go through the same
	// Redis-buffered flow: persist when session metadata is in the cache,
	// buffer otherwise so flushPendingHooks can re-persist with full
	// attribution once /rpc/hooks.otel.logs lands. Claude hook payloads
	// don't carry user.email, so even plugin requests would land with an
	// empty user_email if persisted synchronously on a cache miss — Cursor
	// avoids this because its payload includes UserEmail directly.
	metadata, err := s.getSessionMetadata(ctx, sessionID)
	if err == nil {
		// Persistence does DB writes plus a Temporal workflow start, which
		// can take longer than Claude Code is willing to wait for a hook
		// response (Stop especially — the client closes the connection
		// immediately on the response). Run it detached so the response
		// returns promptly and the work completes in the background.
		go s.persistHook(ctx, payload, &metadata)
	} else {
		if err := s.bufferHook(ctx, sessionID, payload); err != nil {
			logger.ErrorContext(ctx, "Failed to buffer hook",
				attr.SlogEvent("claude_hook_buffer_failed"),
				attr.SlogError(err),
			)
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
			auditReason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			userReason := renderUserBlockReason(scanResult.UserMessage, auditReason)
			// Surface the block reason on the trace summary so the dashboard
			// shows why the call was denied. Always store the technical reason
			// — the user_message override is for the agent-facing response only.
			if metadata, err := s.getSessionMetadata(ctx, *payload.SessionID); err == nil {
				s.writeClaudeBlockToClickHouse(ctx, payload, &metadata, auditReason)
			}
			return constructBlockResponse(payload.HookEventName, userReason), nil
		}
	}

	allow := "allow"
	deny := "deny"
	result := makeHookResult(payload.HookEventName)
	output, _ := result.HookSpecificOutput.(*HookSpecificOutput)
	denyUnverifiedMCP := func() (*gen.ClaudeHookResult, error) {
		reason := claudeShadowMCPMetadataUnavailableReason
		result.SystemMessage = &reason
		if output != nil {
			output.PermissionDecision = &deny
			output.PermissionDecisionReason = &reason
		}
		return result, nil
	}

	// In Claude Code, MCP-routed tools are identified by the
	// "mcp__<server>__<tool>" name convention; native tools (Read, Edit,
	// Bash, ...) are skipped.
	rawToolName := ""
	if payload.ToolName != nil {
		rawToolName = *payload.ToolName
	}
	parsed := parseClaudeToolName(rawToolName)
	if !parsed.IsMCP {
		if output != nil {
			output.PermissionDecision = &allow
		}
		return result, nil
	}
	serverPrefix := parsed.Server
	mcpToolName := parsed.Tool

	sessionID := ""
	if payload.SessionID != nil {
		sessionID = *payload.SessionID
	}
	if sessionID == "" {
		// No session yet to derive org/project from. Native tools are already
		// skipped above; MCP calls must fail closed because buffered telemetry
		// cannot undo an already-allowed tool call.
		s.logger.WarnContext(ctx, "claude PreToolUse fired without session id; denying MCP tool call",
			attr.SlogEvent("claude_hook_pretooluse_no_session"),
		)
		return denyUnverifiedMCP()
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
			UserID:      authCtx.UserID,
			ClaudeOrgID: "",
			GramOrgID:   authCtx.ActiveOrganizationID,
			ProjectID:   authCtx.ProjectID.String(),
		}
		if cached, err := s.getSessionMetadata(ctx, sessionID); err == nil {
			metadata = mergeClaudeAuthContextMetadata(metadata, cached)
		}
	} else {
		var err error
		metadata, err = s.getSessionMetadata(ctx, sessionID)
		if err != nil {
			// OTEL path with no cached metadata yet. Native tools are already
			// skipped above; MCP calls must fail closed because buffered
			// telemetry cannot undo an already-allowed tool call.
			s.logger.WarnContext(ctx, "claude PreToolUse fired before session metadata available; denying MCP tool call",
				attr.SlogEvent("claude_hook_pretooluse_no_metadata"),
				attr.SlogHookSource("claude"),
				attr.SlogHookEvent(payload.HookEventName),
				attr.SlogGenAIConversationID(sessionID),
				attr.SlogToolName(rawToolName),
				attr.SlogError(err),
			)
			return denyUnverifiedMCP()
		}
	}

	policy := s.lookupShadowMCPBlockingPolicy(ctx, metadata.ProjectID)
	if policy == nil {
		if output != nil {
			output.PermissionDecision = &allow
		}
		return result, nil
	}

	// Look up the cached `claude mcp list` snapshot captured at SessionStart.
	// If it's missing we can't enforce the policy — deny with a retry-or-
	// restart message so the user knows the guard is fail-closed rather
	// than silently allowing.
	entries, cacheErr := s.getCachedMCPList(ctx, sessionID)
	if cacheErr != nil {
		auditReason := "missing MCP list snapshot for session"
		userReason := "Speakeasy blocked this tool call: MCP server configuration is not available yet. Please retry in a moment, or restart Claude Code if the issue persists."
		s.logger.With(
			attr.SlogHookSource("claude"),
			attr.SlogHookEvent(payload.HookEventName),
			attr.SlogOrganizationID(metadata.GramOrgID),
			attr.SlogProjectID(metadata.ProjectID),
			attr.SlogGenAIConversationID(sessionID),
			attr.SlogToolName(rawToolName),
		).InfoContext(ctx, "denying claude tool call: no cached MCP list",
			attr.SlogEvent("claude_hook_denied_no_mcp_list"),
			attr.SlogError(cacheErr),
			attr.SlogRiskPolicyID(policy.ID),
			attr.SlogRiskPolicyName(policy.Name),
		)
		s.writeClaudeBlockToClickHouse(ctx, payload, &metadata, auditReason)
		return constructBlockResponse(payload.HookEventName, userReason), nil
	}

	matched := matchCachedMCPEntry(entries, serverPrefix)
	var detail string
	switch {
	case matched == nil:
		detail = fmt.Sprintf("MCP server %q is not in the active configuration", serverPrefix)
	case matched.URL != "" && !s.isGramHostedMCPURLForOrg(ctx, matched.URL, metadata.GramOrgID):
		detail = fmt.Sprintf("MCP server %q is not Gram-hosted (URL: %s)", serverPrefix, matched.URL)
	case matched.URL == "" && matched.Command != "":
		// Local stdio servers have no URL, so the Gram-hosted check above
		// can't apply. Treat them as shadow MCPs until explicitly approved
		// by command.
		detail = fmt.Sprintf("MCP server %q is a local stdio server (command: %s)", serverPrefix, matched.Command)
	case matched.URL == "" && matched.Command == "":
		// Defensive: the parser populates either URL or Command for every
		// real entry, but if a future format slips past it we'd rather
		// fail closed with a clear reason than silently allow.
		detail = fmt.Sprintf("MCP server %q has no recognizable target", serverPrefix)
	}
	evidence := shadowmcp.AccessEvidence{
		FullURL:        "",
		URLHost:        "",
		ServerIdentity: mcpServerIdentityFromToolName(rawToolName),
	}
	if matched != nil {
		evidence.FullURL = matched.URL
		evidence.ServerIdentity = serverPrefix
		if matched.URL == "" && matched.Command != "" {
			evidence.ServerIdentity = matched.Command
		}
	}
	// Access Rules can explicitly allow a shadow MCP server by URL, command,
	// or server identity. Deny rules win inside EvaluateAccessRules.
	if detail != "" {
		if s.shadowMCPClient == nil {
			detail = "Shadow MCP validation is unavailable"
		} else {
			decision := s.shadowMCPClient.EvaluateAccessRules(ctx, metadata.GramOrgID, metadata.ProjectID, evidence)
			s.logger.InfoContext(ctx, "evaluated shadow mcp access rules",
				attr.SlogEvent("shadow_mcp_access_rule_evaluated"),
				attr.SlogOrganizationID(metadata.GramOrgID),
				attr.SlogProjectID(metadata.ProjectID),
				attr.SlogValueAny(map[string]any{
					"outcome":                   decision.Outcome,
					"shadow_mcp_access_rule_id": decision.RuleID,
					"reason":                    decision.Reason,
					"tool_name":                 mcpToolName,
				}),
			)
			if decision.Allows() {
				matchedURL, matchedCommand := "", ""
				if matched != nil {
					matchedURL = matched.URL
					matchedCommand = matched.Command
				}
				s.logger.InfoContext(ctx, "shadow-mcp call allowed via approval",
					attr.SlogEvent("claude_hook_allowlist_allow"),
					attr.SlogToolName(rawToolName),
					attr.SlogRiskPolicyID(policy.ID),
					attr.SlogValueAny(map[string]any{
						"serverPrefix":   serverPrefix,
						"matchedURL":     matchedURL,
						"matchedCommand": matchedCommand,
					}),
				)
				detail = ""
			} else if decision.Reason != "" {
				detail = decision.Reason
			}
		}
	}
	if detail != "" {
		auditReason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", policy.Name, detail)
		userReason := s.renderShadowMCPUserBlockReason(ctx, shadowMCPRequestLinkParams{
			OrganizationID:  metadata.GramOrgID,
			ProjectID:       metadata.ProjectID,
			RequesterUserID: metadata.UserID,
			UserMessage:     policy.UserMessage,
			AuditReason:     auditReason,
			Evidence:        evidence,
			ToolName:        mcpToolName,
			ToolInput:       payload.ToolInput,
			RiskPolicyID:    policy.ID,
		})
		matchedURL := ""
		if matched != nil {
			matchedURL = matched.URL
		}
		s.logger.With(
			attr.SlogHookSource("claude"),
			attr.SlogHookEvent(payload.HookEventName),
			attr.SlogOrganizationID(metadata.GramOrgID),
			attr.SlogProjectID(metadata.ProjectID),
			attr.SlogGenAIConversationID(sessionID),
			attr.SlogToolName(rawToolName),
		).InfoContext(ctx, "denying claude tool call: non-gram MCP server",
			attr.SlogEvent("claude_hook_denied"),
			attr.SlogHookBlockReason(detail),
			attr.SlogRiskPolicyID(policy.ID),
			attr.SlogRiskPolicyName(policy.Name),
			attr.SlogValueAny(map[string]any{
				"serverPrefix": serverPrefix,
				"matchedURL":   matchedURL,
			}),
		)
		// Record a companion ClickHouse entry with gram.hook.block_reason set
		// so the trace_summaries materialized view can flag this trace as
		// blocked. Shares the original PreToolUse trace_id (derived from
		// tool_use_id) so both rows aggregate into the same trace summary.
		// Always store the technical reason — the user_message override
		// is for the agent-facing response only.
		s.writeClaudeBlockToClickHouse(ctx, payload, &metadata, auditReason)

		// Surface the block in the Recent Findings table (risk_results)
		// alongside batch-scanner findings, with the URL in the match
		// column so the dashboard can filter and offer "Exclude from
		// policy" actions against the URL itself.
		s.recordShadowMCPBlockFinding(ctx, payload, &metadata, policy, matched, serverPrefix, detail)

		return constructBlockResponse(payload.HookEventName, userReason), nil
	}

	if output != nil {
		output.PermissionDecision = &allow
	}
	return result, nil
}

func mergeClaudeAuthContextMetadata(metadata SessionMetadata, cached SessionMetadata) SessionMetadata {
	metadata.ServiceName = cached.ServiceName
	metadata.UserEmail = cached.UserEmail
	if cached.UserID != "" {
		metadata.UserID = cached.UserID
	}
	metadata.ClaudeOrgID = cached.ClaudeOrgID
	return metadata
}

// claudeMCPToolName returns the bare tool name and true if rawName follows the
// "mcp__<server>__<tool>" convention used by Claude Code for MCP-routed tools.
// Returns ("", false) for native Claude Code tools (Read, Edit, Bash, etc.).
func (s *Service) handlePostToolUse(ctx context.Context, payload *gen.ClaudePayload) (*gen.ClaudeHookResult, error) {
	return makeHookResult(payload.HookEventName), nil
}

// recordShadowMCPBlockFinding writes a risk_results row so the Recent
// Findings table reflects live hook-time blocks alongside batch-scanner
// findings. Best-effort: the block decision has already been made and
// returned to the user, so failures here just log. The chat_message_id
// FK requires that recordHook successfully persisted the tool-call
// message (gated by FeatureSessionCapture) — when it didn't, we skip.
func (s *Service) recordShadowMCPBlockFinding(
	ctx context.Context,
	payload *gen.ClaudePayload,
	metadata *SessionMetadata,
	policy *risk.ShadowMCPPolicy,
	matched *MCPServerEntry,
	serverPrefix string,
	detail string,
) {
	if s.repo == nil || policy == nil || payload.SessionID == nil || payload.ToolUseID == nil {
		return
	}

	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		s.logger.WarnContext(ctx, "shadow-mcp block: invalid project id",
			attr.SlogEvent("claude_hook_block_finding_skip"), attr.SlogError(err))
		return
	}
	policyID, err := uuid.Parse(policy.ID)
	if err != nil {
		s.logger.WarnContext(ctx, "shadow-mcp block: invalid policy id",
			attr.SlogEvent("claude_hook_block_finding_skip"), attr.SlogError(err))
		return
	}

	chatID := sessionIDToUUID(*payload.SessionID)
	msgID, err := s.repo.FindAssistantToolCallMessageID(ctx, repo.FindAssistantToolCallMessageIDParams{
		ProjectID:  uuid.NullUUID{UUID: projectID, Valid: true},
		ChatID:     chatID,
		ToolCallID: *payload.ToolUseID,
	})
	if err != nil {
		// Most common cause: SessionCapture feature flag is off, so the
		// tool-call chat_message was never persisted. The ClickHouse path
		// still records the block; only the Recent Findings surfacing is
		// skipped.
		s.logger.DebugContext(ctx, "shadow-mcp block: no chat_message found for tool_use_id; skipping risk_result write",
			attr.SlogEvent("claude_hook_block_finding_no_message"),
			attr.SlogError(err),
		)
		return
	}

	// match identifies the server in the dashboard's Recent Findings row and
	// is also the value the "Exclude from policy" action sends back to
	// approveShadowMCP. Prefer the URL for HTTP/SSE entries, then the stdio
	// Command for local servers, and finally fall back to the server-prefix
	// portion of the tool name (e.g. "mise" from "mcp__mise__run_task") when
	// the snapshot didn't yield a matched entry — anything but the raw tool
	// name, which is too granular to allowlist on.
	match := ""
	if matched != nil {
		switch {
		case matched.URL != "":
			match = matched.URL
		case matched.Command != "":
			match = matched.Command
		}
	}
	if match == "" {
		match = serverPrefix
	}
	description := detail
	if matched != nil && matched.Name != "" {
		description = fmt.Sprintf("%s (server: %s)", detail, matched.Name)
	}

	// Use UUIDv7 so the row sorts in insertion order alongside scanner
	// findings: ListRiskResultsByProjectFound paginates with ORDER BY id
	// DESC, which only behaves as "most recent first" when every inserted
	// id is time-ordered. uuid.New() (v4) is random and would interleave
	// hook-time block rows at arbitrary positions in the Recent Findings
	// table.
	resultID, err := uuid.NewV7()
	if err != nil {
		s.logger.WarnContext(ctx, "shadow-mcp block: failed to generate uuidv7",
			attr.SlogEvent("claude_hook_block_finding_skip"), attr.SlogError(err))
		return
	}
	insertParams := repo.InsertShadowMCPBlockResultParams{
		ID:                resultID,
		ProjectID:         projectID,
		OrganizationID:    metadata.GramOrgID,
		RiskPolicyID:      policyID,
		RiskPolicyVersion: policy.Version,
		ChatMessageID:     msgID,
		Description:       pgtype.Text{String: description, Valid: description != ""},
		Match:             pgtype.Text{String: match, Valid: match != ""},
		Confidence:        pgtype.Float8{Float64: 1.0, Valid: true},
	}
	if err := s.repo.InsertShadowMCPBlockResult(ctx, insertParams); err != nil {
		s.logger.WarnContext(ctx, "shadow-mcp block: failed to insert risk_result",
			attr.SlogEvent("claude_hook_block_finding_insert_failed"),
			attr.SlogError(err),
		)
	}
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
