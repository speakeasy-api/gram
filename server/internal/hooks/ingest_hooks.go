package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

const hookIngestSchemaV1 = "hook.ingest.v1"

const canonicalSessionCacheWriteTimeout = time.Second

// eventTypeSkillActivated is the canonical event type senders use when a
// provider reports a skill activation directly (Claude's Skill tool). Inferred
// activations (Codex heuristics) arrive as ordinary events carrying data.skill
// instead, and are distinguished by isExplicitSkillActivation.
const eventTypeSkillActivated = "skill.activated"

func isExplicitSkillActivation(payload *gen.IngestPayload) bool {
	return strings.TrimSpace(payload.Event.Type) == eventTypeSkillActivated
}

// Ingest is the feature-first hook endpoint; this path only accepts the
// canonical Gram contract. Auth is optional so hook senders stay non-blocking
// for machines that never signed in: a keyless request is acknowledged without
// processing (there is nothing to attribute it to), while a presented key that
// fails validation is a hard 401 — the sender explicitly tried to
// authenticate, and its credential-recovery path keys off that status. Events
// are attributed to the sender's self-reported user email when the payload
// carries one — plugins publish with an org-wide hooks key whose AuthContext
// identity is the publishing admin, not the developer at the keyboard —
// falling back to the token owner for personal keys and senders without a
// device agent.
func (s *Service) Ingest(ctx context.Context, payload *gen.IngestPayload) (*gen.IngestHookResult, error) {
	if err := validateCanonicalIngestPayload(payload); err != nil {
		return nil, err
	}
	if apikey := strings.TrimSpace(conv.PtrValOr(payload.ApikeyToken, "")); apikey != "" {
		authedCtx, err := s.authorizePluginRequest(ctx, apikey, strings.TrimSpace(conv.PtrValOr(payload.ProjectSlugInput, "")))
		if err != nil {
			return nil, oops.E(oops.CodeUnauthorized, err, "unauthorized")
		}
		ctx = authedCtx
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		s.logger.InfoContext(ctx, "unauthenticated hook acknowledged without processing",
			attr.SlogEvent("hooks_ingest_unauthenticated"),
			attr.SlogHookSource(strings.TrimSpace(payload.Source.Adapter)),
			attr.SlogHookEvent(strings.TrimSpace(payload.Event.Type)),
		)
		return canonicalAllowResult(), nil
	}
	actor := s.resolveCanonicalActor(ctx, payload, authCtx)

	eventType := strings.TrimSpace(payload.Event.Type)
	source := strings.TrimSpace(payload.Source.Adapter)
	sessionID := canonicalSessionID(payload)
	timestamp := canonicalEventTime(payload)

	replayed := conv.PtrValOr(payload.Replayed, false)

	logger := s.logger.With(
		attr.SlogHookSource(source),
		attr.SlogHookEvent(eventType),
		attr.SlogToolName(canonicalToolName(payload)),
		attr.SlogGenAIConversationID(sessionID),
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogProjectID(authCtx.ProjectID.String()),
		attr.SlogHookReplayed(replayed),
	)
	logger.InfoContext(ctx, "unified hook received", attr.SlogEvent("hooks_ingest"))

	if !s.claimHookIdempotency(ctx, conv.PtrValOr(payload.IdempotencyKey, ""), replayed) {
		ctx = withHookDuplicate(ctx)
	}

	blockReason, userReason := s.evaluateCanonicalHook(ctx, payload, authCtx, actor, timestamp)
	if !s.isHookDuplicate(ctx) {
		// Detach from request cancellation: the idempotency token is already
		// claimed, so a client disconnect here would otherwise drop the event
		// for good — the retry gets marked duplicate and skips persistence.
		persistCtx := context.WithoutCancel(ctx)
		s.upsertShadowMCPInventoryURLs(
			persistCtx,
			authCtx.ActiveOrganizationID,
			authCtx.ProjectID.String(),
			canonicalSessionID(payload),
			canonicalMCPInventoryEntries(payload),
		)
		s.recordCanonicalHook(persistCtx, payload, authCtx, actor, timestamp, blockReason)
	}
	// Transcript-derived MCP attribution (Claude Stop/SubagentStop): stash
	// tuples for the scheduled staged-telemetry sweep to join. Runs for
	// duplicate deliveries too — the Redis Set is idempotent, and skipping
	// retries would permanently lose attribution when the first delivery's
	// cache write failed transiently (the retry arrives already marked
	// duplicate).
	s.captureMCPAttribution(context.WithoutCancel(ctx), payload, authCtx)
	if blockReason != "" {
		return canonicalDenyResult(userReason), nil
	}
	return canonicalAllowResult(), nil
}

// canonicalActor is the human the event is attributed to. Distinct from the
// AuthContext identity: an org-wide plugin key authenticates many machines,
// so its owner (the publishing admin) must not absorb every developer's
// telemetry.
type canonicalActor struct {
	UserID string
	Email  string
}

// resolveCanonicalActor picks the attribution identity for one ingested event:
// the payload's self-reported user email when present (matching the legacy
// per-provider paths, which always trusted the sender's user_email), otherwise
// the authenticated token owner. Publish-minted plugin keys are shared by the
// whole org, so their owner — the admin who published the plugin — is never
// used as a fallback: an event from such a key with no self-reported email
// stays unattributed rather than crediting every machine to the publisher.
func (s *Service) resolveCanonicalActor(ctx context.Context, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext) canonicalActor {
	tokenEmail := ""
	if authCtx.Email != nil {
		tokenEmail = strings.TrimSpace(*authCtx.Email)
	}
	selfReported := canonicalSourceUserEmail(payload)
	if selfReported == "" {
		if authCtx.OrgWidePluginHooksKey {
			return s.cachedSessionActor(ctx, payload, authCtx)
		}
		return canonicalActor{UserID: authCtx.UserID, Email: tokenEmail}
	}
	if strings.EqualFold(selfReported, tokenEmail) {
		return canonicalActor{UserID: authCtx.UserID, Email: tokenEmail}
	}
	actor := canonicalActor{
		UserID: s.resolveUserByEmail(ctx, selfReported, authCtx.ActiveOrganizationID),
		Email:  selfReported,
	}
	if actor.UserID == "" {
		// A self-reported email that matches no Gram user cannot key
		// user-scoped policies; recover a complete identity instead of
		// running unattributed. For shared plugin keys the session metadata
		// cache may already link this session to a user (an earlier canonical
		// SessionStart hook, the OTEL path, or the device bridge). A personal
		// key already identifies the developer, so their events keep the owner
		// identity, exactly as when no email is
		// self-reported. Either way policy enforcement and the recorded rows
		// stay on one identity.
		if authCtx.OrgWidePluginHooksKey {
			if cached := s.cachedSessionActor(ctx, payload, authCtx); cached.UserID != "" {
				return cached
			}
		} else if authCtx.UserID != "" {
			return canonicalActor{UserID: authCtx.UserID, Email: tokenEmail}
		}
	}
	return actor
}

// cachedSessionActor recovers attribution for a shared plugin-key event with
// no self-reported email from the session metadata cache (seeded by the OTEL
// path or the device bridge). Resolving here — not just at persistence time in
// canonicalSessionMetadata — keeps policy enforcement and the recorded rows on
// the same identity: user-scoped shadow-MCP policies must see the user the
// session is already attributed to. Only an entry seeded by the same
// org+project is trusted (the cache is keyed by session id alone).
func (s *Service) cachedSessionActor(ctx context.Context, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext) canonicalActor {
	sessionID := canonicalSessionID(payload)
	if sessionID == "" {
		return canonicalActor{UserID: "", Email: ""}
	}
	cached, err := s.getSessionMetadata(ctx, sessionID)
	if err != nil ||
		cached.GramOrgID != authCtx.ActiveOrganizationID ||
		cached.ProjectID != authCtx.ProjectID.String() {
		return canonicalActor{UserID: "", Email: ""}
	}
	return canonicalActor{UserID: cached.UserID, Email: cached.UserEmail}
}

func canonicalSourceUserEmail(payload *gen.IngestPayload) string {
	if payload != nil && payload.Source != nil {
		return strings.TrimSpace(conv.PtrValOr(payload.Source.UserEmail, ""))
	}
	return ""
}

func validateCanonicalIngestPayload(payload *gen.IngestPayload) error {
	if payload == nil || payload.Source == nil || payload.Event == nil {
		return oops.E(oops.CodeInvalid, nil, "source and event are required")
	}
	if strings.TrimSpace(payload.Source.Adapter) == "" {
		return oops.E(oops.CodeInvalid, nil, "source.adapter is required")
	}
	if strings.TrimSpace(payload.Event.Type) == "" {
		return oops.E(oops.CodeInvalid, nil, "event.type is required")
	}
	if strings.TrimSpace(payload.SchemaVersion) != hookIngestSchemaV1 {
		return oops.E(oops.CodeInvalid, nil, "unsupported hook schema_version")
	}
	return nil
}

func (s *Service) evaluateCanonicalHook(ctx context.Context, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext, actor canonicalActor, timestamp time.Time) (string, string) {
	event := canonicalHookEvent(payload, authCtx, actor, timestamp)
	switch strings.TrimSpace(payload.Event.Type) {
	case "prompt.submitted":
		ev := hookevents.NewUserPromptSubmit(event, hookevents.UserPromptSubmitParams{
			Prompt: canonicalPromptText(payload),
		})
		// A warn (challenge) is never blocked here: the canonical ingest
		// transport has no native confirmation primitive, and hard-denying
		// would clobber the ask a dedicated ask-capable hook (Claude
		// PreToolUse) surfaces for the same event. Defer to that transport.
		if scanResult := s.scanUserPromptForEnforcement(ctx, ev); scanResult != nil && scanResult.Action != "warn" {
			auditReason := fmt.Sprintf("Speakeasy blocked this prompt: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			return auditReason, renderUserBlockReason(scanResult.UserMessage, auditReason)
		}
	case "tool.requested":
		toolName := canonicalToolName(payload)
		toolInput := canonicalToolInput(payload)
		if permissionType := canonicalPermissionType(payload); permissionType != "" {
			ev := hookevents.NewPermissionRequest(event, hookevents.PermissionRequestParams{
				ToolName:       toolName,
				ToolInput:      toolInput,
				PermissionType: permissionType,
			})
			if scanResult := s.scanPermissionRequestForEnforcement(ctx, ev); scanResult != nil && scanResult.Action != "warn" {
				auditReason := fmt.Sprintf("Speakeasy blocked this permission request: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
				userReason := renderUserBlockReason(scanResult.UserMessage, auditReason)
				return auditReason, s.appendCanonicalBlockURL(ctx, authCtx, actor, payload, auditReason, toolName, scanResult.PolicyID, userReason)
			}
		}
		if canonicalMCPData(payload) != nil || toolref.IsMCPToolName(toolName) {
			ev := hookevents.NewBeforeMCPExecution(event, hookevents.BeforeMCPExecutionParams{
				ToolName:  toolName,
				ToolInput: toolInput,
			})
			if scanResult := s.scanMCPRequestForEnforcement(ctx, ev); scanResult != nil && scanResult.Action != "warn" {
				auditReason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
				userReason := renderUserBlockReason(scanResult.UserMessage, auditReason)
				return auditReason, s.appendCanonicalBlockURL(ctx, authCtx, actor, payload, auditReason, toolName, scanResult.PolicyID, userReason)
			}
			return s.evaluateCanonicalShadowMCP(ctx, authCtx, actor, payload, toolName, toolInput)
		}
		ev := hookevents.NewBeforeToolUse(event, hookevents.BeforeToolUseParams{
			ToolName:  toolName,
			ToolInput: toolInput,
		})
		// warn defers to the ask-capable transport (see prompt.submitted note).
		if scanResult := s.scanToolRequestForEnforcement(ctx, ev); scanResult != nil && scanResult.Action != "warn" {
			auditReason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			userReason := renderUserBlockReason(scanResult.UserMessage, auditReason)
			return auditReason, s.appendCanonicalBlockURL(ctx, authCtx, actor, payload, auditReason, toolName, scanResult.PolicyID, userReason)
		}
	}
	return "", ""
}

// appendCanonicalBlockURL mints the durable block row for a policy-denied
// tool call and attaches its URL to the agent-facing reason, matching the
// legacy per-provider handlers. Retried deliveries keep the deny but must not
// mint a second row.
func (s *Service) appendCanonicalBlockURL(ctx context.Context, authCtx *contextvalues.AuthContext, actor canonicalActor, payload *gen.IngestPayload, auditReason, toolName, policyID, userReason string) string {
	if s.isHookDuplicate(ctx) {
		return userReason
	}
	bURL := s.recordToolCallBlockAsync(ctx, toolCallBlockParams{
		Provider:       strings.TrimSpace(payload.Source.Adapter),
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Reason:         auditReason,
		ToolName:       toolName,
		UserID:         actor.UserID,
		RiskPolicyID:   conv.StringToNullUUID(policyID),
		RiskResultID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ChatID:         chatIDForBlock(canonicalSessionID(payload)),
		ChatMessageID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	if bURL == "" {
		return userReason
	}
	return appendBlockURL(userReason, bURL)
}

func canonicalHookEvent(payload *gen.IngestPayload, authCtx *contextvalues.AuthContext, actor canonicalActor, timestamp time.Time) hookevents.Event {
	rawEvent := strings.TrimSpace(conv.PtrValOr(payload.Source.RawEventName, ""))
	if rawEvent == "" {
		rawEvent = strings.TrimSpace(payload.Event.Type)
	}
	return hookevents.Event{
		Provider:     hookevents.Provider(strings.TrimSpace(payload.Source.Adapter)),
		Type:         canonicalRiskEventType(payload),
		RawEventType: rawEvent,
		Timestamp:    timestamp,
		AuthContext:  authCtx,
		Context: hookevents.EventContext{
			OrganizationID: authCtx.ActiveOrganizationID,
			ProjectID:      *authCtx.ProjectID,
			User: hookevents.User{
				ID:    actor.UserID,
				Email: actor.Email,
			},
		},
		ConversationID: canonicalSessionID(payload),
		Raw:            payload,
	}
}

func canonicalRiskEventType(payload *gen.IngestPayload) hookevents.EventType {
	switch strings.TrimSpace(payload.Event.Type) {
	case "prompt.submitted":
		return hookevents.EventTypeUserPromptSubmit
	case "tool.requested":
		if canonicalMCPData(payload) != nil || toolref.IsMCPToolName(canonicalToolName(payload)) {
			return hookevents.EventTypeBeforeMCPExecution
		}
		if canonicalPermissionType(payload) != "" {
			return hookevents.EventTypePermissionRequest
		}
		return hookevents.EventTypeBeforeToolUse
	case "tool.completed":
		if canonicalMCPData(payload) != nil {
			return hookevents.EventTypeAfterMCPExecution
		}
		return hookevents.EventTypeAfterToolUse
	case "tool.failed":
		return hookevents.EventTypeAfterToolUseFailure
	case "assistant.responded":
		return hookevents.EventTypeAfterAgentResponse
	case "assistant.thought":
		return hookevents.EventTypeAfterAgentThought
	case "session.started":
		return hookevents.EventTypeSessionStart
	case "session.updated":
		return hookevents.EventTypeConfigChange
	case "session.ended":
		return hookevents.EventTypeSessionEnd
	case "notification.reported":
		return hookevents.EventTypeNotification
	default:
		return hookevents.EventType(strings.TrimSpace(payload.Event.Type))
	}
}

func (s *Service) evaluateCanonicalShadowMCP(ctx context.Context, authCtx *contextvalues.AuthContext, actor canonicalActor, payload *gen.IngestPayload, rawToolName string, toolInput any) (string, string) {
	policy := s.lookupShadowMCPBlockingPolicy(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), actor.UserID)
	if policy == nil {
		return "", ""
	}

	toolName := toolref.MCPFunctionOf(rawToolName)
	evidence := canonicalShadowMCPEvidence(payload, rawToolName)
	if detail, denied := s.enforceShadowMCPToolAccess(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), actor.UserID, policy.ID, toolName, evidence); denied {
		auditReason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", policy.Name, detail)
		userReason := s.renderShadowMCPUserBlockReason(ctx, shadowMCPRequestLinkParams{
			OrganizationID:  authCtx.ActiveOrganizationID,
			ProjectID:       authCtx.ProjectID.String(),
			RequesterUserID: actor.UserID,
			UserMessage:     policy.UserMessage,
			AuditReason:     auditReason,
			Evidence:        evidence,
			ToolName:        toolName,
			ToolInput:       toolInput,
			RiskPolicyID:    policy.ID,
		})
		// Retried deliveries still get the deny decision, but must not mint
		// another block row (and a second block URL) for the same call.
		if !s.isHookDuplicate(ctx) {
			if bURL := s.recordToolCallBlockAsync(ctx, toolCallBlockParams{
				Provider:       strings.TrimSpace(payload.Source.Adapter),
				OrganizationID: authCtx.ActiveOrganizationID,
				ProjectID:      *authCtx.ProjectID,
				Reason:         auditReason,
				ToolName:       toolName,
				UserID:         actor.UserID,
				RiskPolicyID:   conv.StringToNullUUID(policy.ID),
				RiskResultID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				ChatID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				ChatMessageID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			}); bURL != "" {
				userReason = appendBlockURL(userReason, bURL)
			}
		}
		return auditReason, userReason
	}
	return "", ""
}

func canonicalShadowMCPEvidence(payload *gen.IngestPayload, rawToolName string) shadowmcp.AccessEvidence {
	mcp := canonicalMCPData(payload)
	if mcp == nil {
		return shadowmcp.AccessEvidence{
			FullURL:        "",
			URLHost:        "",
			ServerIdentity: toolref.MCPServerOf(rawToolName),
		}
	}
	identity := strings.TrimSpace(conv.PtrValOr(mcp.ServerIdentity, ""))
	if identity == "" {
		identity = strings.TrimSpace(conv.PtrValOr(mcp.Command, ""))
	}
	if identity == "" {
		identity = strings.TrimSpace(conv.PtrValOr(mcp.ServerName, ""))
	}
	if identity == "" {
		identity = toolref.MCPServerOf(rawToolName)
	}
	return shadowmcp.AccessEvidence{
		FullURL:        strings.TrimSpace(conv.PtrValOr(mcp.URL, "")),
		URLHost:        "",
		ServerIdentity: identity,
	}
}

func (s *Service) recordCanonicalHook(ctx context.Context, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext, actor canonicalActor, timestamp time.Time, blockReason string) {
	// Resolve the session identity once, before the telemetry write, so the
	// hook row and the chat persistence below stamp the same AI-account
	// attribution.
	metadata := s.canonicalSessionMetadata(ctx, payload, authCtx, actor)
	if strings.TrimSpace(payload.Event.Type) == "session.started" &&
		metadata.SessionID != "" && (metadata.UserID != "" || metadata.UserEmail != "") {
		cacheCtx, cancel := context.WithTimeout(ctx, canonicalSessionCacheWriteTimeout)
		err := s.cache.Set(cacheCtx, sessionCacheKey(metadata.SessionID), metadata, 24*time.Hour)
		cancel()
		if err != nil {
			s.logger.WarnContext(ctx, "failed to cache canonical hook session identity",
				attr.SlogEvent("hooks_ingest_session_cache_failed"),
				attr.SlogError(err),
				attr.SlogGenAIConversationID(metadata.SessionID),
				attr.SlogOrganizationID(metadata.GramOrgID),
				attr.SlogProjectID(metadata.ProjectID),
			)
		}
	}
	s.writeCanonicalTelemetry(ctx, payload, authCtx, &metadata, timestamp, blockReason)
	if err := s.persistCanonicalConversationEvent(ctx, payload, authCtx, &metadata); err != nil {
		s.logger.WarnContext(ctx, "failed to persist canonical hook conversation event",
			attr.SlogEvent("hooks_ingest_chat_persist_failed"),
			attr.SlogError(err),
			attr.SlogHookSource(payload.Source.Adapter),
			attr.SlogHookEvent(payload.Event.Type),
			attr.SlogGenAIConversationID(canonicalSessionID(payload)),
			attr.SlogProjectID(authCtx.ProjectID.String()),
		)
	}
}

// canonicalSessionMetadata builds the session identity for a canonical hook
// event: the resolved actor (self-reported user email when the payload
// carries one, else the token owner), enriched with the AI-account
// attribution the OTEL path cached for the session (user_accounts link,
// account_type, provider identity, device-bridge owner). Canonical payloads
// carry no account identity of their own, so without the cached attribution
// telemetry rows and chats captured here reflect only the resolved actor —
// invisible to the account-identity risk rules and the personal/team
// classification. The AI account's own email rides separately in
// ObservedUserEmail (the gram.account_email attribute). The session cache is
// keyed by session id alone, so only trust an entry the same org+project
// seeded.
func (s *Service) canonicalSessionMetadata(ctx context.Context, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext, actor canonicalActor) SessionMetadata {
	metadata := SessionMetadata{
		SessionID:           canonicalSessionID(payload),
		ServiceName:         strings.TrimSpace(payload.Source.Adapter),
		UserEmail:           actor.Email,
		UserID:              actor.UserID,
		Provider:            "",
		ExternalOrgID:       "",
		ExternalAccountUUID: "",
		ExternalAccountID:   "",
		DeviceID:            "",
		AccountType:         "",
		BillingMode:         "",
		UserAccountID:       "",
		ObservedUserEmail:   "",
		GramOrgID:           authCtx.ActiveOrganizationID,
		ProjectID:           authCtx.ProjectID.String(),
	}
	if metadata.SessionID == "" {
		return metadata
	}

	if cached, err := s.getSessionMetadata(ctx, metadata.SessionID); err == nil &&
		cached.GramOrgID == metadata.GramOrgID && cached.ProjectID == metadata.ProjectID {
		metadata.Provider = cached.Provider
		metadata.ExternalOrgID = cached.ExternalOrgID
		metadata.ExternalAccountUUID = cached.ExternalAccountUUID
		metadata.ExternalAccountID = cached.ExternalAccountID
		metadata.DeviceID = cached.DeviceID
		metadata.AccountType = cached.AccountType
		metadata.BillingMode = cached.BillingMode
		metadata.UserAccountID = cached.UserAccountID
		// The OTEL path's UserEmail is the account's own report; fall back to it
		// for cache entries written before ObservedUserEmail existed.
		metadata.ObservedUserEmail = conv.Default(cached.ObservedUserEmail, cached.UserEmail)
		// Fill identity only when the resolved actor carried none (org-scoped
		// ingest keys with no self-reported email): the device bridge may have
		// attributed the owning employee. A resolved identity is never
		// overwritten.
		if metadata.UserEmail == "" {
			metadata.UserEmail = cached.UserEmail
		}
		if metadata.UserID == "" {
			metadata.UserID = cached.UserID
		}
	}
	return metadata
}

func (s *Service) writeCanonicalTelemetry(ctx context.Context, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext, metadata *SessionMetadata, timestamp time.Time, blockReason string) {
	if s.telemetryLogger == nil {
		return
	}

	hookEventName := telemetryHookEventName(payload)
	toolName := canonicalTelemetryToolName(payload)
	if toolName == "" {
		toolName = hookEventName
	}

	attrs := hookTelemetryBaseAttrs(payload, authCtx, hookEventName)
	if blockReason != "" {
		attrs[attr.HookBlockReasonKey] = blockReason
	}
	if toolName != "" {
		attrs[attr.ToolNameKey] = toolName
	}
	if toolCallID := canonicalToolCallID(payload); toolCallID != "" {
		attrs[attr.GenAIToolCallIDKey] = toolCallID
	}
	if input := canonicalToolInput(payload); input != nil {
		attrs[attr.GenAIToolCallArgumentsKey] = jsonString(input)
	}
	if output := canonicalToolOutput(payload); output != nil {
		attrs[attr.GenAIToolCallResultKey] = jsonString(output)
	}
	if errPayload := canonicalToolError(payload); errPayload != nil {
		attrs[attr.HookErrorKey] = jsonString(errPayload)
	}
	if isInterrupt := canonicalIsInterrupt(payload); isInterrupt != nil {
		attrs[attr.HookIsInterruptKey] = *isInterrupt
	}
	if tool := canonicalToolCallData(payload); tool != nil && tool.DurationMs != nil {
		attrs[attr.ToolCallDurationKey] = time.Duration(*tool.DurationMs * float64(time.Millisecond)).Seconds()
	}
	if usage := canonicalUsageData(payload); usage != nil {
		if usage.InputTokens != nil {
			attrs[attr.GenAIUsageInputTokensKey] = *usage.InputTokens
		}
		if usage.OutputTokens != nil {
			attrs[attr.GenAIUsageOutputTokensKey] = *usage.OutputTokens
		}
		if usage.CacheReadTokens != nil {
			attrs[attr.GenAIUsageCacheReadInputTokensKey] = *usage.CacheReadTokens
		}
		if usage.CacheWriteTokens != nil {
			attrs[attr.GenAIUsageCacheCreationInputTokensKey] = *usage.CacheWriteTokens
		}
		if usage.Cost != nil {
			attrs[attr.GenAIUsageCostKey] = *usage.Cost
		}
	}
	if mcp := canonicalMCPData(payload); mcp != nil {
		if server := strings.TrimSpace(conv.PtrValOr(mcp.ServerIdentity, "")); server != "" {
			attrs[attr.ToolCallSourceKey] = server
		} else if server := strings.TrimSpace(conv.PtrValOr(mcp.ServerName, "")); server != "" {
			attrs[attr.ToolCallSourceKey] = server
		}
		if url := strings.TrimSpace(conv.PtrValOr(mcp.URL, "")); url != "" {
			attrs[attr.MCPServerURLKey] = url
			attrs[attr.MCPMatchKey] = url
		} else if command := strings.TrimSpace(conv.PtrValOr(mcp.Command, "")); command != "" {
			attrs[attr.MCPMatchKey] = command
		}
	}
	skill := canonicalSkillName(payload)
	if skill != "" && isExplicitSkillActivation(payload) {
		attrs[attr.GenAIToolCallArgumentsKey] = jsonString(map[string]string{"skill": skill})
	}

	// Carry the account attribution (provider, external_org_id, account_type,
	// device_id) onto every hook event row so per-tool-call telemetry can be
	// split by personal vs team account, matching the legacy per-provider paths.
	stampAccountAttribution(attrs, *metadata)

	s.logHookTelemetry(ctx, authCtx, metadata, timestamp, toolName, attrs)

	// A skill name on an ordinary tool/prompt event is an inferred activation
	// (Codex has no dedicated Skill tool): the underlying event was recorded
	// truthfully above, and the activation gets its own derived row so skill
	// dashboards see the same skill.activated vocabulary as Claude senders.
	// A policy-blocked event never ran, so it is not an activation.
	if skill != "" && !isExplicitSkillActivation(payload) && blockReason == "" {
		attrs = hookTelemetryBaseAttrs(payload, authCtx, eventTypeSkillActivated)
		// Skill counts aggregate at trace level (trace_summaries), and its MV
		// resolves tool_name/skill_name with any(): sharing a trace with the
		// underlying tool or prompt rows lets a non-Skill sibling win the
		// summary and drop the activation from skill analytics — and the
		// session-hash fallback would additionally collapse every
		// prompt-mention activation in a session into one summary row. Every
		// derived row gets its own trace.
		attrs[attr.TraceIDKey] = generateTraceID()
		attrs[attr.ToolNameKey] = "Skill"
		attrs[attr.GenAIToolCallArgumentsKey] = jsonString(map[string]string{"skill": skill})
		stampAccountAttribution(attrs, *metadata)
		s.logHookTelemetry(ctx, authCtx, metadata, timestamp, "Skill", attrs)
	}
}

// hookTelemetryBaseAttrs builds the attributes shared by every telemetry row
// derived from one ingested hook event. Each row gets its own span id; the
// trace id is payload-derived so sibling rows stay on one trace.
func hookTelemetryBaseAttrs(payload *gen.IngestPayload, authCtx *contextvalues.AuthContext, hookEventName string) map[attr.Key]any {
	attrs := map[attr.Key]any{
		attr.EventSourceKey:    string(telemetry.EventSourceHook),
		attr.HookEventKey:      hookEventName,
		attr.HookSourceKey:     strings.TrimSpace(payload.Source.Adapter),
		attr.ProjectIDKey:      authCtx.ProjectID.String(),
		attr.OrganizationIDKey: authCtx.ActiveOrganizationID,
		attr.SpanIDKey:         generateSpanID(),
		attr.TraceIDKey:        canonicalTraceID(payload),
		attr.LogBodyKey:        "Hook: " + hookEventName,
	}
	if sessionID := canonicalSessionID(payload); sessionID != "" {
		attrs[attr.GenAIConversationIDKey] = sessionID
	}
	if conv.PtrValOr(payload.Replayed, false) {
		// Downtime backlog redelivered from a device's offline spool: the
		// row's timestamp is the original occurred_at when the envelope
		// carried one (arrival time otherwise), so without this marker
		// replays would be indistinguishable from live traffic in
		// time-bucketed consumers.
		attrs[attr.HookReplayedKey] = true
	}
	if hostname := strings.TrimSpace(conv.PtrValOr(payload.Source.Hostname, "")); hostname != "" {
		attrs[attr.HookHostnameKey] = hostname
	}
	if model := canonicalModel(payload); model != "" {
		attrs[attr.GenAIResponseModelKey] = model
	}
	return attrs
}

func (s *Service) logHookTelemetry(ctx context.Context, authCtx *contextvalues.AuthContext, metadata *SessionMetadata, timestamp time.Time, toolName string, attrs map[attr.Key]any) {
	s.telemetryLogger.Log(ctx, telemetry.LogParams{
		Timestamp: timestamp,
		ToolInfo: telemetry.ToolInfo{
			Name:           toolName,
			OrganizationID: authCtx.ActiveOrganizationID,
			ProjectID:      authCtx.ProjectID.String(),
			ID:             "",
			URN:            "",
			DeploymentID:   "",
			FunctionID:     nil,
		},
		UserInfo:   telemetry.UserInfoByIDAndEmail(metadata.UserID, metadata.UserEmail),
		Attributes: attrs,
	})
}

// telemetryHookEventName resolves the value stored in the gram.hook.event
// telemetry attribute. That attribute's vocabulary is the provider-style
// HookEvent names: the per-platform ingest endpoints have always written them,
// and the ClickHouse consumers (session summaries, tool-call success/failure
// counts, the is_completed_tool_call predicate) match on them. Canonical
// events are therefore translated back via the adapter's raw event name, with
// a fixed canonical fallback for senders that omit one, so unified-ingest rows
// keep counting without a ClickHouse migration.
func telemetryHookEventName(payload *gen.IngestPayload) string {
	// Skill activations are a Gram-specific classification layered onto an
	// ordinary provider tool event; resolving via the raw name would erase it.
	if isExplicitSkillActivation(payload) {
		return eventTypeSkillActivated
	}
	raw := strings.TrimSpace(conv.PtrValOr(payload.Source.RawEventName, ""))
	if raw != "" {
		var parse func(string) (HookEvent, bool)
		switch strings.TrimSpace(payload.Source.Adapter) {
		case "claude":
			parse = parseClaudeHookEvent
		case "cursor":
			parse = parseCursorHookEvent
		case "codex":
			parse = parseCodexHookEvent
		}
		if parse != nil {
			if event, ok := parse(raw); ok {
				return string(event)
			}
		}
	}
	switch eventType := strings.TrimSpace(payload.Event.Type); eventType {
	case "session.started":
		return string(HookEventSessionStart)
	case "session.ended":
		return string(HookEventSessionEnd)
	case "prompt.submitted":
		return string(HookEventUserPromptSubmit)
	case "tool.requested":
		return string(HookEventPreToolUse)
	case "tool.completed":
		return string(HookEventPostToolUse)
	case "tool.failed":
		return string(HookEventPostToolUseFailure)
	case "assistant.responded":
		return string(HookEventAfterAgentResponse)
	case "assistant.thought":
		return string(HookEventAfterAgentThought)
	case "notification.reported":
		return string(HookEventNotification)
	default:
		// session.updated, usage.reported, skill.activated and any future
		// canonical types have no provider-style equivalent; store them as-is.
		return eventType
	}
}

func (s *Service) persistCanonicalConversationEvent(ctx context.Context, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext, metadata *SessionMetadata) error {
	sessionID := canonicalSessionID(payload)
	if sessionID == "" || authCtx.ProjectID == nil {
		return nil
	}
	baseMsg := func(role, content string) chatRepo.CreateChatMessageParams {
		return chatRepo.CreateChatMessageParams{
			ChatID:           sessionIDToUUID(sessionID),
			ProjectID:        *authCtx.ProjectID,
			Role:             role,
			Content:          content,
			ContentRaw:       nil,
			ContentAssetUrl:  conv.ToPGTextEmpty(""),
			StorageError:     conv.ToPGTextEmpty(""),
			Model:            conv.ToPGTextEmpty(canonicalModel(payload)),
			MessageID:        conv.ToPGTextEmpty(""),
			ToolCallID:       conv.ToPGTextEmpty(""),
			UserID:           conv.ToPGTextEmpty(metadata.UserID),
			ExternalUserID:   conv.ToPGTextEmpty(metadata.UserEmail),
			FinishReason:     conv.ToPGTextEmpty(""),
			ToolCalls:        nil,
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
			Origin:           conv.ToPGTextEmpty(""),
			UserAgent:        conv.ToPGTextEmpty(""),
			IpAddress:        conv.ToPGTextEmpty(""),
			Source:           conv.ToPGTextEmpty(strings.TrimSpace(payload.Source.Adapter)),
			ContentHash:      nil,
			Generation:       0,
		}
	}

	var msg chatRepo.CreateChatMessageParams
	var titleContent string
	switch strings.TrimSpace(payload.Event.Type) {
	case "prompt.submitted":
		content := canonicalPromptText(payload)
		if strings.TrimSpace(content) == "" {
			return nil
		}
		msg = baseMsg("user", content)
		titleContent = content
	case "assistant.responded":
		content := canonicalMessageText(payload)
		if strings.TrimSpace(content) == "" {
			return nil
		}
		msg = baseMsg("assistant", content)
		titleContent = content
	case "tool.requested":
		// Permission prompts (codex PermissionRequest) also normalize to
		// tool.requested but are only pre-approval previews: they may be
		// denied or followed by the real request, so persisting them would
		// put phantom or duplicate tool_calls rows in the transcript.
		if canonicalPermissionType(payload) != "" ||
			strings.EqualFold(strings.TrimSpace(conv.PtrValOr(payload.Source.RawEventName, "")), "PermissionRequest") {
			return nil
		}
		toolName := canonicalToolName(payload)
		if strings.TrimSpace(toolName) == "" {
			return nil
		}
		toolCallsJSON, err := canonicalToolCallsJSON(payload)
		if err != nil {
			return err
		}
		msg = baseMsg("assistant", "")
		msg.FinishReason = conv.ToPGText("tool_calls")
		msg.ToolCalls = toolCallsJSON
		titleContent = toolName
	case "tool.completed", "tool.failed":
		content := canonicalToolResultContent(payload)
		if strings.TrimSpace(content) == "" {
			return nil
		}
		msg = baseMsg("tool", content)
		msg.ToolCallID = conv.ToPGTextEmpty(canonicalChatToolCallID(payload))
		titleContent = content
	default:
		return nil
	}

	return s.insertMessageWithFallbackUpsert(ctx, metadata, msg.ChatID, *authCtx.ProjectID, msg, canonicalChatTitle(payload, titleContent))
}

func canonicalToolCallsJSON(payload *gen.IngestPayload) ([]byte, error) {
	toolCalls := []map[string]any{{
		"id":   canonicalChatToolCallID(payload),
		"type": "function",
		"function": map[string]any{
			"name":      canonicalToolName(payload),
			"arguments": marshalToJSON(canonicalToolInput(payload)),
		},
	}}
	toolCallsJSON, err := json.Marshal(toolCalls)
	if err != nil {
		return nil, fmt.Errorf("marshal canonical tool_calls: %w", err)
	}
	return toolCallsJSON, nil
}

func canonicalChatToolCallID(payload *gen.IngestPayload) string {
	if id := canonicalToolCallID(payload); id != "" {
		return id
	}
	if name := canonicalToolName(payload); name != "" {
		return name
	}
	return canonicalTraceID(payload)
}

func canonicalToolResultContent(payload *gen.IngestPayload) string {
	if strings.TrimSpace(payload.Event.Type) == "tool.failed" {
		return marshalToJSON(canonicalToolError(payload))
	}
	if mcp := canonicalMCPData(payload); mcp != nil && mcp.ResultJSON != nil {
		return strings.TrimSpace(*mcp.ResultJSON)
	}
	return marshalToJSON(canonicalToolOutput(payload))
}

func canonicalAllowResult() *gen.IngestHookResult {
	return &gen.IngestHookResult{
		Decision: "allow",
		Reason:   nil,
		Message:  nil,
		Effects:  nil,
	}
}

func canonicalDenyResult(message string) *gen.IngestHookResult {
	if strings.TrimSpace(message) == "" {
		message = "Request denied by Speakeasy policy."
	}
	reason := "policy_denied"
	return &gen.IngestHookResult{
		Decision: "deny",
		Reason:   &reason,
		Message:  &message,
		Effects:  nil,
	}
}

func canonicalEventTime(payload *gen.IngestPayload) time.Time {
	if payload != nil && payload.Event != nil {
		if raw := strings.TrimSpace(conv.PtrValOr(payload.Event.OccurredAt, "")); raw != "" {
			if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
				return t
			}
		}
	}
	return time.Now()
}

func canonicalSessionID(payload *gen.IngestPayload) string {
	if payload != nil && payload.Session != nil {
		return strings.TrimSpace(conv.PtrValOr(payload.Session.ID, ""))
	}
	return ""
}

func canonicalModel(payload *gen.IngestPayload) string {
	if payload != nil && payload.Session != nil {
		return strings.TrimSpace(conv.PtrValOr(payload.Session.Model, ""))
	}
	return ""
}

func canonicalToolName(payload *gen.IngestPayload) string {
	if tool := canonicalToolCallData(payload); tool != nil {
		return strings.TrimSpace(conv.PtrValOr(tool.Name, ""))
	}
	return ""
}

func canonicalTelemetryToolName(payload *gen.IngestPayload) string {
	// Only explicit skill.activated events are relabeled: an inferred skill on
	// an ordinary tool/prompt event keeps the event's own tool identity and
	// gets a separate derived skill row instead.
	if skill := canonicalSkillName(payload); skill != "" && isExplicitSkillActivation(payload) {
		return "Skill"
	}
	return canonicalToolName(payload)
}

func canonicalToolCallID(payload *gen.IngestPayload) string {
	if tool := canonicalToolCallData(payload); tool != nil {
		return strings.TrimSpace(conv.PtrValOr(tool.ID, ""))
	}
	return ""
}

func canonicalTraceID(payload *gen.IngestPayload) string {
	if id := canonicalToolCallID(payload); id != "" {
		return hashToolCallIDToTraceID(id)
	}
	if sessionID := canonicalSessionID(payload); sessionID != "" {
		return hashToolCallIDToTraceID(sessionID)
	}
	return generateTraceID()
}

func canonicalToolInput(payload *gen.IngestPayload) any {
	if tool := canonicalToolCallData(payload); tool != nil {
		return tool.Input
	}
	return nil
}

func canonicalToolOutput(payload *gen.IngestPayload) any {
	if tool := canonicalToolCallData(payload); tool != nil && tool.Output != nil {
		return tool.Output
	}
	if mcp := canonicalMCPData(payload); mcp != nil && mcp.ResultJSON != nil {
		return *mcp.ResultJSON
	}
	return nil
}

func canonicalToolError(payload *gen.IngestPayload) any {
	if tool := canonicalToolCallData(payload); tool != nil {
		return tool.Error
	}
	return nil
}

func canonicalIsInterrupt(payload *gen.IngestPayload) *bool {
	if tool := canonicalToolCallData(payload); tool != nil {
		return tool.IsInterrupt
	}
	return nil
}

func canonicalPermissionType(payload *gen.IngestPayload) string {
	if tool := canonicalToolCallData(payload); tool != nil {
		return strings.TrimSpace(conv.PtrValOr(tool.PermissionType, ""))
	}
	return ""
}

func canonicalPromptText(payload *gen.IngestPayload) string {
	if payload != nil && payload.Data != nil && payload.Data.Prompt != nil {
		return strings.TrimSpace(conv.PtrValOr(payload.Data.Prompt.Text, ""))
	}
	return ""
}

func canonicalMessageText(payload *gen.IngestPayload) string {
	if payload != nil && payload.Data != nil && payload.Data.Message != nil {
		return strings.TrimSpace(conv.PtrValOr(payload.Data.Message.Text, ""))
	}
	return ""
}

func canonicalSkillName(payload *gen.IngestPayload) string {
	if payload != nil && payload.Data != nil && payload.Data.Skill != nil {
		return strings.TrimSpace(payload.Data.Skill.Name)
	}
	return ""
}

func canonicalChatTitle(payload *gen.IngestPayload, fallback string) string {
	title := canonicalPromptText(payload)
	if title == "" {
		title = fallback
	}
	title = strings.TrimSpace(title)
	runes := []rune(title)
	if len(runes) <= 80 {
		return title
	}
	return string(runes[:80])
}

func canonicalToolCallData(payload *gen.IngestPayload) *gen.HookToolCallData {
	if payload != nil && payload.Data != nil {
		return payload.Data.ToolCall
	}
	return nil
}

func canonicalMCPData(payload *gen.IngestPayload) *gen.HookMCPData {
	if payload != nil && payload.Data != nil {
		return payload.Data.Mcp
	}
	return nil
}

func canonicalMCPInventoryEntries(payload *gen.IngestPayload) []MCPServerEntry {
	if payload == nil || payload.Data == nil || len(payload.Data.McpInventory) == 0 {
		return nil
	}
	entries := make([]MCPServerEntry, 0, len(payload.Data.McpInventory))
	for _, mcp := range payload.Data.McpInventory {
		if mcp == nil {
			continue
		}
		entries = append(entries, MCPServerEntry{
			RawLine:       "",
			Source:        strings.TrimSpace(payload.Source.Adapter),
			PluginName:    "",
			Name:          strings.TrimSpace(conv.PtrValOr(mcp.ServerName, "")),
			URL:           strings.TrimSpace(conv.PtrValOr(mcp.URL, "")),
			Command:       strings.TrimSpace(conv.PtrValOr(mcp.Command, "")),
			Transport:     "",
			Status:        "unknown",
			StatusRaw:     "",
			ConnectorUUID: "",
			ToolPrefix:    "",
		})
	}
	return entries
}

func canonicalUsageData(payload *gen.IngestPayload) *gen.HookUsageData {
	if payload != nil && payload.Data != nil {
		return payload.Data.Usage
	}
	return nil
}

func jsonString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	case json.RawMessage:
		return string(t)
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprint(t)
		}
		return string(b)
	}
}
