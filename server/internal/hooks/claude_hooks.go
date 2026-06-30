package hooks

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
	claudeevents "github.com/speakeasy-api/gram/server/internal/hookevents/adapters/claude"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

type pendingShadowMCPBlockFinding struct {
	ID                string `json:"id"`
	ToolCallID        string `json:"tool_call_id"`
	PolicyID          string `json:"policy_id"`
	RiskPolicyVersion int64  `json:"risk_policy_version"`
	Description       string `json:"description"`
	Match             string `json:"match"`
}

// decodeBodySampleLimit caps how many bytes of a failing request body get
// logged, so we don't dump megabytes of OTLP into the logs on every bad
// payload.
const decodeBodySampleLimit = 1024

// claudeHookStopCollectionVersion is the plugin hook protocol version at which
// the plugin captures the full transcript via ClaudeMessages on Stop/SubagentStop.
// At or above it, the per-event handlers are blocking-only and persist nothing —
// see recordHook. Plugins sending no X-Gram-Hook-Version (older builds) persist
// on the per-event handlers as before.
const claudeHookStopCollectionVersion = 2

const claudeShadowMCPMetadataUnavailableReason = "Speakeasy could not verify this MCP tool call. Try restarting Claude, or running /reload-plugins."

// Diagnostic codes appended to claudeShadowMCPMetadataUnavailableReason. The
// three fail-closed branches that can't verify an MCP call all surface the
// same generic copy, so the code is the only thing that tells a user (or
// support) which branch denied the call from the message alone. They mirror
// the slog event suffixes on each branch.
const (
	denyCodeNoSession   = "NO_SESSION"
	denyCodeNoMetadata  = "NO_METADATA"
	denyCodeNoUserEmail = "NO_USER_EMAIL"
)

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

// Metrics handles authenticated OTEL metrics data from Claude Code
func (s *Service) Metrics(ctx context.Context, payload *gen.MetricsPayload) error {
	logger := s.logger.With(
		attr.SlogHookSource("claude"),
		attr.SlogHookEvent("Metrics"),
	)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.E(oops.CodeUnauthorized, errors.New("rejected unauthorized claude OTEL metrics request"), "unauthorized").LogWarn(ctx, logger, attr.SlogEvent("claude_metrics_unauthorized"))
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

// Claude is the unified endpoint for all Claude Code hook events.
func (s *Service) Claude(ctx context.Context, payload *gen.ClaudePayload) (res *gen.ClaudeHookResult, err error) {
	start := time.Now()

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
	hookEventName := payload.HookEventName
	if parsedEvent, ok := parseClaudeHookEvent(payload.HookEventName); ok {
		hookEventName = string(parsedEvent)
	}
	orgSlug := ""
	outcome := hookMetricOutcomeAccepted
	defer func() {
		if err != nil && outcome == hookMetricOutcomeAccepted {
			outcome = hookMetricOutcomeFailure
		}
		s.metrics.RecordHookEventDuration(ctx, "claude", hookEventName, outcome, orgSlug, time.Since(start))
	}()

	if hasPluginAuth {
		// Auth is optional. Returning a 401 on failure deadlocks the client:
		// send_hook.sh maps any non-2xx to "block all tool calls", but
		// recovering (e.g. `gram login`) requires Bash, which the hook just
		// blocked. On failure we leave ctx unchanged and fall through to the
		// same path a no-headers request takes — recordHook buffers the event
		// in Redis, and the OTEL Logs endpoint flushes it once the session is
		// validated. Policies that need auth context degrade gracefully.
		if authedCtx, err := s.authorizePluginRequest(ctx, conv.PtrValOr(payload.ApikeyToken, ""), projectSlugHint); err != nil {
			outcome = hookMetricOutcomeUnauthorized
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
	if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil {
		orgSlug = authCtx.OrganizationSlug
	}

	// Claim the per-invocation idempotency token once, before persistence and
	// the block side-effects in the handlers below. A retry re-sends the same
	// token: the decision (scan) still re-runs so the user stays blocked, but
	// tagging the context as a duplicate suppresses the duplicate writes
	// (persistence, block-reason telemetry, shadow-MCP findings).
	if !s.claimHookIdempotency(ctx, conv.PtrValOr(payload.IdempotencyKey, "")) {
		ctx = withHookDuplicate(ctx)
	}

	s.recordHook(ctx, payload)

	hookEvent, err := s.normalizeClaudeHookEvent(ctx, payload, time.Now())
	if err != nil {
		return nil, err
	}
	if hookEvent == nil {
		logger.ErrorContext(ctx, fmt.Sprintf("Unknown hook event: %s", payload.HookEventName))
		return makeHookResult(payload.HookEventName), nil
	}

	// Route to appropriate handler based on hook type
	var (
		result *gen.ClaudeHookResult
	)
	switch ev := hookEvent.(type) {
	case *hookevents.SessionStart:
		result, err = s.handleSessionStart(ctx, ev)
	case *hookevents.ConfigChange:
		result, err = s.handleConfigChange(ctx, ev)
	case *hookevents.BeforeToolUse:
		result, err = s.handlePreToolUse(ctx, ev)
	case *hookevents.AfterToolUse:
		result, err = s.handlePostToolUse(ctx, ev)
	case *hookevents.AfterToolUseFailure:
		result, err = s.handlePostToolUseFailure(ctx, ev)
	case *hookevents.UserPromptSubmit:
		result, err = s.handleUserPromptSubmit(ctx, ev)
	case *hookevents.Stop:
		result, err = s.handleStop(ctx, ev)
	case *hookevents.SessionEnd:
		result, err = s.handleSessionEnd(ctx, ev)
	case *hookevents.Notification:
		result, err = s.handleNotification(ctx, ev)
	default:
		logger.ErrorContext(ctx, fmt.Sprintf("Unknown hook event: %s", payload.HookEventName))
		result = makeHookResult(payload.HookEventName)
	}

	return result, err
}

func (s *Service) normalizeClaudeHookEvent(ctx context.Context, payload *gen.ClaudePayload, timestamp time.Time) (any, error) {
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	eventContext := hookevents.EventContext{
		OrganizationID: "",
		ProjectID:      uuid.Nil,
		User: hookevents.User{
			ID:    "",
			Email: "",
		},
	}
	if authCtx != nil && authCtx.ProjectID != nil {
		eventContext.OrganizationID = authCtx.ActiveOrganizationID
		eventContext.ProjectID = *authCtx.ProjectID
		eventContext.User.ID = authCtx.UserID
		if authCtx.Email != nil {
			eventContext.User.Email = strings.TrimSpace(*authCtx.Email)
		}
	}

	if payload == nil {
		event, err := claudeevents.Normalize(authCtx, payload, eventContext, timestamp)
		if err != nil {
			return nil, fmt.Errorf("normalize claude hook event: %w", err)
		}
		return event, nil
	}

	sessionID := conv.PtrValOr(payload.SessionID, "")
	if sessionID != "" {
		metadata, err := s.resolveClaudeSessionMetadata(ctx, sessionID, strings.TrimSpace(conv.PtrValOr(payload.UserEmail, "")))
		if err == nil {
			if projectID, parseErr := uuid.Parse(metadata.ProjectID); parseErr == nil {
				eventContext = hookevents.EventContext{
					OrganizationID: metadata.GramOrgID,
					ProjectID:      projectID,
					User: hookevents.User{
						ID:    metadata.UserID,
						Email: strings.TrimSpace(metadata.UserEmail),
					},
				}
			}
		}
	}

	event, err := claudeevents.Normalize(authCtx, payload, eventContext, timestamp)
	if err != nil {
		return nil, fmt.Errorf("normalize claude hook event: %w", err)
	}
	return event, nil
}

func claudePayloadFromEvent(ev hookevents.Event) *gen.ClaudePayload {
	payload, _ := ev.Raw.(*gen.ClaudePayload)
	return payload
}

func (s *Service) handleSessionStart(ctx context.Context, ev *hookevents.SessionStart) (*gen.ClaudeHookResult, error) {
	payload := claudePayloadFromEvent(ev.Event)
	if payload == nil {
		return makeHookResult(ev.RawEventType), nil
	}
	s.captureMCPListSnapshot(ctx, payload)

	// Always allow sessions to start
	continueVal := true
	result := makeHookResult(ev.RawEventType)
	result.Continue = &continueVal
	return result, nil
}

// handleConfigChange re-syncs the cached MCP inventory when Claude reports a
// settings file change mid-session (e.g. an MCP server was added or removed).
// The mcp_inventory.sh hook ships a fresh inventory in the payload, just as
// it does for SessionStart, so we reuse the same capture path. ConfigChange
// carries no allow/deny decision and must not block, so we never set a
// blocking result.
func (s *Service) handleConfigChange(ctx context.Context, ev *hookevents.ConfigChange) (*gen.ClaudeHookResult, error) {
	payload := claudePayloadFromEvent(ev.Event)
	if payload == nil {
		return makeHookResult(ev.RawEventType), nil
	}
	s.captureMCPListSnapshot(ctx, payload)
	return makeHookResult(ev.RawEventType), nil
}

// captureMCPListSnapshot parses the MCP inventory shipped by the
// mcp_inventory.sh hook script and caches it under sessionMCPListCacheKey.
// The script is registered against both SessionStart (which re-fires on
// startup/resume/clear/compact) and ConfigChange (which fires when a
// settings file changes mid-session), so the cached inventory re-syncs
// whenever Claude reloads the session or its configuration changes.
//
// Two payload shapes are supported, set by the hook script depending on
// the execution environment it detected:
//   - additional_data.mcp_inventory_claude_code: raw text from
//     `claude mcp list`. Parsed by ParseClaudeMCPList.
//   - additional_data.mcp_inventory_cowork: structured array scraped from
//     .mcp.json files inside cowork (no `claude` CLI available). Parsed
//     by ParseCoworkMCPInventory.
func (s *Service) captureMCPListSnapshot(ctx context.Context, payload *gen.ClaudePayload) {
	if payload.SessionID == nil || *payload.SessionID == "" {
		return
	}
	entries, variant, ok := s.parseMCPInventoryFromPayload(ctx, payload)
	if !ok {
		return
	}
	s.cacheMCPListSnapshot(ctx, *payload.SessionID, entries, variant)
}

// parseMCPInventoryFromPayload extracts the MCP inventory carried in the hook
// payload's additional_data, returning the parsed entries, the detected agent
// variant, and whether an inventory was present at all. It does not touch the
// cache, so it is safe to call from both the SessionStart/ConfigChange capture
// path and the PreToolUse enforcement path (which ships the same fields,
// replayed from the session inventory file written at SessionStart — see
// renderHookScript / renderClaudeMCPInventoryScript).
//
// Two payload shapes are supported, set by the hook script depending on the
// execution environment it detected:
//   - additional_data.mcp_inventory_claude_code: raw text from
//     `claude mcp list`. Parsed by ParseClaudeMCPList.
//   - additional_data.mcp_inventory_cowork: structured array scraped from
//     .mcp.json files inside cowork (no `claude` CLI available). Parsed
//     by ParseCoworkMCPInventory.
func (s *Service) parseMCPInventoryFromPayload(ctx context.Context, payload *gen.ClaudePayload) ([]MCPServerEntry, string, bool) {
	if payload.AdditionalData == nil {
		return nil, "", false
	}
	switch {
	case payload.AdditionalData["mcp_inventory_claude_code"] != nil:
		raw, ok := payload.AdditionalData["mcp_inventory_claude_code"].(string)
		if !ok || raw == "" {
			return nil, "", false
		}
		return ParseClaudeMCPList(raw), agentVariantClaudeCode, true
	case payload.AdditionalData["mcp_inventory_cowork"] != nil:
		raw := payload.AdditionalData["mcp_inventory_cowork"]
		entries := ParseCoworkMCPInventory(raw)
		// Diagnostic: cmux's per-run config has evolved its field names
		// across versions, so when the inventory ships but every entry
		// comes out without a ConnectorUUID we'd silently fall back to
		// rendering the UUID instead of the server name. Log a sample of
		// the raw payload to make schema drift visible without flooding
		// the logs on a healthy session.
		anyConnectorUUID := false
		for _, e := range entries {
			if e.ConnectorUUID != "" {
				anyConnectorUUID = true
				break
			}
		}
		if len(entries) > 0 && !anyConnectorUUID {
			sessionID := ""
			if payload.SessionID != nil {
				sessionID = *payload.SessionID
			}
			s.logger.WarnContext(ctx, "cowork mcp inventory has no connector_uuid on any entry",
				attr.SlogEvent("claude_hook_cowork_inventory_missing_uuid"),
				attr.SlogGenAIConversationID(sessionID),
				attr.SlogValueAny(raw),
			)
		}
		return entries, agentVariantCowork, true
	default:
		return nil, "", false
	}
}

// cacheMCPListSnapshot stores the parsed inventory and agent variant under the
// session's cache keys. Shared by the SessionStart/ConfigChange capture path
// and the PreToolUse enforcement resolver, so a payload-carried inventory
// self-heals the cache that the best-effort telemetry path later reads.
func (s *Service) cacheMCPListSnapshot(ctx context.Context, sessionID string, entries []MCPServerEntry, variant string) {
	key := sessionMCPListCacheKey(sessionID)
	if err := s.cache.Set(ctx, key, entries, sessionMCPListTTL); err != nil {
		s.logger.WarnContext(ctx, "failed to cache MCP list snapshot",
			attr.SlogEvent("claude_hook_mcp_list_cache_set_failed"),
			attr.SlogError(err),
		)
		return
	}

	variantKey := sessionAgentVariantCacheKey(sessionID)
	if err := s.cache.Set(ctx, variantKey, variant, sessionMCPListTTL); err != nil {
		s.logger.WarnContext(ctx, "failed to cache session agent variant",
			attr.SlogEvent("claude_hook_agent_variant_cache_set_failed"),
			attr.SlogError(err),
		)
	}
}

// payloadInventoryIsFresh reports whether the hook payload's MCP inventory was
// gathered live during this PreToolUse call (additional_data.mcp_inventory_fresh)
// rather than replayed from the per-session inventory file. hook.sh sets the
// flag only when it shells out to gather the inventory inline because the file
// did not exist yet; a replay from the file leaves it unset. A live gather
// reflects the agent's current MCP configuration and supersedes the cache; a
// replayed snapshot may lag a ConfigChange the server already cached, so it is
// only used to fill cache misses (see resolveMCPListForEnforcement).
func payloadInventoryIsFresh(payload *gen.ClaudePayload) bool {
	if payload.AdditionalData == nil {
		return false
	}
	switch v := payload.AdditionalData["mcp_inventory_fresh"].(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1"
	default:
		return false
	}
}

// resolveMCPListForEnforcement returns the MCP inventory the PreToolUse
// shadow-MCP guard should enforce against. The resolution order is:
//
//  1. A payload inventory gathered live this call (fresh) is authoritative —
//     it reflects the agent's current MCP configuration — so it supersedes any
//     cached snapshot and is written back to heal the cache.
//  2. Otherwise the cached SessionStart/ConfigChange snapshot when present.
//     ConfigChange refreshes the cache synchronously server-side, whereas the
//     per-session file the payload replays is written asynchronously and can
//     lag, so a replayed (non-fresh) inventory must not clobber a cache the
//     server just updated.
//  3. On a genuine cache miss (the DNO-286 window, before the async
//     SessionStart snapshot has landed) a replayed payload inventory fills the
//     gap and is written back so the best-effort telemetry path heals on later
//     events. A cache transport error is NOT a miss: it fails closed rather
//     than enforcing against a possibly-stale replay.
//
// Callers treat a returned error as fail-closed.
func (s *Service) resolveMCPListForEnforcement(ctx context.Context, payload *gen.ClaudePayload, sessionID string) ([]MCPServerEntry, error) {
	entries, variant, ok := s.parseMCPInventoryFromPayload(ctx, payload)

	if ok && payloadInventoryIsFresh(payload) {
		s.cacheMCPListSnapshot(ctx, sessionID, entries, variant)
		return entries, nil
	}

	cached, err := s.getCachedMCPList(ctx, sessionID)
	if err == nil {
		return cached, nil
	}

	// Only a genuine cache miss lets a non-fresh replayed inventory stand in: it
	// means the cache is legitimately empty (the DNO-286 window before the async
	// SessionStart snapshot lands), not that we failed to read it. On a Redis
	// transport error we cannot establish the cache-authoritative ordering, so
	// we fail closed exactly as the no-payload path does rather than enforce
	// against a possibly-stale replay.
	if ok && errors.Is(err, redisCache.ErrCacheMiss) {
		s.cacheMCPListSnapshot(ctx, sessionID, entries, variant)
		return entries, nil
	}

	return nil, err
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
		return ctx, fmt.Errorf("authorize claude hook api key: %w", err)
	}
	projectScheme := &security.APIKeyScheme{
		Name:           constants.ProjectSlugSecuritySchema,
		Scopes:         []string{},
		RequiredScopes: []string{"hooks"},
	}
	ctx, err = s.auth.Authorize(ctx, projectSlug, projectScheme)
	if err != nil {
		return ctx, fmt.Errorf("authorize claude hook project slug: %w", err)
	}
	return ctx, nil
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

	// Skip persistence for a redelivery (the token was claimed in Claude()).
	if s.isHookDuplicate(ctx) {
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

	payloadUserEmail := strings.TrimSpace(conv.PtrValOr(payload.UserEmail, ""))

	// Both plugin-authenticated and OTEL-only requests go through the same
	// Redis-buffered flow: persist when session metadata is in the cache,
	// buffer otherwise so flushPendingHooks can re-persist with full
	// attribution once /rpc/hooks.otel.logs lands. Newer plugin hooks may
	// carry user_email from the device agent; use it immediately when present
	// so Claude can attribute hooks before OTEL logs arrive. Older hooks still
	// fall back to buffering.
	authMetadata, hasAuthMetadata := s.claudeAuthContextMetadata(ctx, sessionID, payloadUserEmail)

	metadata, err := s.getSessionMetadata(ctx, sessionID)
	if err == nil {
		if hasAuthMetadata {
			metadata = s.mergeClaudeAuthContextMetadata(ctx, authMetadata, metadata)
		}
		// Persistence does DB writes plus a Temporal workflow start, which
		// can take longer than Claude Code is willing to wait for a hook
		// response (Stop especially — the client closes the connection
		// immediately on the response). Run it detached so the response
		// returns promptly and the work completes in the background.
		go s.persistHook(ctx, payload, &metadata)
		return
	}

	if hasAuthMetadata && payloadUserEmail != "" {
		go s.persistHook(ctx, payload, &authMetadata)
		return
	}

	if err := s.bufferHook(ctx, sessionID, payload); err != nil {
		logger.ErrorContext(ctx, "Failed to buffer hook",
			attr.SlogEvent("claude_hook_buffer_failed"),
			attr.SlogError(err),
		)
	}
}

func (s *Service) claudeAuthContextMetadata(ctx context.Context, sessionID, userEmail string) (SessionMetadata, bool) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return SessionMetadata{
			SessionID:   "",
			ServiceName: "",
			UserEmail:   "",
			UserID:      "",
			ClaudeOrgID: "",
			GramOrgID:   "",
			ProjectID:   "",
		}, false
	}

	metadata := SessionMetadata{
		SessionID:   sessionID,
		ServiceName: "",
		UserEmail:   userEmail,
		UserID:      "",
		ClaudeOrgID: "",
		GramOrgID:   authCtx.ActiveOrganizationID,
		ProjectID:   authCtx.ProjectID.String(),
	}
	if metadata.UserEmail != "" {
		metadata.UserID = s.resolveUserByEmail(ctx, metadata.UserEmail, metadata.GramOrgID)
	}

	return metadata, true
}

func (s *Service) resolveClaudeSessionMetadata(ctx context.Context, sessionID, userEmail string) (SessionMetadata, error) {
	authMetadata, hasAuthMetadata := s.claudeAuthContextMetadata(ctx, sessionID, strings.TrimSpace(userEmail))

	metadata, err := s.getSessionMetadata(ctx, sessionID)
	if err == nil {
		if hasAuthMetadata {
			metadata = s.mergeClaudeAuthContextMetadata(ctx, authMetadata, metadata)
		} else {
			metadata.UserID = s.resolveUserByEmail(ctx, metadata.UserEmail, metadata.GramOrgID)
		}
		return metadata, nil
	}

	if hasAuthMetadata {
		return authMetadata, nil
	}
	return SessionMetadata{}, err
}

func (s *Service) persistHook(ctx context.Context, payload *gen.ClaudePayload, metadata *SessionMetadata) {
	metadata.UserEmail = strings.TrimSpace(metadata.UserEmail)
	if metadata.UserEmail == "" {
		s.logger.WarnContext(ctx, "skipping claude hook persistence without user email",
			attr.SlogEvent("claude_hook_persist_no_user_email"),
			attr.SlogHookSource("claude"),
			attr.SlogHookEvent(payload.HookEventName),
			attr.SlogGenAIConversationID(conv.PtrValOr(payload.SessionID, "")),
		)
		return
	}

	metadata.UserID = s.resolveUserByEmail(ctx, metadata.UserEmail, metadata.GramOrgID)

	// Stop-collection plugins persist chat_messages via ClaudeMessages on Stop, so
	// persisting them on the per-event handlers too would double-write. ClickHouse
	// tool telemetry stays per-event regardless of version (it needs per-event
	// context like duration and the live MCP snapshot) and is deduped across
	// installs inside persistToolCallEvent. Versionless plugins persist everything
	// per-event, so a server deploy is safe for hooks already in the field.
	stopCollection := payload.HookVersion != nil && *payload.HookVersion >= claudeHookStopCollectionVersion

	if isConversationEvent(payload.HookEventName) {
		// Conversation events never wrote ClickHouse, only chat_messages — nothing
		// to do here once that moves to the Stop batch.
		if stopCollection {
			return
		}
		if err := s.persistConversationEvent(ctx, payload, metadata); err != nil {
			s.logger.ErrorContext(ctx, "Failed to persist conversation event", attr.SlogError(err))
		}
		return
	}

	if err := s.persistToolCallEvent(ctx, payload, metadata, stopCollection); err != nil {
		s.logger.ErrorContext(ctx, "Failed to persist tool call event", attr.SlogError(err))
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

func (s *Service) handlePreToolUse(ctx context.Context, ev *hookevents.BeforeToolUse) (*gen.ClaudeHookResult, error) {
	payload := claudePayloadFromEvent(ev.Event)
	if payload == nil {
		return makeHookResult(ev.RawEventType), nil
	}
	if s.riskScanner != nil && ev.ConversationID != "" {
		if scanResult := s.scanToolRequestForEnforcement(ctx, ev); scanResult != nil {
			auditReason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			userReason := renderUserBlockReason(scanResult.UserMessage, auditReason)
			// Surface the block reason on the trace summary so the dashboard
			// shows why the call was denied. Always store the technical reason
			// — the user_message override is for the agent-facing response only.
			metadata, metaErr := s.getSessionMetadata(ctx, *payload.SessionID)
			if metaErr == nil {
				s.writeClaudeBlockToClickHouse(ctx, payload, &metadata, auditReason)
			}
			if blockID, err := uuid.NewV7(); err == nil {
				userReason = appendBlockURL(userReason, s.blockViewURL(blockID))
				// Prefer the email from the session metadata fetched above,
				// falling back to the raw payload when it wasn't cached.
				userEmail := conv.PtrValOr(payload.UserEmail, "")
				if metaErr == nil && strings.TrimSpace(metadata.UserEmail) != "" {
					userEmail = metadata.UserEmail
				}
				asyncCtx := context.WithoutCancel(ctx)
				// Resolve the owning user inside the goroutine so the DB lookup
				// stays off the deny hot path (a plain `go s.insert(...)` would
				// evaluate the resolveUserByEmail argument synchronously).
				go func() {
					s.insertToolCallBlock(asyncCtx, blockID, toolCallBlockParams{
						Provider:       "claude",
						OrganizationID: ev.Context.OrganizationID,
						ProjectID:      ev.Context.ProjectID,
						Reason:         auditReason,
						ToolName:       ev.ToolName,
						UserID:         s.resolveUserByEmail(asyncCtx, userEmail, ev.Context.OrganizationID),
						RiskPolicyID:   conv.StringToNullUUID(scanResult.PolicyID),
						RiskResultID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
						ChatID:         chatIDForBlock(conv.PtrValOr(payload.SessionID, "")),
						ChatMessageID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
					})
				}()
			}
			return constructBlockResponse(payload.HookEventName, userReason), nil
		}
	}

	allow := "allow"
	deny := "deny"
	result := makeHookResult(payload.HookEventName)
	output, _ := result.HookSpecificOutput.(*HookSpecificOutput)
	denyUnverifiedMCP := func(code string) (*gen.ClaudeHookResult, error) {
		reason := fmt.Sprintf("%s (err code: %s)", claudeShadowMCPMetadataUnavailableReason, code)
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
		return denyUnverifiedMCP(denyCodeNoSession)
	}
	// Plugin path: when the request authenticated via Gram-Key + Gram-Project,
	// org/project come from the auth context — same pattern as recordHook.
	// This lets the shadow-MCP guard run on the very first PreToolUse of a
	// session, before OTEL Logs has had a chance to seed Redis. Redis is still
	// consulted to enrich UserEmail / ServiceName / ClaudeOrgID for the
	// downstream ClickHouse row, but absence of cached fields is non-fatal.
	payloadUserEmail := strings.TrimSpace(conv.PtrValOr(payload.UserEmail, ""))
	metadata, err := s.resolveClaudeSessionMetadata(ctx, sessionID, payloadUserEmail)
	if err != nil {
		// OTEL path with no cached metadata yet. Native tools are already
		// skipped above; MCP calls must fail closed because buffered telemetry
		// cannot undo an already-allowed tool call.
		s.logger.WarnContext(ctx, "claude PreToolUse fired before session metadata available; denying MCP tool call",
			attr.SlogEvent("claude_hook_pretooluse_no_metadata"),
			attr.SlogHookSource("claude"),
			attr.SlogHookEvent(payload.HookEventName),
			attr.SlogGenAIConversationID(sessionID),
			attr.SlogToolName(rawToolName),
			attr.SlogError(err),
		)
		return denyUnverifiedMCP(denyCodeNoMetadata)
	}
	if strings.TrimSpace(metadata.UserEmail) == "" {
		s.logger.WarnContext(ctx, "claude PreToolUse metadata has no user email; denying MCP tool call",
			attr.SlogEvent("claude_hook_pretooluse_no_user_email"),
			attr.SlogHookSource("claude"),
			attr.SlogHookEvent(payload.HookEventName),
			attr.SlogGenAIConversationID(sessionID),
			attr.SlogToolName(rawToolName),
		)
		return denyUnverifiedMCP(denyCodeNoUserEmail)
	}

	policy := s.lookupShadowMCPBlockingPolicy(ctx, metadata.GramOrgID, metadata.ProjectID, metadata.UserID)
	if policy == nil {
		if output != nil {
			output.PermissionDecision = &allow
		}
		return result, nil
	}

	// Resolve the `claude mcp list` inventory to enforce against. The hook
	// script replays the SessionStart inventory file on every PreToolUse, so
	// this normally comes straight from the request payload and is immune to
	// the SessionStart-snapshot race (DNO-286); it falls back to the cached
	// snapshot only when the payload carried none. If neither is available we
	// can't enforce the policy — deny with a retry-or-restart message so the
	// user knows the guard is fail-closed rather than silently allowing.
	entries, cacheErr := s.resolveMCPListForEnforcement(ctx, payload, sessionID)
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
		ServerIdentity: "",
	}
	if matched != nil {
		evidence.FullURL = matched.URL
		if matched.Command != "" {
			evidence.ServerIdentity = matched.Command
		}
	}
	// A non-empty detail means this call is blocked. Return immediately for
	// clean allows; otherwise give a URL-scoped bypass grant a chance to allow
	// the call before recording and returning the block below.
	if detail == "" {
		if output != nil {
			output.PermissionDecision = &allow
		}
		return result, nil
	}

	if _, allowed := s.canBypassPolicy(ctx, metadata.GramOrgID, metadata.UserID, policy.ID, evidence, mcpToolName); allowed {
		matchedURL, matchedCommand := "", ""
		if matched != nil {
			matchedURL = matched.URL
			matchedCommand = matched.Command
		}
		s.logger.InfoContext(ctx, "shadow-mcp call allowed via risk policy bypass grant",
			attr.SlogEvent("claude_hook_policy_bypass_allow"),
			attr.SlogToolName(rawToolName),
			attr.SlogRiskPolicyID(policy.ID),
			attr.SlogValueAny(map[string]any{
				"serverPrefix":   serverPrefix,
				"matchedURL":     matchedURL,
				"matchedCommand": matchedCommand,
			}),
		)
		if output != nil {
			output.PermissionDecision = &allow
		}
		return result, nil
	}

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
	// policy" actions against the URL itself. The persisted row also
	// backs the durable block page: when it is written we append a
	// signed link to that page so the agent (and user) can open the
	// block, read the reason, and leave feedback.
	// Pre-mint the durable block id so its URL goes in the deny response
	// immediately, then persist the Recent Findings row and the block row off
	// the hot path. The page becomes valid within moments — well before a human
	// opens the link.
	// Only mint the durable block URL when the project id parses: an invalid id
	// would make insertToolCallBlock (and recordShadowMCPBlockFinding) skip the
	// write, leaving the appended /blocks/<id> link pointing at a page with no
	// backing row.
	if projectUUID, parseErr := uuid.Parse(metadata.ProjectID); parseErr != nil {
		s.logger.WarnContext(ctx, "tool call block: invalid project id; skipping durable block link",
			attr.SlogEvent("claude_hook_block_invalid_project"), attr.SlogError(parseErr))
	} else if blockID, err := uuid.NewV7(); err == nil {
		userReason = appendBlockURL(userReason, s.blockViewURL(blockID))
		asyncCtx := context.WithoutCancel(ctx)
		metaCopy := metadata
		go func() {
			resultID, msgID, _ := s.recordShadowMCPBlockFinding(asyncCtx, payload, &metaCopy, policy, matched, serverPrefix, detail)
			s.insertToolCallBlock(asyncCtx, blockID, toolCallBlockParams{
				Provider:       "claude",
				OrganizationID: metaCopy.GramOrgID,
				ProjectID:      projectUUID,
				Reason:         auditReason,
				ToolName:       mcpToolName,
				UserID:         metaCopy.UserID,
				RiskPolicyID:   conv.StringToNullUUID(policy.ID),
				RiskResultID:   conv.NilableToNullUUID(resultID),
				ChatID:         chatIDForBlock(conv.PtrValOr(payload.SessionID, "")),
				ChatMessageID:  conv.NilableToNullUUID(msgID),
			})
		}()
	}

	return constructBlockResponse(payload.HookEventName, userReason), nil
}

func (s *Service) mergeClaudeAuthContextMetadata(ctx context.Context, metadata SessionMetadata, cached SessionMetadata) SessionMetadata {
	metadata.ServiceName = cached.ServiceName
	if cached.UserEmail != "" {
		metadata.UserEmail = cached.UserEmail
	}
	metadata.UserID = s.resolveUserByEmail(ctx, metadata.UserEmail, metadata.GramOrgID)
	metadata.ClaudeOrgID = cached.ClaudeOrgID
	return metadata
}

// claudeMCPToolName returns the bare tool name and true if rawName follows the
// "mcp__<server>__<tool>" convention used by Claude Code for MCP-routed tools.
// Returns ("", false) for native Claude Code tools (Read, Edit, Bash, etc.).
func (s *Service) handlePostToolUse(ctx context.Context, ev *hookevents.AfterToolUse) (*gen.ClaudeHookResult, error) {
	return makeHookResult(ev.RawEventType), nil
}

// recordShadowMCPBlockFinding writes a risk_results row so the Recent Findings
// table reflects live hook-time blocks alongside batch-scanner findings.
// Best-effort: the block decision has already been made and returned to the
// user, so failures here just log. If the matching chat_message is not present
// yet, buffer the finding and attach it after the per-event write or Stop batch
// persists the real assistant tool-call row.
func (s *Service) recordShadowMCPBlockFinding(
	ctx context.Context,
	payload *gen.ClaudePayload,
	metadata *SessionMetadata,
	policy *risk.ShadowMCPPolicy,
	matched *MCPServerEntry,
	serverPrefix string,
	detail string,
) (uuid.UUID, uuid.UUID, bool) {
	if s.repo == nil || policy == nil || payload.SessionID == nil || payload.ToolUseID == nil || s.isHookDuplicate(ctx) {
		return uuid.Nil, uuid.Nil, false
	}

	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		s.logger.WarnContext(ctx, "shadow-mcp block: invalid project id",
			attr.SlogEvent("claude_hook_block_finding_skip"), attr.SlogError(err))
		return uuid.Nil, uuid.Nil, false
	}
	policyID, err := uuid.Parse(policy.ID)
	if err != nil {
		s.logger.WarnContext(ctx, "shadow-mcp block: invalid policy id",
			attr.SlogEvent("claude_hook_block_finding_skip"), attr.SlogError(err))
		return uuid.Nil, uuid.Nil, false
	}

	pending := buildPendingShadowMCPBlockFinding(*payload.ToolUseID, policy, matched, serverPrefix, detail)
	chatID := sessionIDToUUID(*payload.SessionID)
	msgID, err := s.repo.FindAssistantToolCallMessageID(ctx, repo.FindAssistantToolCallMessageIDParams{
		ProjectID:  uuid.NullUUID{UUID: projectID, Valid: true},
		ChatID:     chatID,
		ToolCallID: *payload.ToolUseID,
	})
	if err != nil {
		// No chat_message yet — common for stop-collection (v2) sessions, where
		// per-event persistence is skipped and the row arrives with the Stop
		// batch. Buffer the finding (with its pre-minted id) for the flush to
		// insert rather than dropping it. The durable block row links no finding
		// yet; the flush surfaces it in Recent Findings when the message lands.
		if bErr := s.bufferShadowMCPBlockFinding(ctx, *payload.SessionID, pending); bErr != nil {
			s.logger.WarnContext(ctx, "shadow-mcp block: failed to buffer risk_result pending chat_message",
				attr.SlogEvent("claude_hook_block_finding_buffer_failed"),
				attr.SlogError(bErr),
			)
			return uuid.Nil, uuid.Nil, false
		}
		s.logger.DebugContext(ctx, "shadow-mcp block: no chat_message found for tool_use_id; buffered risk_result",
			attr.SlogEvent("claude_hook_block_finding_no_message"),
			attr.SlogError(err),
		)
		return uuid.Nil, uuid.Nil, false
	}

	resultID, err := s.insertShadowMCPBlockFinding(ctx, metadata, projectID, policyID, msgID, pending)
	if err != nil {
		s.logger.WarnContext(ctx, "shadow-mcp block: failed to insert risk_result",
			attr.SlogEvent("claude_hook_block_finding_insert_failed"),
			attr.SlogError(err),
		)
		return uuid.Nil, uuid.Nil, false
	}
	return resultID, msgID, true
}

func buildPendingShadowMCPBlockFinding(toolCallID string, policy *risk.ShadowMCPPolicy, matched *MCPServerEntry, serverPrefix string, detail string) pendingShadowMCPBlockFinding {
	match := shadowMCPBlockFindingMatch(matched, serverPrefix)
	description := detail
	if matched != nil && matched.Name != "" {
		description = fmt.Sprintf("%s (server: %s)", detail, matched.Name)
	}
	return pendingShadowMCPBlockFinding{
		ID:                "",
		ToolCallID:        toolCallID,
		PolicyID:          policy.ID,
		RiskPolicyVersion: policy.Version,
		Description:       description,
		Match:             match,
	}
}

func shadowMCPBlockFindingMatch(matched *MCPServerEntry, serverPrefix string) string {
	if matched != nil {
		switch {
		case matched.URL != "":
			return matched.URL
		case matched.Command != "":
			return matched.Command
		}
	}
	return serverPrefix
}

func (s *Service) insertShadowMCPBlockFinding(
	ctx context.Context,
	metadata *SessionMetadata,
	projectID uuid.UUID,
	policyID uuid.UUID,
	msgID uuid.UUID,
	finding pendingShadowMCPBlockFinding,
) (uuid.UUID, error) {
	// Use UUIDv7 so the row sorts in insertion order alongside scanner
	// findings: ListRiskResultsByProjectFound paginates with ORDER BY id
	// DESC, which only behaves as "most recent first" when every inserted
	// id is time-ordered. uuid.New() (v4) is random and would interleave
	// hook-time block rows at arbitrary positions in the Recent Findings
	// table. A buffered finding reuses the id it was minted with so the
	// durable block row and the eventual insert agree.
	var resultID uuid.UUID
	if finding.ID != "" {
		parsed, err := uuid.Parse(finding.ID)
		if err != nil {
			return uuid.Nil, fmt.Errorf("parse pending result id: %w", err)
		}
		resultID = parsed
	} else {
		var err error
		resultID, err = uuid.NewV7()
		if err != nil {
			return uuid.Nil, fmt.Errorf("generate uuidv7: %w", err)
		}
	}
	insertParams := repo.InsertShadowMCPBlockResultParams{
		ID:                resultID,
		ProjectID:         projectID,
		OrganizationID:    metadata.GramOrgID,
		RiskPolicyID:      policyID,
		RiskPolicyVersion: finding.RiskPolicyVersion,
		ChatMessageID:     msgID,
		Description:       pgtype.Text{String: finding.Description, Valid: finding.Description != ""},
		Match:             pgtype.Text{String: finding.Match, Valid: finding.Match != ""},
		Confidence:        pgtype.Float8{Float64: 1.0, Valid: true},
	}
	if err := s.repo.InsertShadowMCPBlockResult(ctx, insertParams); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return resultID, nil
		}
		return uuid.Nil, fmt.Errorf("insert shadow-mcp block result: %w", err)
	}
	return resultID, nil
}

// writeClaudeBlockToClickHouse writes a companion ClickHouse log entry for a
// Claude PreToolUse call that the shadow-MCP guard denied. It reuses
// buildTelemetryAttributesWithMetadata so the new row shares the same trace_id
// (derived from tool_use_id) as the original PreToolUse log, and adds
// gram.hook.block_reason. trace_summaries_mv aggregates with max(), so the
// trace will surface as blocked regardless of which row arrives first.
func (s *Service) writeClaudeBlockToClickHouse(ctx context.Context, payload *gen.ClaudePayload, metadata *SessionMetadata, reason string) {
	if s.telemetryLogger == nil || reason == "" || s.isHookDuplicate(ctx) {
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
		UserInfo:   telemetry.UserInfoByIDAndEmail(metadata.UserID, metadata.UserEmail),
		Attributes: attrs,
	})
}

func (s *Service) handlePostToolUseFailure(ctx context.Context, ev *hookevents.AfterToolUseFailure) (*gen.ClaudeHookResult, error) {
	return makeHookResult(ev.RawEventType), nil
}
