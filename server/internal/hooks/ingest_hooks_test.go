package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/cache"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskRepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// ingestUserScopedShadowMCPScanner reports a blocking shadow-MCP policy for a
// single user. Unlike userScopedShadowMCPScanner it returns a policy without
// an ID: these tests read the persisted tool_call_blocks row back, and a
// made-up policy UUID would fail the row's risk_policies reference.
type ingestUserScopedShadowMCPScanner struct {
	userID string
}

type sessionCacheDeadlineRecorder struct {
	cache.Cache
	remaining chan time.Duration
}

func (r *sessionCacheDeadlineRecorder) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	deadline, ok := ctx.Deadline()
	if ok {
		r.remaining <- time.Until(deadline)
	} else {
		r.remaining <- 0
	}
	if err := r.Cache.Set(ctx, key, value, ttl); err != nil {
		return fmt.Errorf("set cache: %w", err)
	}
	return nil
}

func (s ingestUserScopedShadowMCPScanner) ScanForEnforcement(_ context.Context, _ string, _ uuid.UUID, _ string, _ string, _ string, _ string) (*risk.ScanResult, error) {
	return nil, nil
}

func (s ingestUserScopedShadowMCPScanner) LookupShadowMCPBlockingPolicy(_ context.Context, _ string, _ uuid.UUID, userID string) (*risk.ShadowMCPPolicy, error) {
	if userID != s.userID {
		return nil, nil
	}
	return &risk.ShadowMCPPolicy{Name: "shadow-mcp-block"}, nil
}

func (s ingestUserScopedShadowMCPScanner) HasEnabledShadowMCPPolicy(_ context.Context, _ uuid.UUID) (bool, error) {
	return true, nil
}

func (s ingestUserScopedShadowMCPScanner) HasAcknowledgedChallenge(_ context.Context, _ uuid.UUID, _, _, _, _ string) bool {
	return false
}

func (s ingestUserScopedShadowMCPScanner) RecordPolicyChallenge(_ context.Context, _ string, _ uuid.UUID, _, _, _, _, _, _, _ string) {
}

func requireBlockIDFromMessage(t *testing.T, message string) uuid.UUID {
	t.Helper()
	const marker = "/blocks/"
	index := strings.LastIndex(message, marker)
	require.NotEqual(t, -1, index, "block message must include %q", marker)
	fields := strings.Fields(message[index+len(marker):])
	require.NotEmpty(t, fields, "block message must include an id after %q", marker)
	blockID, err := uuid.Parse(fields[0])
	require.NoError(t, err)
	return blockID
}

func TestIngest_AcceptsCustomHookSource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	sessionID := "custom-ingest-source"

	result, err := ti.service.Ingest(ctx, canonicalIngestPayload("openclaw", "session.started", sessionID))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "allow", result.Decision)
}

func TestIngest_RequiresCurrentSchemaVersion(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	payload := canonicalIngestPayload("openclaw", "session.started", "bad-schema")
	payload.SchemaVersion = "hook.ingest.v0"

	result, err := ti.service.Ingest(ctx, payload)
	require.Error(t, err)
	require.Nil(t, result)
	require.Contains(t, err.Error(), "unsupported hook schema_version")
}

// A keyless request on the optional-auth ingest endpoint is acknowledged
// without processing: hook senders must stay non-blocking for machines that
// never signed in, and without credentials there is no org to attribute the
// event to. Even a shadow-MCP-shaped tool request comes back "allow".
func TestIngest_NoCredentialsFailsOpen(t *testing.T) {
	t.Parallel()

	_, ti := newTestHooksService(t)

	toolName := "mcp__local_server__search"
	toolCallID := "call-keyless"
	serverIdentity := "local-server"
	payload := canonicalIngestPayload("claude", "tool.requested", "keyless-session")
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolCallID,
			Name:  &toolName,
			Input: map[string]any{"query": "secret"},
		},
		Mcp: &gen.HookMCPData{
			ServerIdentity: &serverIdentity,
		},
	}

	result, err := ti.service.Ingest(t.Context(), payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "allow", result.Decision)
}

// A request that presents an API key must have it validated: a rejected key
// is a hard 401 so the sender's credential-recovery path (org-key retry,
// established-machine fail-closed ratchet) can react, instead of the event
// being silently accepted or dropped.
func TestIngest_RejectedCredentialsUnauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestHooksService(t)

	badKey := "gram_key_expired_or_invalid"
	slug := "default"
	payload := canonicalIngestPayload("claude", "session.started", "bad-key-session")
	payload.ApikeyToken = &badKey
	payload.ProjectSlugInput = &slug

	result, err := ti.service.Ingest(t.Context(), payload)
	require.Error(t, err)
	require.Nil(t, result)
	require.Contains(t, strings.ToLower(err.Error()), "unauthorized")
}

// A shared plugins-* key carries no usable identity of its own, but the
// session may already be attributed through the OTEL/device-bridge metadata
// cache. User-scoped shadow-MCP policies must see that cached identity during
// enforcement — not only at persistence time — or per-user blocking silently
// skips every event the shared key sends without a self-reported email.
func TestIngest_ShadowMCPPolicyUsesCachedSessionIdentityForSharedKey(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	authCtx.APIKeyName = "plugins-hooks-20260708-120102-abc123"
	authCtx.OrgWidePluginHooksKey = true

	cachedUserID := "user_cached_owner"
	sessionID := "canonical-shadow-mcp-cached-identity"
	require.NoError(t, ti.service.cache.Set(ctx, sessionCacheKey(sessionID), SessionMetadata{
		SessionID: sessionID,
		UserID:    cachedUserID,
		UserEmail: "cached-dev@example.com",
		GramOrgID: authCtx.ActiveOrganizationID,
		ProjectID: authCtx.ProjectID.String(),
	}, 0))

	// The policy only exists for the cached user: a deny proves enforcement
	// resolved the actor from the session cache rather than running
	// unattributed.
	ti.service.riskScanner = ingestUserScopedShadowMCPScanner{userID: cachedUserID}

	toolName := "mcp__local_server__search"
	serverIdentity := "local-server"
	toolCallID := "call-cached-identity"
	payload := canonicalIngestPayload("claude", "tool.requested", sessionID)
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolCallID,
			Name:  &toolName,
			Input: map[string]any{"query": "secret"},
		},
		Mcp: &gen.HookMCPData{
			ServerIdentity: &serverIdentity,
		},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "deny", result.Decision)
}

// A shared-key event may self-report an email that matches no Gram user (a
// personal or provider-account address). That claim cannot key user-scoped
// policies, so enforcement must still recover the session's cached identity
// rather than running unattributed.
func TestIngest_ShadowMCPPolicyRecoversCachedIdentityForUnresolvableSharedKeyEmail(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	authCtx.APIKeyName = "plugins-hooks-20260708-120102-abc123"
	authCtx.OrgWidePluginHooksKey = true

	cachedUserID := "user_cached_owner"
	sessionID := "canonical-shadow-mcp-unresolvable-email"
	require.NoError(t, ti.service.cache.Set(ctx, sessionCacheKey(sessionID), SessionMetadata{
		SessionID: sessionID,
		UserID:    cachedUserID,
		UserEmail: "cached-dev@example.com",
		GramOrgID: authCtx.ActiveOrganizationID,
		ProjectID: authCtx.ProjectID.String(),
	}, 0))

	ti.service.riskScanner = ingestUserScopedShadowMCPScanner{userID: cachedUserID}

	toolName := "mcp__local_server__search"
	serverIdentity := "local-server"
	toolCallID := "call-unresolvable-email"
	unresolvable := "personal-address@example.net"
	payload := canonicalIngestPayload("claude", "tool.requested", sessionID)
	payload.Source.UserEmail = &unresolvable
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolCallID,
			Name:  &toolName,
			Input: map[string]any{"query": "secret"},
		},
		Mcp: &gen.HookMCPData{
			ServerIdentity: &serverIdentity,
		},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "deny", result.Decision)
}

// A personal key already identifies the developer: a self-reported email that
// matches no Gram user must fall back to the key owner rather than strip
// user-scoped policy checks from the event.
func TestIngest_ShadowMCPPolicyFallsBackToOwnerForUnresolvablePersonalKeyEmail(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	ti.service.riskScanner = ingestUserScopedShadowMCPScanner{userID: authCtx.UserID}

	toolName := "mcp__local_server__search"
	serverIdentity := "local-server"
	toolCallID := "call-personal-unresolvable"
	unresolvable := "personal-address@example.net"
	payload := canonicalIngestPayload("claude", "tool.requested", "canonical-shadow-mcp-personal-unresolvable")
	payload.Source.UserEmail = &unresolvable
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolCallID,
			Name:  &toolName,
			Input: map[string]any{"query": "secret"},
		},
		Mcp: &gen.HookMCPData{
			ServerIdentity: &serverIdentity,
		},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "deny", result.Decision)
}

func TestIngest_ShadowMCPPolicyUsesAuthenticatedTokenOwner(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	ti.service.riskScanner = ingestUserScopedShadowMCPScanner{userID: authCtx.UserID}

	toolName := "mcp__local_server__search"
	serverIdentity := "local-server"
	payload := canonicalIngestPayload("custom-adapter", "tool.requested", "canonical-shadow-mcp")
	toolCallID := "call-1"
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolCallID,
			Name:  &toolName,
			Input: map[string]any{"query": "secret"},
		},
		Mcp: &gen.HookMCPData{
			ServerIdentity: &serverIdentity,
		},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "deny", result.Decision)
	require.NotNil(t, result.Message)
	require.Contains(t, *result.Message, "/blocks/")
	blockID := requireBlockIDFromMessage(t, *result.Message)

	var block riskRepo.GetToolCallBlockRow
	require.Eventually(t, func() bool {
		var err error
		block, err = riskRepo.New(ti.conn).GetToolCallBlock(ctx, riskRepo.GetToolCallBlockParams{
			ID:           blockID,
			ViewerUserID: authCtx.UserID,
		})
		return err == nil
	}, 2*time.Second, 25*time.Millisecond)
	require.Equal(t, *authCtx.ProjectID, block.ProjectID)
	require.Equal(t, "search", block.ToolName.String)
	require.Equal(t, authCtx.UserID, block.UserID)
}

func TestIngest_DuplicateDeliveryDoesNotMintSecondBlockRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	ti.service.riskScanner = ingestUserScopedShadowMCPScanner{userID: authCtx.UserID}

	toolName := "mcp__local_server__search"
	serverIdentity := "local-server"
	idempotencyKey := "dup-" + uuid.NewString()
	payload := canonicalIngestPayload("custom-adapter", "tool.requested", "canonical-shadow-mcp-dup")
	toolCallID := "call-dup-1"
	payload.IdempotencyKey = &idempotencyKey
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolCallID,
			Name:  &toolName,
			Input: map[string]any{"query": "secret"},
		},
		Mcp: &gen.HookMCPData{
			ServerIdentity: &serverIdentity,
		},
	}

	first, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "deny", first.Decision)
	require.NotNil(t, first.Message)
	require.Contains(t, *first.Message, "/blocks/")

	retry, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "deny", retry.Decision, "retried delivery must still be denied")
	require.NotNil(t, retry.Message)
	require.NotContains(t, *retry.Message, "/blocks/",
		"a duplicate delivery must not mint a second block row and URL")
}

// The canonical ingest path attributes events to the payload's self-reported
// user email when present: plugins publish with an org-wide hooks key whose
// token owner is the publishing admin, so the sender's own identity must win.
func TestIngest_SelfReportedUserEmailWinsAttribution(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := "canonical-self-email-" + uuid.NewString()
	chatID := sessionIDToUUID(sessionID)

	selfEmail := "dev@example.com"
	prompt := "hello from the dev machine"
	payload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	payload.Source.UserEmail = &selfEmail
	payload.Data = &gen.HookIngestData{
		Prompt: &gen.HookPromptData{Text: &prompt},
	}

	res, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	msgs, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
		ChatID:    chatID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.Equal(t, selfEmail, msgs[0].ExternalUserID.String,
		"chat message must attribute to the self-reported email, not the token owner")
}

// A shared plugin key with no self-reported email must not attribute events to
// the key's owner (the admin who published the plugin); the event stays
// unattributed instead.
func TestResolveCanonicalActor_SharedPluginKeyDoesNotUseOwnerIdentity(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	payload := canonicalIngestPayload("claude", "prompt.submitted", "actor-test")

	pluginKeyCtx := *authCtx
	pluginKeyCtx.APIKeyName = "plugins-hooks-20260708-120102-abc123"
	pluginKeyCtx.OrgWidePluginHooksKey = true
	actor := ti.service.resolveCanonicalActor(ctx, payload, &pluginKeyCtx)
	require.Empty(t, actor.UserID, "shared plugin key owner must not become the actor")
	require.Empty(t, actor.Email)

	personalKeyCtx := *authCtx
	personalKeyCtx.APIKeyName = "my-personal-key"
	actor = ti.service.resolveCanonicalActor(ctx, payload, &personalKeyCtx)
	require.Equal(t, authCtx.UserID, actor.UserID, "personal keys keep token-owner attribution")

	selfEmail := "dev@example.com"
	payload.Source.UserEmail = &selfEmail
	actor = ti.service.resolveCanonicalActor(ctx, payload, &pluginKeyCtx)
	require.Equal(t, selfEmail, actor.Email, "self-reported email attributes shared-key events")

	legacyPersonalKeyCtx := *authCtx
	legacyPersonalKeyCtx.APIKeyName = "plugins-hooks"
	legacyPersonalKeyCtx.OrgWidePluginHooksKey = false
	payload.Source.UserEmail = nil
	actor = ti.service.resolveCanonicalActor(ctx, payload, &legacyPersonalKeyCtx)
	require.Equal(t, authCtx.UserID, actor.UserID,
		"a legacy personal key with a formerly-unrestricted plugins-* name keeps owner attribution")
}

func TestIngest_CachesSelfReportedActorForLaterSharedKeyEvents(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	authCtx.APIKeyName = "plugins-hooks-20260713-104500-c0d3e1"
	authCtx.OrgWidePluginHooksKey = true
	remaining := make(chan time.Duration, 1)
	ti.service.cache = &sessionCacheDeadlineRecorder{Cache: ti.service.cache, remaining: remaining}

	userID := "user_codex_session_actor"
	userEmail := "codex-session@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userID, userEmail)

	sessionID := "canonical-codex-session-" + uuid.NewString()
	started := canonicalIngestPayload("codex", "session.started", sessionID)
	started.Source.UserEmail = &userEmail
	result, err := ti.service.Ingest(ctx, started)
	require.NoError(t, err)
	require.Equal(t, "allow", result.Decision)

	var cached SessionMetadata
	require.NoError(t, ti.service.cache.Get(ctx, sessionCacheKey(sessionID), &cached))
	require.Equal(t, userID, cached.UserID)
	require.Equal(t, userEmail, cached.UserEmail)
	writeBudget := <-remaining
	require.Positive(t, writeBudget, "session cache write must carry a deadline")
	require.LessOrEqual(t, writeBudget, canonicalSessionCacheWriteTimeout)

	later := canonicalIngestPayload("codex", "tool.requested", sessionID)
	actor := ti.service.resolveCanonicalActor(ctx, later, authCtx)
	require.Equal(t, userID, actor.UserID,
		"later shared-key events must recover the actor learned at SessionStart")
	require.Equal(t, userEmail, actor.Email)
}

func TestCanonicalShadowMCPEvidence_PrefersStdioCommand(t *testing.T) {
	t.Parallel()

	toolName := "mcp__mutable_alias__search"
	serverName := "mutable-alias"
	command := "npx -y @modelcontextprotocol/server-linear"
	payload := canonicalIngestPayload("custom-adapter", "tool.requested", "canonical-shadow-mcp-command")
	payload.Data = &gen.HookIngestData{
		Mcp: &gen.HookMCPData{
			ServerName: &serverName,
			Command:    &command,
		},
	}

	evidence := canonicalShadowMCPEvidence(payload, toolName)
	require.Equal(t, command, evidence.ServerIdentity)
}

func TestCanonicalChatTitle_TruncatesByRunes(t *testing.T) {
	t.Parallel()

	text := strings.Repeat("界", 100)
	payload := canonicalIngestPayload("custom-adapter", "prompt.submitted", "unicode-title")
	payload.Data = &gen.HookIngestData{
		Prompt: &gen.HookPromptData{Text: &text},
	}

	title := canonicalChatTitle(payload, "")
	require.True(t, utf8.ValidString(title))
	require.Len(t, []rune(title), 80)
}

func TestIngest_SkillActivationIsAcceptedAsFeatureEvent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	payload := canonicalIngestPayload("claude", "skill.activated", "skill-session")
	payload.Data = &gen.HookIngestData{
		Skill: &gen.HookSkillData{Name: "repo-review"},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "allow", result.Decision)
}

// TestIngest_InferredSkillEmitsDerivedTelemetryRow covers Codex-style skill
// detection, where the sender attaches data.skill to an ordinary tool event
// instead of reclassifying it: the underlying tool row must stay truthful
// (policy scans and tool counts key on it) and the activation must land as a
// separate skill.activated row matching the Claude vocabulary.
func TestIngest_InferredSkillEmitsDerivedTelemetryRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)

	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	occurredAt := timestamp.Format(time.RFC3339Nano)
	raw := "PreToolUse"
	toolName := "Bash"
	toolID := "call_skill_read"
	payload := canonicalIngestPayload("codex", "tool.requested", "codex-skill-session")
	payload.Source.RawEventName = &raw
	payload.Event.OccurredAt = &occurredAt
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolID,
			Name:  &toolName,
			Input: map[string]any{"command": "cat .agents/skills/repo-review/SKILL.md"},
		},
		Skill: &gen.HookSkillData{Name: "repo-review"},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "allow", result.Decision)

	var logs []telemetryrepo.TelemetryLog
	require.Eventually(t, func() bool {
		logs, err = chClient.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
			GramProjectID: authCtx.ProjectID.String(),
			TimeStart:     timestamp.Add(-2 * time.Minute).UnixNano(),
			TimeEnd:       time.Now().Add(time.Minute).UnixNano(),
			GramURNs:      nil,
			SortOrder:     "desc",
			Cursor:        "",
			Limit:         10,
		})
		return err == nil && len(logs) == 2
	}, 2*time.Second, 50*time.Millisecond, "expected the tool row plus a derived skill row")

	byEvent := map[string]telemetryrepo.TelemetryLog{}
	for _, l := range logs {
		switch {
		case strings.Contains(l.Attributes, "skill.activated"):
			byEvent["skill"] = l
		case strings.Contains(l.Attributes, "PreToolUse"):
			byEvent["tool"] = l
		}
	}

	toolRow, ok := byEvent["tool"]
	require.True(t, ok, "the underlying tool event must be recorded with its provider event name")
	require.Contains(t, toolRow.Attributes, `"Bash"`, "the tool row must keep the real tool identity")
	require.NotContains(t, toolRow.Attributes, "skill.activated")

	skillRow, ok := byEvent["skill"]
	require.True(t, ok, "an inferred skill must produce a derived skill.activated row")
	require.Contains(t, skillRow.Attributes, "repo-review")
	require.Contains(t, skillRow.Attributes, `"Skill"`)
	require.NotNil(t, skillRow.GramChatID)
	require.Equal(t, "codex-skill-session", *skillRow.GramChatID)

	// trace_summaries resolves tool_name with any(): on a shared trace the
	// Bash sibling could win the summary and hide the activation from
	// tool_name = 'Skill' skill analytics.
	require.NotNil(t, toolRow.TraceID)
	require.NotNil(t, skillRow.TraceID)
	require.NotEqual(t, *toolRow.TraceID, *skillRow.TraceID,
		"the derived skill row must not share a trace with the tool row")
}

// TestIngest_SkillRowSurvivesToolIOScrub: orgs with tool_io_logs disabled get
// gen_ai.tool.call.arguments deleted before insert, but ClickHouse
// materializes skill_name from that JSON — the scrubber must keep the minimal
// {"skill": name} on Skill rows while still dropping the real tool input.
func TestIngest_SkillRowSurvivesToolIOScrub(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	enabled := func(context.Context, string) (bool, error) { return true, nil }
	disabled := func(context.Context, string) (bool, error) { return false, nil }
	ti.service.telemetryLogger = telemetry.NewLogger(ctx, testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), chConn, enabled, disabled, nil)
	chClient := telemetryrepo.New(chConn)
	authCtx := hookAuthContext(t, ctx)

	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	occurredAt := timestamp.Format(time.RFC3339Nano)
	raw := "PreToolUse"
	toolName := "Bash"
	toolID := "call_scrubbed_skill_read"
	payload := canonicalIngestPayload("codex", "tool.requested", "codex-scrubbed-skill-session")
	payload.Source.RawEventName = &raw
	payload.Event.OccurredAt = &occurredAt
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolID,
			Name:  &toolName,
			Input: map[string]any{"command": "cat .agents/skills/repo-review/SKILL.md # secret-workspace-path"},
		},
		Skill: &gen.HookSkillData{Name: "repo-review"},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "allow", result.Decision)

	var rows []telemetryrepo.TelemetryLog
	require.Eventually(t, func() bool {
		var listErr error
		rows, listErr = chClient.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
			GramProjectID: authCtx.ProjectID.String(),
			TimeStart:     timestamp.Add(-2 * time.Minute).UnixNano(),
			TimeEnd:       time.Now().Add(time.Minute).UnixNano(),
			GramURNs:      nil,
			SortOrder:     "desc",
			Cursor:        "",
			Limit:         10,
		})
		return listErr == nil && len(rows) == 2
	}, 2*time.Second, 50*time.Millisecond)

	var sawSkillRow bool
	for _, row := range rows {
		require.NotContains(t, row.Attributes, "secret-workspace-path",
			"scrubbed orgs must not retain tool input on any row")
		if strings.Contains(row.Attributes, "skill.activated") {
			sawSkillRow = true
			require.Contains(t, row.Attributes, "repo-review",
				"the skill name must survive the tool IO scrub")
		}
	}
	require.True(t, sawSkillRow)
}

// TestIngest_PromptInferredSkillsGetDistinctTraces: skill dashboards count
// activations at trace level, and prompt events carry no tool call id — the
// session-hash trace fallback would collapse every prompt-mention activation
// in a session into one summary row, so each derived row mints its own trace.
func TestIngest_PromptInferredSkillsGetDistinctTraces(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)

	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	occurredAt := timestamp.Format(time.RFC3339Nano)
	for i, promptText := range []string{"use $repo-review on this", "run $repo-review again"} {
		payload := canonicalIngestPayload("codex", "prompt.submitted", "codex-prompt-skill-session")
		payload.Event.OccurredAt = &occurredAt
		key := "prompt-skill-" + uuid.NewString()
		payload.IdempotencyKey = &key
		text := promptText
		payload.Data = &gen.HookIngestData{
			Prompt: &gen.HookPromptData{Text: &text},
			Skill:  &gen.HookSkillData{Name: "repo-review"},
		}
		result, err := ti.service.Ingest(ctx, payload)
		require.NoError(t, err, "ingest %d", i)
		require.Equal(t, "allow", result.Decision)
	}

	var skillTraces []string
	require.Eventually(t, func() bool {
		rows, err := chClient.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
			GramProjectID: authCtx.ProjectID.String(),
			TimeStart:     timestamp.Add(-2 * time.Minute).UnixNano(),
			TimeEnd:       time.Now().Add(time.Minute).UnixNano(),
			GramURNs:      nil,
			SortOrder:     "desc",
			Cursor:        "",
			Limit:         10,
		})
		if err != nil {
			return false
		}
		skillTraces = skillTraces[:0]
		for _, row := range rows {
			if strings.Contains(row.Attributes, "skill.activated") && row.TraceID != nil {
				skillTraces = append(skillTraces, *row.TraceID)
			}
		}
		return len(skillTraces) == 2
	}, 2*time.Second, 50*time.Millisecond, "expected two derived skill rows")
	require.NotEqual(t, skillTraces[0], skillTraces[1],
		"prompt-inferred activations in one session must not share a trace")
}

// TestIngest_ExplicitSkillActivationEmitsSingleRow pins the other half of the
// derived-row gate: a sender-classified skill.activated event is already the
// skill row and must not spawn a duplicate.
func TestIngest_ExplicitSkillActivationEmitsSingleRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)

	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	occurredAt := timestamp.Format(time.RFC3339Nano)
	payload := canonicalIngestPayload("claude", "skill.activated", "claude-skill-session")
	payload.Event.OccurredAt = &occurredAt
	payload.Data = &gen.HookIngestData{
		Skill: &gen.HookSkillData{Name: "repo-review"},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "allow", result.Decision)

	listRows := func() ([]telemetryrepo.TelemetryLog, error) {
		return chClient.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
			GramProjectID: authCtx.ProjectID.String(),
			TimeStart:     timestamp.Add(-2 * time.Minute).UnixNano(),
			TimeEnd:       time.Now().Add(time.Minute).UnixNano(),
			GramURNs:      nil,
			SortOrder:     "desc",
			Cursor:        "",
			Limit:         10,
		})
	}

	require.Eventually(t, func() bool {
		rows, err := listRows()
		return err == nil && len(rows) >= 1
	}, 2*time.Second, 50*time.Millisecond)
	require.Never(t, func() bool {
		rows, err := listRows()
		return err == nil && len(rows) > 1
	}, 500*time.Millisecond, 100*time.Millisecond,
		"an explicit skill.activated event must not mint a derived duplicate")
	rows, err := listRows()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Contains(t, rows[0].Attributes, "skill.activated")
	require.Contains(t, rows[0].Attributes, "repo-review")
}

// TestIngest_BlockedEventDoesNotEmitDerivedSkillRow: a policy-denied tool call
// never ran, so an inferred skill on it is not an activation.
func TestIngest_BlockedEventDoesNotEmitDerivedSkillRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)
	ti.service.riskScanner = ingestUserScopedShadowMCPScanner{userID: authCtx.UserID}

	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	occurredAt := timestamp.Format(time.RFC3339Nano)
	toolName := "mcp__local_server__search"
	serverIdentity := "local-server"
	toolCallID := "call-blocked-skill"
	payload := canonicalIngestPayload("codex", "tool.requested", "codex-blocked-skill-session")
	payload.Event.OccurredAt = &occurredAt
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolCallID,
			Name:  &toolName,
			Input: map[string]any{"query": "cat .agents/skills/repo-review/SKILL.md"},
		},
		Mcp:   &gen.HookMCPData{ServerIdentity: &serverIdentity},
		Skill: &gen.HookSkillData{Name: "repo-review"},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "deny", result.Decision)

	listRows := func() ([]telemetryrepo.TelemetryLog, error) {
		return chClient.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
			GramProjectID: authCtx.ProjectID.String(),
			TimeStart:     timestamp.Add(-2 * time.Minute).UnixNano(),
			TimeEnd:       time.Now().Add(time.Minute).UnixNano(),
			GramURNs:      nil,
			SortOrder:     "desc",
			Cursor:        "",
			Limit:         10,
		})
	}

	require.Eventually(t, func() bool {
		rows, err := listRows()
		return err == nil && len(rows) >= 1
	}, 2*time.Second, 50*time.Millisecond)
	require.Never(t, func() bool {
		rows, err := listRows()
		if err != nil {
			return false
		}
		for _, row := range rows {
			if strings.Contains(row.Attributes, "skill.activated") {
				return true
			}
		}
		return false
	}, 500*time.Millisecond, 100*time.Millisecond,
		"a blocked event must not produce a derived activation row")
}

func TestIngest_ThoughtEventsExcludedFromTranscript(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := "canonical-thought-" + uuid.NewString()
	chatID := sessionIDToUUID(sessionID)

	text := "internal reasoning about the task"
	role := "assistant"
	thoughtPayload := canonicalIngestPayload("cursor", "assistant.thought", sessionID)
	thoughtPayload.Data = &gen.HookIngestData{
		Message: &gen.HookMessageData{Text: &text, Role: &role},
	}
	res, err := ti.service.Ingest(ctx, thoughtPayload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	// Same data shape as assistant.responded, which does persist — proving
	// the exclusion is keyed on the event type, not on missing content.
	responsePayload := canonicalIngestPayload("cursor", "assistant.responded", sessionID)
	responsePayload.Data = &gen.HookIngestData{
		Message: &gen.HookMessageData{Text: &text, Role: &role},
	}
	res, err = ti.service.Ingest(ctx, responsePayload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	msgs, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
		ChatID:    chatID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, msgs, 1, "thought events must not be persisted as chat messages")
	require.Equal(t, "assistant", msgs[0].Role)
}

// TestIngest_LinksChatToUserAccount confirms the canonical ingest path adopts
// the account attribution the OTEL path cached for the session, so a chat
// created here is linked to its user_accounts row — the join the
// account-identity risk rules and the personal/team classification read.
// Without the merge, chats captured through /rpc/hooks.ingest are never
// linked (the payload itself carries no AI-account identity). The link is
// adopted without rewriting the canonical user identity: UserID/UserEmail
// stay the authenticated actor's, and the account's own email rides
// separately (ObservedUserEmail / gram.account_email).
func TestIngest_LinksChatToUserAccount(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := "canonical-account-link-" + uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	userAccountID := uuid.NewString()

	// Seed session metadata as the OTEL path would for an attributed personal
	// account. No ObservedUserEmail: entries cached before the field existed
	// must still adopt via the UserEmail fallback.
	require.NoError(t, ti.service.cache.Set(ctx, sessionCacheKey(sessionID), SessionMetadata{
		SessionID:     sessionID,
		ServiceName:   "claude-code",
		UserEmail:     "personal@gmail.com",
		UserID:        "bridged-employee",
		Provider:      providerAnthropic,
		AccountType:   accountTypePersonal,
		UserAccountID: userAccountID,
		GramOrgID:     authCtx.ActiveOrganizationID,
		ProjectID:     authCtx.ProjectID.String(),
	}, time.Hour))

	prompt := "hello from a canonical hook"
	payload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	payload.Data = &gen.HookIngestData{
		Prompt: &gen.HookPromptData{Text: &prompt},
	}
	res, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	chat, err := chatRepo.New(ti.conn).GetChat(ctx, chatID)
	require.NoError(t, err)
	require.True(t, chat.UserAccountID.Valid)
	require.Equal(t, userAccountID, chat.UserAccountID.UUID.String())

	// The canonical identity is the authenticated actor, not the cached
	// account identity: the session was sent under the actor's token.
	require.Equal(t, authCtx.UserID, chat.UserID.String)
	require.NotEqual(t, "personal@gmail.com", chat.ExternalUserID.String,
		"account email must not replace the actor's on the chat")

	// Message rows carry the same identity as the linked chat — both are
	// written from the hydrated session metadata, not the raw auth context.
	msgs, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
		ChatID:    chatID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.Equal(t, chat.UserID.String, msgs[0].UserID.String)
	require.Equal(t, chat.ExternalUserID.String, msgs[0].ExternalUserID.String)
}

// TestIngest_StampsAccountAttributionOnTelemetry confirms canonical hook rows
// carry the cached account attribution (provider, account_type, external org
// id) with the account's own email as the gram.account_email attribute, while
// user.email stays the authenticated actor — dashboards and policies reading
// telemetry see both the AI account behind the session and the Gram identity
// that sent it, without one masquerading as the other.
func TestIngest_StampsAccountAttributionOnTelemetry(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)

	sessionID := "canonical-stamp-" + uuid.NewString()
	externalOrgID := "stamp-ext-org-" + sessionID
	require.NoError(t, ti.service.cache.Set(ctx, sessionCacheKey(sessionID), SessionMetadata{
		SessionID:         sessionID,
		ServiceName:       "claude-code",
		UserEmail:         "personal@gmail.com",
		UserID:            "bridged-employee",
		Provider:          providerAnthropic,
		ExternalOrgID:     externalOrgID,
		AccountType:       accountTypePersonal,
		UserAccountID:     uuid.NewString(),
		ObservedUserEmail: "personal@gmail.com",
		GramOrgID:         authCtx.ActiveOrganizationID,
		ProjectID:         authCtx.ProjectID.String(),
	}, time.Hour))

	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	occurredAt := timestamp.Format(time.RFC3339Nano)
	prompt := "attribution should ride on this row"
	payload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	payload.Event.OccurredAt = &occurredAt
	payload.Data = &gen.HookIngestData{
		Prompt: &gen.HookPromptData{Text: &prompt},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "allow", result.Decision)

	var logs []telemetryrepo.TelemetryLog
	require.Eventually(t, func() bool {
		logs, err = chClient.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
			GramProjectID: authCtx.ProjectID.String(),
			TimeStart:     timestamp.Add(-2 * time.Minute).UnixNano(),
			TimeEnd:       time.Now().Add(time.Minute).UnixNano(),
			GramURNs:      nil,
			SortOrder:     "desc",
			Cursor:        "",
			Limit:         10,
		})
		return err == nil && len(logs) == 1
	}, 2*time.Second, 50*time.Millisecond, "expected the hook row to land in telemetry")

	require.Contains(t, logs[0].Attributes, providerAnthropic)
	require.Contains(t, logs[0].Attributes, accountTypePersonal)
	require.Contains(t, logs[0].Attributes, externalOrgID)
	// Attribute keys nest on dots in the stored JSON: gram.account_email
	// carries the account's own email while user.email stays the actor.
	require.Contains(t, logs[0].Attributes, `"account_email":"personal@gmail.com"`,
		"account email must ride as its own attribute")
	require.NotContains(t, logs[0].Attributes, `"email":"personal@gmail.com"`,
		"account email must not replace the actor's user.email")
}

func TestIngest_PersistsRenderableToolCalls(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := "canonical-tools-" + uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	toolCallID := "call_" + uuid.NewString()
	toolName := "Read"

	prompt := "read the file"
	promptPayload := canonicalIngestPayload("custom-adapter", "prompt.submitted", sessionID)
	promptTurnID := "turn-prompt"
	promptPayload.Session.TurnID = &promptTurnID
	promptPayload.Data = &gen.HookIngestData{
		Prompt: &gen.HookPromptData{Text: &prompt},
	}
	res, err := ti.service.Ingest(ctx, promptPayload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	requestPayload := canonicalIngestPayload("custom-adapter", "tool.requested", sessionID)
	requestTurnID := "turn-tool-request"
	requestPayload.Session.TurnID = &requestTurnID
	requestPayload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolCallID,
			Name:  &toolName,
			Input: map[string]any{"file_path": "/tmp/input.txt"},
		},
	}
	res, err = ti.service.Ingest(ctx, requestPayload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	resultPayload := canonicalIngestPayload("custom-adapter", "tool.completed", sessionID)
	resultTurnID := "turn-tool-result"
	resultPayload.Session.TurnID = &resultTurnID
	resultPayload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:     &toolCallID,
			Name:   &toolName,
			Output: map[string]any{"content": "ok"},
		},
	}
	res, err = ti.service.Ingest(ctx, resultPayload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	msgs, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
		ChatID:    chatID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, msgs, 3)

	var toolRequest, toolResult chatRepo.ChatMessage
	for _, msg := range msgs {
		require.Zero(t, msg.Generation, "hook turn IDs must not split chat.load generations")
		switch {
		case msg.Role == "assistant" && len(msg.ToolCalls) > 0:
			toolRequest = msg
		case msg.Role == "tool":
			toolResult = msg
		}
	}
	require.NotEmpty(t, toolRequest.ID)
	require.Equal(t, "tool_calls", toolRequest.FinishReason.String)
	require.Equal(t, "custom-adapter", toolRequest.Source.String)

	var toolCalls []struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	require.NoError(t, json.Unmarshal(toolRequest.ToolCalls, &toolCalls))
	require.Len(t, toolCalls, 1)
	require.Equal(t, toolCallID, toolCalls[0].ID)
	require.Equal(t, "function", toolCalls[0].Type)
	require.Equal(t, toolName, toolCalls[0].Function.Name)
	require.JSONEq(t, `{"file_path":"/tmp/input.txt"}`, toolCalls[0].Function.Arguments)

	require.NotEmpty(t, toolResult.ID)
	require.Equal(t, "tool", toolResult.Role)
	require.Equal(t, toolCallID, toolResult.ToolCallID.String)
	require.JSONEq(t, `{"content":"ok"}`, toolResult.Content)
	require.Equal(t, "custom-adapter", toolResult.Source.String)
}

// Codex PermissionRequest normalizes to tool.requested but is only a
// pre-approval preview — it may be denied or followed by the real request,
// so it must not create tool_calls rows in the captured transcript.
func TestIngest_PermissionRequestsNotPersistedAsToolCalls(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := "canonical-perms-" + uuid.NewString()
	toolName := "shell"
	permissionType := "exec"
	rawEvent := "PermissionRequest"

	payload := canonicalIngestPayload("codex", "tool.requested", sessionID)
	payload.Source.RawEventName = &rawEvent
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			Name:           &toolName,
			Input:          map[string]any{"command": "ls"},
			PermissionType: &permissionType,
		},
	}
	res, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	msgs, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
		ChatID:    sessionIDToUUID(sessionID),
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Empty(t, msgs, "permission prompts must not persist chat rows")
}

func canonicalIngestPayload(adapter, eventType, sessionID string) *gen.IngestPayload {
	return &gen.IngestPayload{
		SchemaVersion: hookIngestSchemaV1,
		Source: &gen.HookIngestSource{
			Adapter: adapter,
		},
		Session: &gen.HookIngestSession{
			ID: &sessionID,
		},
		Event: &gen.HookIngestEvent{
			Type: eventType,
		},
	}
}

// The gram.hook.event attribute vocabulary is the provider-style HookEvent
// names — ClickHouse summary predicates match on PostToolUse and friends, so
// canonical event types must translate back before they reach telemetry.
func TestTelemetryHookEventName_TranslatesCanonicalVocabulary(t *testing.T) {
	t.Parallel()

	withRaw := func(adapter, eventType, raw string) *gen.IngestPayload {
		payload := canonicalIngestPayload(adapter, eventType, "vocab-session")
		payload.Source.RawEventName = &raw
		return payload
	}

	// Known adapters resolve through their raw provider event name.
	require.Equal(t, "PostToolUse", telemetryHookEventName(withRaw("claude", "tool.completed", "PostToolUse")))
	require.Equal(t, "PostToolUseFailure", telemetryHookEventName(withRaw("claude", "tool.failed", "PostToolUseFailure")))
	require.Equal(t, "AfterMCPExecution", telemetryHookEventName(withRaw("cursor", "tool.completed", "afterMCPExecution")))
	require.Equal(t, "BeforeMCPExecution", telemetryHookEventName(withRaw("cursor", "tool.requested", "beforeMCPExecution")))
	require.Equal(t, "UserPromptSubmit", telemetryHookEventName(withRaw("claude", "prompt.submitted", "UserPromptSubmit")))
	require.Equal(t, "PermissionRequest", telemetryHookEventName(withRaw("codex", "tool.requested", "PermissionRequest")))

	// Unrecognized raw names for known adapters fall back to the canonical map.
	require.Equal(t, "PreToolUse", telemetryHookEventName(withRaw("cursor", "tool.requested", "beforeReadFile")))

	// Custom adapters have no raw vocabulary: canonical types map to their
	// provider-style equivalents so summaries still count them.
	require.Equal(t, "PostToolUse", telemetryHookEventName(canonicalIngestPayload("openclaw", "tool.completed", "vocab-session")))
	require.Equal(t, "SessionStart", telemetryHookEventName(canonicalIngestPayload("openclaw", "session.started", "vocab-session")))
	require.Equal(t, "AfterAgentThought", telemetryHookEventName(canonicalIngestPayload("openclaw", "assistant.thought", "vocab-session")))

	// Canonical types without a provider-style equivalent pass through.
	require.Equal(t, "usage.reported", telemetryHookEventName(canonicalIngestPayload("openclaw", "usage.reported", "vocab-session")))

	// Skill activation is layered onto an ordinary tool event; the raw
	// provider name must not erase it.
	require.Equal(t, "skill.activated", telemetryHookEventName(withRaw("claude", "skill.activated", "PostToolUse")))
}

// TestIngest_ReplayedFlagPersistsOnChatMessage pins the DNO-499 contract: a
// message redelivered from a device's offline spool (X-Gram-Replayed)
// persists chat_messages.replayed=true and a live message does not — the bit
// risk-results reads surface so findings from retroactive scanning stay
// distinguishable from live ones.
func TestIngest_ReplayedFlagPersistsOnChatMessage(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := "replayed-flag-" + uuid.NewString()
	chatID := sessionIDToUUID(sessionID)

	replayedPrompt := "replayed prompt from downtime backlog"
	replayed := true
	replayedPayload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	replayedPayload.Data = &gen.HookIngestData{
		Prompt: &gen.HookPromptData{Text: &replayedPrompt},
	}
	replayedPayload.Replayed = &replayed
	res, err := ti.service.Ingest(ctx, replayedPayload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	livePrompt := "live prompt after recovery"
	livePayload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	livePayload.Data = &gen.HookIngestData{
		Prompt: &gen.HookPromptData{Text: &livePrompt},
	}
	res, err = ti.service.Ingest(ctx, livePayload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	msgs, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
		ChatID:    chatID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	replayedByContent := make(map[string]bool, len(msgs))
	for _, m := range msgs {
		replayedByContent[m.Content] = m.Replayed
	}
	require.True(t, replayedByContent[replayedPrompt], "a replayed delivery must persist replayed=true")
	require.False(t, replayedByContent[livePrompt], "a live delivery must persist replayed=false")
}

// TestIngest_ReplayedMessageSortsByOccurredAt pins the DNO-536 contract: rows
// persist with the event's original occurred_at as created_at, so downtime
// backlog replayed AFTER a live event still sorts BEFORE it in transcript
// order — arrival order must not decide conversation order. A future
// occurred_at (skewed device clock) is clamped to arrival time so it cannot
// sort past rows that come after it.
func TestIngest_ReplayedMessageSortsByOccurredAt(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := "occurred-at-order-" + uuid.NewString()
	chatID := sessionIDToUUID(sessionID)

	// The live event arrives FIRST but occurred later — the recovery case:
	// the send that proves the control plane is back precedes the drain.
	livePrompt := "i need you"
	liveAt := time.Now().UTC().Format(time.RFC3339Nano)
	livePayload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	livePayload.Data = &gen.HookIngestData{
		Prompt: &gen.HookPromptData{Text: &livePrompt},
	}
	livePayload.Event.OccurredAt = &liveAt
	res, err := ti.service.Ingest(ctx, livePayload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	// The backlog event arrives SECOND but occurred five minutes earlier.
	backlogPrompt := "nothing, just chilling"
	backlogAt := time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339Nano)
	replayed := true
	backlogPayload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	backlogPayload.Data = &gen.HookIngestData{
		Prompt: &gen.HookPromptData{Text: &backlogPrompt},
	}
	backlogPayload.Event.OccurredAt = &backlogAt
	backlogPayload.Replayed = &replayed
	res, err = ti.service.Ingest(ctx, backlogPayload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	// A skewed clock cannot push a row into the future.
	skewedPrompt := "from the future"
	skewedAt := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	skewedPayload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	skewedPayload.Data = &gen.HookIngestData{
		Prompt: &gen.HookPromptData{Text: &skewedPrompt},
	}
	skewedPayload.Event.OccurredAt = &skewedAt
	res, err = ti.service.Ingest(ctx, skewedPayload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	// Nor arbitrarily far into the past: occurred_at is client-controlled,
	// and without a floor one hostile/broken clock would pin a row to the
	// head of the transcript forever. The floor mirrors the client spool's
	// 14-day expiry — no legitimate replay is older.
	ancientPrompt := "from the distant past"
	ancientAt := time.Now().UTC().Add(-30 * 24 * time.Hour).Format(time.RFC3339Nano)
	ancientPayload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	ancientPayload.Data = &gen.HookIngestData{
		Prompt: &gen.HookPromptData{Text: &ancientPrompt},
	}
	ancientPayload.Event.OccurredAt = &ancientAt
	res, err = ti.service.Ingest(ctx, ancientPayload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	msgs, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
		ChatID:    chatID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, msgs, 4)
	require.Equal(t, ancientPrompt, msgs[0].Content, "a floored past event still sorts oldest")
	require.Equal(t, backlogPrompt, msgs[1].Content, "the older backlog message must sort before the live event despite arriving second")
	require.Equal(t, livePrompt, msgs[2].Content)
	require.Equal(t, skewedPrompt, msgs[3].Content)

	wantBacklogAt, err := time.Parse(time.RFC3339Nano, backlogAt)
	require.NoError(t, err)
	require.WithinDuration(t, wantBacklogAt, msgs[1].CreatedAt.Time, time.Second, "created_at must carry the event's occurred_at")
	require.WithinDuration(t, time.Now(), msgs[3].CreatedAt.Time, 30*time.Second, "a future occurred_at must be clamped to arrival time")
	require.WithinDuration(t, time.Now().Add(-14*24*time.Hour), msgs[0].CreatedAt.Time, 30*time.Second, "a far-past occurred_at must be floored to the 14-day backdate bound")
}
