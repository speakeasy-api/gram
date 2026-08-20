package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	riskRepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
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
	if strings.HasPrefix(key, "session:metadata:") {
		deadline, ok := ctx.Deadline()
		if ok {
			r.remaining <- time.Until(deadline)
		} else {
			r.remaining <- 0
		}
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

// The OTEL logs path caches service.name "cowork" for a session's metadata;
// the desktop hook client then sends its generic "claude-code-desktop"
// adapter slug on every canonical event (cowork and Claude Code Desktop share
// it). The metadata merge must keep the more specific cowork identity so
// chat sources and hook_source stay "cowork", and a session.started re-cache
// never downgrades it back to the adapter.
func TestCanonicalSessionMetadata_KeepsCachedCoworkServiceName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := "canonical-cowork-surface"
	require.NoError(t, ti.service.cache.Set(ctx, sessionCacheKey(sessionID), SessionMetadata{
		SessionID:   sessionID,
		ServiceName: "cowork",
		GramOrgID:   authCtx.ActiveOrganizationID,
		ProjectID:   authCtx.ProjectID.String(),
	}, 0))

	payload := canonicalIngestPayload("claude-code-desktop", "session.started", sessionID)
	metadata := ti.service.canonicalSessionMetadata(ctx, payload, authCtx, canonicalActor{UserID: "", Email: ""})
	require.Equal(t, "cowork", metadata.ServiceName)
}

// With no cached OTEL identity (the session's log stream has not arrived
// yet), the canonical metadata keeps the adapter slug — for the desktop
// client that means "claude-code-desktop", which is itself a distinct surface
// from the claude-code CLI.
func TestCanonicalSessionMetadata_AdapterStandsWithoutCachedServiceName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	payload := canonicalIngestPayload("claude-code-desktop", "session.started", "canonical-ccd-no-cache")
	metadata := ti.service.canonicalSessionMetadata(ctx, payload, authCtx, canonicalActor{UserID: "", Email: ""})
	require.Equal(t, "claude-code-desktop", metadata.ServiceName)
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

func TestIngest_RejectsReservedAssistantAdapter(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	payload := canonicalIngestPayload("assistant", "tool.requested", "reserved-adapter")
	toolName := "bun_run"
	toolCallID := "call-reserved"
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolCallID,
			Name:  &toolName,
			Input: map[string]any{},
		},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.Error(t, err)
	require.Nil(t, result)
	require.Contains(t, err.Error(), "source.adapter is reserved")
}

func TestIngestAuthenticated_RejectsReservedAssistantAdapterByDefault(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	payload := canonicalIngestPayload("assistant", "tool.requested", "reserved-adapter-auth")
	toolName := "bun_run"
	toolCallID := "call-reserved-auth"
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolCallID,
			Name:  &toolName,
			Input: map[string]any{},
		},
	}

	result, err := ti.service.IngestAuthenticated(t.Context(), authCtx, payload)
	require.Error(t, err)
	require.Nil(t, result)
	require.Contains(t, err.Error(), "source.adapter is reserved")
}

func TestIngestAuthenticated_AllowsReservedAssistantAdapter(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := uuid.NewString()
	payload := canonicalIngestPayload("assistant", "tool.requested", sessionID)
	toolName := "bun_run"
	toolCallID := "call-assistant-allow"
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolCallID,
			Name:  &toolName,
			Input: map[string]any{"code": "1"},
		},
	}

	result, err := ti.service.IngestAuthenticatedWithOptions(t.Context(), authCtx, payload, AuthenticatedIngestOptions{
		AllowWarnAcknowledgement:     true,
		AllowSessionIdentityFallback: false,
		SourceAttributes:             nil,
		OutputToolCalls:              nil,
		OriginatingClient:            "assistant",
		AllowReservedAdapter:         true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "allow", result.Decision)

	messages, err := chatRepo.New(ti.conn).ListChatMessages(t.Context(), chatRepo.ListChatMessagesParams{
		ChatID:    sessionIDToUUID(sessionID),
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Empty(t, messages)
}

func TestIngestAssistantToolCall_AllowsReservedAdapter(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	payload := canonicalIngestPayload("assistant", "tool.requested", uuid.NewString())
	toolName := "bun_run"
	toolCallID := "call-assistant-method"
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &toolCallID,
			Name:  &toolName,
			Input: map[string]any{"code": "1"},
		},
	}

	result, err := ti.service.IngestAssistantToolCall(t.Context(), authCtx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "allow", result.Decision)
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

func TestIngestAuthenticated_UsesSuppliedIdentity(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := "authenticated-ingest-" + uuid.NewString()
	idempotencyKey := "authenticated-ingest-" + uuid.NewString()
	prompt := "authenticated ingestion prompt " + uuid.NewString()
	untrustedAPIKey := "must-not-override-authenticated-context"
	untrustedProjectSlug := "must-not-override-authenticated-project"
	payload := canonicalIngestPayload("litellm", "prompt.submitted", sessionID)
	payload.ApikeyToken = &untrustedAPIKey
	payload.ProjectSlugInput = &untrustedProjectSlug
	payload.IdempotencyKey = &idempotencyKey
	payload.Data = &gen.HookIngestData{
		Prompt: &gen.HookPromptData{Text: &prompt},
	}
	ti.service.riskScanner = &stubResultScanner{result: &risk.ScanResult{
		Action:      "block",
		PolicyID:    uuid.NewString(),
		PolicyName:  "authenticated boundary policy",
		Description: "blocked by deterministic test scanner",
	}}

	result, err := ti.service.IngestAuthenticated(t.Context(), authCtx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "deny", result.Decision)

	messages, err := chatRepo.New(ti.conn).ListChatMessages(t.Context(), chatRepo.ListChatMessagesParams{
		ChatID:    sessionIDToUUID(sessionID),
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.True(t, messages[0].ProjectID.Valid)
	require.Equal(t, *authCtx.ProjectID, messages[0].ProjectID.UUID)
	require.Equal(t, "user", messages[0].Role)
	require.Equal(t, prompt, messages[0].Content)
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

func TestCanonicalAgentTurnIDAcceptsLegacyCodexTurnID(t *testing.T) {
	t.Parallel()

	payload := canonicalIngestPayload("codex", "prompt.submitted", "codex-session")
	payload.Session.TurnID = new("turn-1")
	require.Equal(t, "codex:turn-1", canonicalAgentTurnID(payload))
}

func TestCanonicalAgentTurnIDExtractsLegacyOpenCodeMessageID(t *testing.T) {
	t.Parallel()

	payload := canonicalIngestPayload("opencode", "prompt.submitted", "opencode-session")
	payload.Raw = json.RawMessage(`{"input":{"messageID":"msg-input"},"output":{"message":{"id":"msg-output"}}}`)
	require.Equal(t, "opencode:msg-output", canonicalAgentTurnID(payload))
}

func TestCanonicalAgentTurnIDRejectsSpoofedProviderPrefix(t *testing.T) {
	t.Parallel()

	payload := canonicalIngestPayload("custom-adapter", "prompt.submitted", "custom-session")
	payload.Session.TurnID = new("agent-turn:v1:opencode:msg-1")
	require.Empty(t, canonicalAgentTurnID(payload))
}

func TestCanonicalAgentTurnIDRejectsProviderWithoutSharedIdentity(t *testing.T) {
	t.Parallel()

	payload := canonicalIngestPayload("claude", "prompt.submitted", "claude-session")
	payload.Session.TurnID = new("turn-1")
	require.Empty(t, canonicalAgentTurnID(payload))
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
	require.Equal(t, sessionIDToUUID("codex-skill-session").String(), *skillRow.GramChatID,
		"telemetry carries the resolved chat id, not the raw session id")

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
	ti.service.telemetryLogger = telemetry.NewLogger(ctx, testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), chConn, enabled, disabled, nil, telemetry.NewNoopLogPublisher(testenv.NewLogger(t)))
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

// TestIngest_NonUUIDSessionIDStampsResolvedChatID pins the cross-store identity
// invariant: telemetry_logs.chat_id (gen_ai.conversation.id) must be the chats
// row id, not the raw agent session id. Claude/Codex/Cursor session ids are
// themselves UUIDs so the two coincided by accident; opencode's ("ses_...") are
// not, and stamping the raw string sent a malformed UUID to every consumer that
// loads a chat by the telemetry id — the costs page session drill-in among them.
func TestIngest_NonUUIDSessionIDStampsResolvedChatID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	// The chats row only exists when session capture is on — this test compares
	// the two stores, so it needs the transcript side written.
	ti.service.productFeatures = alwaysEnabledFeatures{}
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)

	const sessionID = "ses_05c47a44fffeCPIAOSNROITwrf"
	_, err := uuid.Parse(sessionID)
	require.Error(t, err, "the fixture must be a non-UUID session id for this test to mean anything")

	chatID := sessionIDToUUID(sessionID)
	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)

	prompt := "hello from opencode"
	payload := canonicalIngestPayload("opencode", "prompt.submitted", sessionID)
	payload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}
	res, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	// The transcript lands under the mapped UUID.
	persisted, err := chatRepo.New(ti.conn).GetChat(ctx, chatRepo.GetChatParams{ID: chatID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)

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
	}, 2*time.Second, 50*time.Millisecond, "expected the ingested prompt row")

	require.NotNil(t, logs[0].GramChatID)
	require.NotEqual(t, sessionID, *logs[0].GramChatID, "the raw agent session id must not reach telemetry as the chat id")
	require.Equal(t, persisted.ID.String(), *logs[0].GramChatID,
		"telemetry chat_id must resolve to the same chats row the transcript was stored under")

	// The costs page hands this value straight to a uuid-typed endpoint.
	_, err = uuid.Parse(*logs[0].GramChatID)
	require.NoError(t, err, "telemetry chat_id must be a parseable UUID")
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

	chat, err := chatRepo.New(ti.conn).GetChat(ctx, chatRepo.GetChatParams{ID: chatID, ProjectID: *authCtx.ProjectID})
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

func TestIngest_PersistsPromptAttachmentsAsScannableToolRows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := "prompt-attachment-" + uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	promptID := "prompt-" + uuid.NewString()
	entryUUID := "attachment-" + uuid.NewString()
	displayPath := "marker.txt"
	filePath := "/repo/marker.txt"
	content := strings.Repeat("safe prefix\n", 500) + "MARKER_abc123\n"
	timestamp := "2026-07-22T09:38:49.652Z"
	numLines := 1
	totalLines := 1
	startLine := 1

	promptText := "Summarize @marker.txt"
	promptPayload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	promptPayload.Data = &gen.HookIngestData{
		Prompt: &gen.HookPromptData{Text: &promptText},
	}
	res, err := ti.service.Ingest(ctx, promptPayload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	secondPromptText := "Unrelated follow-up prompt"
	secondPromptPayload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	secondPromptPayload.Data = &gen.HookIngestData{
		Prompt: &gen.HookPromptData{Text: &secondPromptText},
	}
	res, err = ti.service.Ingest(ctx, secondPromptPayload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	promptSHA256 := testPromptSHA256(promptText)
	payload := canonicalIngestPayload("claude", "assistant.responded", sessionID)
	payload.Data = &gen.HookIngestData{
		PromptAttachments: []*gen.HookPromptAttachmentEntry{{
			EntryUUID:      entryUUID,
			PromptID:       &promptID,
			PromptSha256:   &promptSHA256,
			FilePath:       &filePath,
			DisplayPath:    &displayPath,
			AttachmentKind: "file",
			Content:        content,
			NumLines:       &numLines,
			TotalLines:     &totalLines,
			StartLine:      &startLine,
			Timestamp:      &timestamp,
		}},
	}

	res, err = ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	msgs, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
		ChatID:    chatID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	var promptRow, secondPromptRow *chatRepo.ChatMessage
	for i := range msgs {
		if msgs[i].Role == "user" && msgs[i].Content == promptText {
			promptRow = &msgs[i]
		}
		if msgs[i].Role == "user" && msgs[i].Content == secondPromptText {
			secondPromptRow = &msgs[i]
		}
	}
	require.NotNil(t, promptRow)
	require.NotNil(t, secondPromptRow)
	require.Equal(t, promptText, promptRow.Content)
	require.False(t, promptRow.MessageID.Valid)
	require.False(t, secondPromptRow.MessageID.Valid, "hash match must not stamp the newer unrelated prompt")

	parts, err := chatRepo.New(ti.conn).ListChatContentPartsByChatID(ctx, chatRepo.ListChatContentPartsByChatIDParams{
		ChatID:               chatID,
		ProjectID:            *authCtx.ProjectID,
		ParentChatMessageIds: []uuid.UUID{promptRow.ID, secondPromptRow.ID},
	})
	require.NoError(t, err)
	require.Len(t, parts, 1)
	attachmentRow := parts[0]
	require.Equal(t, message.PromptAttachment, attachmentRow.Kind)
	require.NotEmpty(t, attachmentRow.ContentAssetUrl)
	assetURL, err := url.Parse(attachmentRow.ContentAssetUrl)
	require.NoError(t, err)
	reader, err := ti.assetStorage.Read(ctx, assetURL)
	require.NoError(t, err)
	assetContent, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.Equal(t, content, string(assetContent))
	require.Equal(t, entryUUID, attachmentRow.ExternalID.String)
	require.Equal(t, promptRow.ID, attachmentRow.ParentChatMessageID.UUID)
	require.JSONEq(t, `{"display_path": "marker.txt", "kind": "file"}`, string(attachmentRow.Metadata))
	require.True(t, attachmentRow.CreatedAt.Valid)
	require.Equal(t, int64(2026), int64(attachmentRow.CreatedAt.Time.Year()))

	policyID, err := uuid.NewV7()
	require.NoError(t, err)
	policy, err := riskRepo.New(ti.conn).CreateRiskPolicy(ctx, riskRepo.CreateRiskPolicyParams{
		ID:             policyID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           "content part test policy",
		Sources:        []string{"gitleaks"},
		Enabled:        true,
		Action:         "flag",
		AudienceType:   "everyone",
		AutoName:       false,
		UserMessage:    pgtype.Text{},
	})
	require.NoError(t, err)
	resultID, err := uuid.NewV7()
	require.NoError(t, err)
	_, err = riskRepo.New(ti.conn).InsertRiskResults(ctx, []riskRepo.InsertRiskResultsParams{{
		ID:                resultID,
		ProjectID:         *authCtx.ProjectID,
		OrganizationID:    authCtx.ActiveOrganizationID,
		RiskPolicyID:      policy.ID,
		RiskPolicyVersion: policy.Version,
		ChatMessageID:     uuid.NullUUID{},
		ChatContentPartID: uuid.NullUUID{UUID: attachmentRow.ID, Valid: true},
		Source:            "gitleaks",
		Found:             true,
		RuleID:            pgtype.Text{String: "secret.test", Valid: true},
		Description:       pgtype.Text{String: "test finding", Valid: true},
		Match:             pgtype.Text{String: "secret", Valid: true},
		StartPos:          pgtype.Int4{Int32: 0, Valid: true},
		EndPos:            pgtype.Int4{Int32: 6, Valid: true},
		Confidence:        pgtype.Float8{Float64: 1, Valid: true},
		Tags:              []string{},
	}})
	require.NoError(t, err)
	results, err := testrepo.New(ti.conn).ListRiskResultsAll(ctx, testrepo.ListRiskResultsAllParams{
		ProjectID:    *authCtx.ProjectID,
		RiskPolicyID: policy.ID,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.False(t, results[0].ChatMessageID.Valid)
	require.Equal(t, attachmentRow.ID, results[0].ChatContentPartID.UUID)

	findingID, err := uuid.NewV7()
	require.NoError(t, err)
	chInserter := &recordingRiskFindingInserter{rows: nil}
	fingerprinter, err := risk.ParsePepperKeyRing(fmt.Appendf(nil, `{"current":"v1","keys":{"v1":%q}}`, base64.StdEncoding.EncodeToString([]byte("test-fingerprint-key-material"))))
	require.NoError(t, err)
	chWriter := risk.NewFindingCHWriter(testenv.NewLogger(t), ti.conn, testenv.NewMeterProvider(t), chInserter, fingerprinter)
	startPos := int32(0)
	endPos := int32(6)
	confidence := float64(1)
	createdAt := time.Now().UTC().Format(time.RFC3339)
	require.NoError(t, chWriter.HandleBatch(ctx, []*riskv1.Finding{
		riskv1.Finding_builder{
			Id:                new(findingID.String()),
			RequestId:         new("req-content-part"),
			ChatMessageId:     nil,
			ContentPartId:     new(attachmentRow.ID.String()),
			ProjectId:         new(authCtx.ProjectID.String()),
			OrganizationId:    &authCtx.ActiveOrganizationID,
			RiskPolicyId:      new(policy.ID.String()),
			RiskPolicyVersion: &policy.Version,
			CreatedAt:         &createdAt,
			RuleId:            new("secret.test"),
			Description:       new("test finding"),
			Match:             new("secret"),
			StartPos:          &startPos,
			EndPos:            &endPos,
			Tags:              []string{},
			Source:            new("gitleaks"),
			Confidence:        &confidence,
			DeadLetterReason:  nil,
		}.Build(),
	}, nil))
	require.Len(t, chInserter.rows, 1)
	require.Empty(t, chInserter.rows[0].ChatMessageID)
	require.Equal(t, attachmentRow.ID.String(), chInserter.rows[0].ContentPartID)
	require.Equal(t, chatID.String(), chInserter.rows[0].ChatID)

	// A redelivered attachment event must not stamp an unrelated prompt row.
	redelivery := canonicalIngestPayload("claude", "assistant.responded", sessionID)
	redelivery.Data = payload.Data
	res, err = ti.service.Ingest(ctx, redelivery)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	msgs, err = chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
		ChatID:    chatID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	for i := range msgs {
		if msgs[i].Role == "user" && msgs[i].Content == secondPromptText {
			require.Empty(t, msgs[i].MessageID.String, "redelivered attachments must not stamp unrelated prompt rows")
		}
	}
}

func testPromptSHA256(text string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return hex.EncodeToString(sum[:])
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

// A sender that omits per-call tool ids must still produce a joinable
// (recorded id, trace id) pair: the shadow-MCP provenance lookup joins via
// trace_id = hashToolCallIDToTraceID(recorded id) (DNO-604).
func TestCanonicalToolCallIDAndTraceID_JoinWithoutPerCallID(t *testing.T) {
	t.Parallel()

	toolName := "mcp__linear__get_issue"
	payload := canonicalIngestPayload("custom-adapter", "tool.requested", "join-session")
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			Name: &toolName,
		},
	}

	require.Equal(t, "12|join-session|mcp__linear__get_issue", canonicalChatToolCallID(payload))
	require.Equal(t, hashToolCallIDToTraceID(canonicalChatToolCallID(payload)), canonicalTraceID(payload))

	// A per-call id, when present, takes precedence on both sides.
	id := "call_123"
	payload.Data.ToolCall.ID = &id
	require.Equal(t, id, canonicalChatToolCallID(payload))
	require.Equal(t, hashToolCallIDToTraceID(id), canonicalTraceID(payload))

	// Without a tool name there is no per-tool key: non-tool events keep the
	// per-session trace.
	require.Equal(t,
		hashToolCallIDToTraceID("join-session"),
		canonicalTraceID(canonicalIngestPayload("custom-adapter", "prompt.submitted", "join-session")),
	)

	// A non-tool event carrying tool_call data must also keep the per-session
	// trace: only tool events have a recorded chat side to join, and anything
	// else migrating into the tool trace would regroup trace_summaries rows.
	skillPayload := canonicalIngestPayload("custom-adapter", "skill.activated", "join-session")
	skillPayload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			Name: &toolName,
		},
	}
	require.Equal(t, hashToolCallIDToTraceID("join-session"), canonicalTraceID(skillPayload))
}

// The synthetic key encoding must be injective: session ids are
// client-controlled, so a "|" inside one must not let two distinct
// (session, tool) pairs share a recorded id — a collision would let the
// provenance lookup resolve one call to another call's MCP server.
func TestSyntheticToolCallID_InjectiveEncoding(t *testing.T) {
	t.Parallel()

	require.NotEqual(t,
		syntheticToolCallID("a|b", "c"),
		syntheticToolCallID("a", "b|c"),
	)
	require.Equal(t, "3|a|b|c", syntheticToolCallID("a|b", "c"))
	require.Equal(t, "1|a|b|c", syntheticToolCallID("a", "b|c"))

	require.Empty(t, syntheticToolCallID("", "tool"))
	require.Empty(t, syntheticToolCallID("session", ""))
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

func TestMergeSourceAttributesDoesNotOverrideCanonicalFields(t *testing.T) {
	t.Parallel()
	base := map[attr.Key]any{attr.ProjectIDKey: "canonical-project"}
	mergeSourceAttributes(base, map[attr.Key]any{
		attr.ProjectIDKey:     "external-project",
		attr.LiteLLMCallIDKey: "call-id",
	})
	require.Equal(t, "canonical-project", base[attr.ProjectIDKey])
	require.Equal(t, "call-id", base[attr.LiteLLMCallIDKey])
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

	// OpenCode's message.part.updated carries every streaming part update, not
	// just failures; agenthooks decides whether it is a real tool failure, so the
	// raw name must defer to the canonical Event.Type instead of forcing a
	// failure. session.idle likewise defers (canonical assistant.responded).
	require.Equal(t, "PostToolUseFailure", telemetryHookEventName(withRaw("opencode", "tool.failed", "message.part.updated")))
	require.Equal(t, "session.updated", telemetryHookEventName(withRaw("opencode", "session.updated", "message.part.updated")))
	require.Equal(t, "AfterAgentResponse", telemetryHookEventName(withRaw("opencode", "assistant.responded", "session.idle")))

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

type recordingRiskFindingInserter struct {
	rows []chrepo.RiskFindingRow
}

func (r *recordingRiskFindingInserter) InsertRiskFindings(_ context.Context, rows []chrepo.RiskFindingRow) error {
	r.rows = rows
	return nil
}

// TestCanonicalSessionMetadata_AttributesCodexAdapter: a Codex session
// delivered only through the relay (Ingest is its sole path) is attributed at
// ingest — provider openai, email-based classification, and the org-level
// billing mode from the codex_compliance config for team sessions (DNO-734).
func TestCanonicalSessionMetadata_AttributesCodexAdapter(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	seedCodexBillingConfig(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "flat_rate")

	payload := canonicalIngestPayload("codex", "tool.requested", "codex-ingest-attribution")
	metadata := ti.service.canonicalSessionMetadata(ctx, payload, authCtx, canonicalActor{UserID: "user-123", Email: "dev@example.com"})

	require.Equal(t, providerOpenAI, metadata.Provider)
	require.Equal(t, accountTypeTeam, metadata.AccountType)
	require.Equal(t, "flat_rate", metadata.BillingMode)
}

// TestCanonicalSessionMetadata_CodexAdapterUnresolvedActorIsPersonal: an actor
// whose email did not resolve to an org member classifies personal and does
// not inherit the company's billing mode. Claude adapters remain untouched —
// their attribution belongs to the OTEL path.
func TestCanonicalSessionMetadata_CodexAdapterUnresolvedActorIsPersonal(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	seedCodexBillingConfig(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "flat_rate")

	payload := canonicalIngestPayload("Codex", "prompt.submitted", "codex-ingest-personal")
	metadata := ti.service.canonicalSessionMetadata(ctx, payload, authCtx, canonicalActor{UserID: "", Email: "someone@personal.example"})

	require.Equal(t, providerOpenAI, metadata.Provider)
	require.Equal(t, accountTypePersonal, metadata.AccountType)
	require.Empty(t, metadata.BillingMode)

	claudeMetadata := ti.service.canonicalSessionMetadata(ctx, canonicalIngestPayload("claude-code", "prompt.submitted", "claude-ingest-untouched"), authCtx, canonicalActor{UserID: "", Email: ""})
	require.Empty(t, claudeMetadata.Provider)
	require.Empty(t, claudeMetadata.AccountType)
}

// TestCanonicalSessionMetadata_CodexReattributesOnNewActorEmail: a cached
// classification is only adopted when this event's actor email is the one it
// was computed from — the same identity rule the legacy-hook and OTEL paths
// apply — so a different resolved actor on the same session re-classifies
// instead of inheriting the prior attribution. Re-attribution is observable
// through the billing mode: the cache carries flat_rate, but no config exists
// in this test, so a fresh resolution must come back empty.
func TestCanonicalSessionMetadata_CodexReattributesOnNewActorEmail(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	newActorID := "codex-ingest-new-actor"
	newActorEmail := "new-actor@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, newActorID, newActorEmail)

	sessionID := "codex-ingest-actor-change"
	require.NoError(t, ti.service.cache.Set(ctx, sessionCacheKey(sessionID), SessionMetadata{
		SessionID:         sessionID,
		ServiceName:       "codex",
		UserEmail:         "teammate@example.com",
		UserID:            "teammate-user-id",
		Provider:          providerOpenAI,
		AccountType:       accountTypeTeam,
		BillingMode:       "flat_rate",
		ObservedUserEmail: "teammate@example.com",
		GramOrgID:         authCtx.ActiveOrganizationID,
		ProjectID:         authCtx.ProjectID.String(),
	}, 0))

	payload := canonicalIngestPayload("codex", "tool.requested", sessionID)
	metadata := ti.service.canonicalSessionMetadata(ctx, payload, authCtx, canonicalActor{UserID: newActorID, Email: newActorEmail})

	require.Equal(t, providerOpenAI, metadata.Provider)
	require.Equal(t, accountTypeTeam, metadata.AccountType)
	// The cached flat_rate must NOT survive: a fresh resolution ran and found
	// no config declaration.
	require.Empty(t, metadata.BillingMode)
	require.Equal(t, newActorEmail, metadata.ObservedUserEmail)
}

// TestCanonicalSessionMetadata_CodexReattributionDropsStaleCachedUserID: when
// the actor email changes to one that resolves to no org member, the
// re-classification must not run with the PRIOR actor's UserID (the session
// identity fallback fills UserID from the cache independently of the email).
// A stale id would classify the unresolved email team and unlock the
// team-gated org billing mode; the fresh actor must come back personal with
// no billing mode even though a flat_rate declaration exists.
func TestCanonicalSessionMetadata_CodexReattributionDropsStaleCachedUserID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	seedCodexBillingConfig(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "flat_rate")

	sessionID := "codex-ingest-stale-user-id"
	require.NoError(t, ti.service.cache.Set(ctx, sessionCacheKey(sessionID), SessionMetadata{
		SessionID:         sessionID,
		ServiceName:       "codex",
		UserEmail:         "teammate@example.com",
		UserID:            "teammate-user-id",
		Provider:          providerOpenAI,
		AccountType:       accountTypeTeam,
		BillingMode:       "flat_rate",
		ObservedUserEmail: "teammate@example.com",
		GramOrgID:         authCtx.ActiveOrganizationID,
		ProjectID:         authCtx.ProjectID.String(),
	}, 0))

	payload := canonicalIngestPayload("codex", "tool.requested", sessionID)
	metadata := ti.service.canonicalSessionMetadata(ctx, payload, authCtx, canonicalActor{UserID: "", Email: "stranger@personal.example"})

	require.Equal(t, providerOpenAI, metadata.Provider)
	require.Empty(t, metadata.UserID, "cached teammate id must not survive the identity change")
	require.Equal(t, accountTypePersonal, metadata.AccountType)
	require.Empty(t, metadata.BillingMode)
	require.Equal(t, "stranger@personal.example", metadata.ObservedUserEmail)
}

// TestIngest_ShadowMCPGuardCoversCodexMetaTools: Codex's built-in MCP resource
// tools carry no mcp__ prefix and their target lives in tool_input.server, so
// neither arm of the gate (resolved MCP data, MCP-shaped tool name) recognizes
// them. Before DNO-767 they were classified as ordinary tool calls and a
// block_all shadow-MCP policy never ran, letting a Codex session read any MCP
// server's resources. A meta-tool whose server cannot be read must still deny —
// an unproven target is not an absent one.
func TestIngest_ShadowMCPGuardCoversCodexMetaTools(t *testing.T) {
	t.Parallel()

	for _, toolName := range []string{"list_mcp_resources", "list_mcp_resource_templates", "read_mcp_resource"} {
		for name, toolInput := range map[string]any{
			"named server":      map[string]any{"server": "platform-logs"},
			"missing server":    map[string]any{},
			"blank server":      map[string]any{"server": "  "},
			"non-string server": map[string]any{"server": 42},
			"nil input":         nil,
		} {
			t.Run(toolName+"/"+name, func(t *testing.T) {
				t.Parallel()
				ctx, ti := newTestHooksService(t)
				ti.service.riskScanner = stubBlockingShadowMCPScanner{}

				payload := canonicalIngestPayload("codex", "tool.requested", "codex-meta-"+toolName+"-"+name)
				callID := "call-1"
				payload.Data = &gen.HookIngestData{
					ToolCall: &gen.HookToolCallData{
						ID:    &callID,
						Name:  &toolName,
						Input: toolInput,
					},
					// The sender read the list and found no servers, so an
					// empty inventory is proof of absence here.
					McpInventoryCollected: new(true),
				}

				result, err := ti.service.Ingest(ctx, payload)
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "deny", result.Decision,
					"a codex MCP meta-tool must be evaluated by the shadow-MCP policy")
			})
		}
	}
}

// TestIngest_ShadowMCPGuardIgnoresMetaToolNamesFromOtherAdapters: the meta-tool
// names are Codex's, so an unrelated tool of the same name on another agent
// must not be reclassified as an MCP call.
func TestIngest_ShadowMCPGuardIgnoresMetaToolNamesFromOtherAdapters(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}

	toolName := "read_mcp_resource"
	callID := "call-1"
	payload := canonicalIngestPayload("custom-adapter", "tool.requested", "non-codex-meta-tool")
	payload.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID:    &callID,
			Name:  &toolName,
			Input: map[string]any{"server": "platform-logs"},
		},
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEqual(t, "deny", result.Decision)
}

// TestIngest_ShadowMCPResolvesCodexMetaToolAgainstInventory: bringing the
// meta-tools under the guard must not blanket-deny them. The guard can only
// reach its generic "not Gram-hosted" deny without a URL, so a meta-tool
// reading resources from a Gram-hosted server would be blocked — traffic the
// legacy endpoint permits. Resolving the name against the session inventory is
// what separates allowed from denied, and it must name the server when denying.
func TestIngest_ShadowMCPResolvesCodexMetaToolAgainstInventory(t *testing.T) {
	t.Parallel()

	gramHosted := MCPServerEntry{
		Source: "codex", Name: "speakeasy-team",
		URL:    "https://app.getgram.ai/mcp/speakeasy-team-8g3az",
		Status: "unknown",
	}
	external := MCPServerEntry{
		Source: "codex", Name: "someone-else",
		URL:    "https://mcp.example.test/mcp",
		Status: "unknown",
	}

	tests := []struct {
		name       string
		entry      MCPServerEntry
		target     string
		wantDenied bool
	}{
		{"gram-hosted target is allowed", gramHosted, "speakeasy-team", false},
		{"external target is denied", external, "someone-else", true},
		{"target absent from inventory is denied", gramHosted, "unlisted", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestHooksService(t)
			ti.service.riskScanner = stubBlockingShadowMCPScanner{}

			sessionID := "codex-meta-inventory-" + tc.name
			require.NoError(t, ti.service.cache.Set(ctx,
				sessionMCPListCacheKey(sessionID), []MCPServerEntry{tc.entry}, sessionMCPListTTL))

			toolName := "read_mcp_resource"
			callID := "call-1"
			payload := canonicalIngestPayload("codex", "tool.requested", sessionID)
			payload.Data = &gen.HookIngestData{
				ToolCall: &gen.HookToolCallData{
					ID: &callID, Name: &toolName,
					Input: map[string]any{"server": tc.target},
				},
				McpInventoryCollected: new(true),
			}

			result, err := ti.service.Ingest(ctx, payload)
			require.NoError(t, err)
			require.NotNil(t, result)
			if !tc.wantDenied {
				require.NotEqual(t, "deny", result.Decision,
					"a Gram-hosted meta-tool target must not be blocked")
				return
			}
			require.Equal(t, "deny", result.Decision)
		})
	}
}

// TestCanonicalMCPInventoryEntriesCarryCodexToolPrefix: Codex addresses a
// server by its sanitized tool prefix as well as its configured name, and the
// cached-entry fallback matches only on ToolPrefix. Without it a hyphenated
// server is unresolvable on the ingest path while the legacy endpoint resolves
// it, so a Gram-hosted target would be denied.
func TestCanonicalMCPInventoryEntriesCarryCodexToolPrefix(t *testing.T) {
	t.Parallel()

	name := "platform-logs"
	url := "https://app.getgram.ai/mcp/platform-logs"
	payload := canonicalIngestPayload("codex", "session.started", "codex-tool-prefix")
	payload.Data = &gen.HookIngestData{
		McpInventory: []*gen.HookMCPData{{ServerName: &name, URL: &url}},
	}

	entries := canonicalMCPInventoryEntries(payload)
	require.Len(t, entries, 1)
	require.Equal(t, "platform_logs", entries[0].ToolPrefix)

	// The sanitized form must resolve to the configured server, as it does on
	// the legacy endpoint.
	matched := matchCodexCachedMCPServerEntry(entries, "platform_logs")
	require.NotNil(t, matched)
	require.Equal(t, url, matched.URL)

	// Other adapters keep no codex-specific prefix.
	payload.Source.Adapter = "claude"
	require.Empty(t, canonicalMCPInventoryEntries(payload)[0].ToolPrefix)
}

func TestIngestStoresExplicitEmptyMCPInventory(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	sessionID := uuid.NewString()
	stale := []MCPServerEntry{{Name: "stale-server", URL: "https://stale.example.test/mcp"}}
	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(sessionID), stale, sessionMCPListTTL))

	payload := canonicalIngestPayload("claude", "mcp.inventory", sessionID)
	payload.Data = &gen.HookIngestData{
		McpInventory:          []*gen.HookMCPData{},
		McpInventoryCollected: new(true),
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)

	entries, err := ti.service.getCachedMCPList(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, entries)
	require.Empty(t, entries)
	require.True(t, ti.service.canonicalClientReportsMCPInventory(ctx, payload))
}

func TestIngestPartialMCPInventoryWithoutAuthoritativeSnapshot(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}
	sessionID := uuid.NewString()
	name := "partial-server"
	url := "https://mcp.example.test/partial"
	payload := canonicalIngestPayload("codex", "mcp.inventory", sessionID)
	payload.Data = &gen.HookIngestData{
		McpInventory:          []*gen.HookMCPData{{ServerName: &name, URL: &url}},
		McpInventoryCollected: new(false),
	}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	_, err = ti.service.getCachedMCPList(ctx, sessionID)
	require.Error(t, err)
	require.False(t, ti.service.canonicalClientReportsMCPInventory(ctx, payload))

	toolName := "read_mcp_resource"
	callID := "call-1"
	call := canonicalIngestPayload("codex", "tool.requested", sessionID)
	call.Data = &gen.HookIngestData{ToolCall: &gen.HookToolCallData{
		ID: &callID, Name: &toolName, Input: map[string]any{"server": name},
	}}
	result, err = ti.service.Ingest(ctx, call)
	require.NoError(t, err)
	require.NotEqual(t, "deny", result.Decision,
		"a partial inventory must not enable enforcement without complete evidence")
}

func TestIngestPartialMCPInventoryPreservesAuthoritativeSnapshot(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}
	sessionID := uuid.NewString()
	name := "speakeasy-team"
	hostedURL := "https://app.getgram.ai/mcp/speakeasy-team"
	want := []MCPServerEntry{{Source: "codex", Name: name, URL: hostedURL, Status: "unknown", ToolPrefix: "speakeasy_team"}}
	complete := canonicalIngestPayload("codex", "mcp.inventory", sessionID)
	complete.Data = &gen.HookIngestData{
		McpInventory:          []*gen.HookMCPData{{ServerName: &name, URL: &hostedURL}},
		McpInventoryCollected: new(true),
	}
	_, err := ti.service.Ingest(ctx, complete)
	require.NoError(t, err)

	partialURL := "https://mcp.example.test/partial"
	partial := canonicalIngestPayload("codex", "mcp.inventory", sessionID)
	partial.Data = &gen.HookIngestData{
		McpInventory:          []*gen.HookMCPData{{ServerName: &name, URL: &partialURL}},
		McpInventoryCollected: new(false),
	}
	_, err = ti.service.Ingest(ctx, partial)
	require.NoError(t, err)

	entries, err := ti.service.getCachedMCPList(ctx, sessionID)
	require.NoError(t, err)
	require.Equal(t, want, entries)
	require.True(t, ti.service.canonicalClientReportsMCPInventory(ctx, partial))

	toolName := "read_mcp_resource"
	callID := "call-1"
	call := canonicalIngestPayload("codex", "tool.requested", sessionID)
	call.Data = &gen.HookIngestData{ToolCall: &gen.HookToolCallData{
		ID: &callID, Name: &toolName, Input: map[string]any{"server": name},
	}}
	result, err := ti.service.Ingest(ctx, call)
	require.NoError(t, err)
	require.NotEqual(t, "deny", result.Decision,
		"a partial inventory must not replace the complete Gram-hosted target")
}

func TestIngestStoresCollectedEmptyMCPInventory(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	sessionID := uuid.NewString()
	stale := []MCPServerEntry{{Name: "stale-server", URL: "https://stale.example.test/mcp"}}
	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(sessionID), stale, sessionMCPListTTL))

	payload := canonicalIngestPayload("claude", "session.updated", sessionID)
	payload.Data = &gen.HookIngestData{McpInventoryCollected: new(true)}
	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)

	entries, err := ti.service.getCachedMCPList(ctx, sessionID)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestIngestPreservesMCPInventoryForUnrelatedExplicitEmptyEvent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	sessionID := uuid.NewString()
	name := "current-server"
	url := "https://current.example.test/mcp"
	want := []MCPServerEntry{{Source: "claude", Name: name, URL: url, Status: "unknown"}}
	snapshot := canonicalIngestPayload("claude", "session.started", sessionID)
	snapshot.Data = &gen.HookIngestData{
		McpInventory:          []*gen.HookMCPData{{ServerName: &name, URL: &url}},
		McpInventoryCollected: new(true),
	}
	result, err := ti.service.Ingest(ctx, snapshot)
	require.NoError(t, err)
	require.NotNil(t, result)

	payload := canonicalIngestPayload("claude", "session.updated", sessionID)
	payload.Data = &gen.HookIngestData{McpInventory: []*gen.HookMCPData{}}
	result, err = ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result)

	entries, err := ti.service.getCachedMCPList(ctx, sessionID)
	require.NoError(t, err)
	require.Equal(t, want, entries)
	require.True(t, ti.service.canonicalClientReportsMCPInventory(ctx, payload))
}

// TestIngest_ShadowMCPMetaToolGateDegradesWithoutAReadInventory: the guard
// denies a meta-tool call it cannot clear against an inventory, so an empty
// inventory only justifies a deny when the sender actually read the list. A
// sender that could not read it — no agent binary, a failed probe — reports
// mcp_inventory_collected false, and every relay predating the flag omits it
// entirely. Enforcing on either would deny reads of Gram-hosted servers that
// work today (DNO-771).
func TestIngest_ShadowMCPMetaToolGateDegradesWithoutAReadInventory(t *testing.T) {
	t.Parallel()

	for name, collected := range map[string]*bool{
		"flag absent: a relay predating it":   nil,
		"flag false: the list was unreadable": new(false),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestHooksService(t)
			ti.service.riskScanner = stubBlockingShadowMCPScanner{}

			toolName := "read_mcp_resource"
			callID := "call-1"
			payload := canonicalIngestPayload("codex", "tool.requested", "codex-unread-inventory-"+name)
			payload.Data = &gen.HookIngestData{
				ToolCall: &gen.HookToolCallData{
					ID: &callID, Name: &toolName,
					Input: map[string]any{"server": "platform-logs"},
				},
				McpInventoryCollected: collected,
			}

			result, err := ti.service.Ingest(ctx, payload)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotEqual(t, "deny", result.Decision,
				"an inventory that was never read is not evidence the server is absent")
		})
	}
}

// TestIngest_ShadowMCPMetaToolGateReadsSessionState drives the real event
// sequence rather than a hand-built payload: the sender reports whether it read
// the MCP list on session.started, and the meta-tool call it gates arrives
// later as its own tool.requested event carrying no such field. Reading the
// flag off the gating event instead of the session would skip every meta-tool
// call in production while a test that injects the flag into a tool.requested
// payload still passed.
func TestIngest_ShadowMCPMetaToolGateReadsSessionState(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}

	sessionID := "codex-session-state-gate"

	// The ordered inventory event says the sender read the list and found no
	// servers before the related tool request arrives.
	inventory := canonicalIngestPayload("codex", "mcp.inventory", sessionID)
	inventory.Data = &gen.HookIngestData{
		McpInventory:          []*gen.HookMCPData{},
		McpInventoryCollected: new(true),
	}
	_, err := ti.service.Ingest(ctx, inventory)
	require.NoError(t, err)

	// tool.requested: a meta-tool call, with no inventory fields of its own.
	toolName := "read_mcp_resource"
	callID := "call-1"
	call := canonicalIngestPayload("codex", "tool.requested", sessionID)
	call.Data = &gen.HookIngestData{
		ToolCall: &gen.HookToolCallData{
			ID: &callID, Name: &toolName,
			Input: map[string]any{"server": "platform-logs"},
		},
	}

	result, err := ti.service.Ingest(ctx, call)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "deny", result.Decision,
		"the session reported a successful read, so the guard must enforce on its meta-tool calls")
}

// Canonical events carry the session working directory (hook.ingest.v1
// session.cwd); the chat row must persist it so session portability can
// materialize a moved session into the right project directory. Later events
// without a cwd must never null out a previously recorded one.
func TestIngest_PersistsSessionCwd(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	// Chat rows are only written when session capture is enabled for the org.
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := "canonical-session-cwd"
	chatID := sessionIDToUUID(sessionID)
	cwd := "/Users/test/code/api"

	prompt := "add a --verbose flag"
	payload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	payload.Session.Cwd = &cwd
	payload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}
	res, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "allow", res.Decision)

	chat, err := chatRepo.New(ti.conn).GetChat(ctx, chatRepo.GetChatParams{ID: chatID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.True(t, chat.Cwd.Valid, "chat must persist the session cwd")
	require.Equal(t, cwd, chat.Cwd.String)

	// An ordinary follow-up event finds the chat already there and never
	// reaches the upsert, so drive the conflict directly: a racing insert for
	// the same chat carrying no cwd must not null out the recorded one.
	_, err = repo.New(ti.conn).UpsertClaudeCodeSession(ctx, repo.UpsertClaudeCodeSessionParams{
		ID:             chatID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         conv.ToPGTextEmpty(""),
		ExternalUserID: conv.ToPGTextEmpty("employee@example.com"),
		UserAccountID:  conv.StringToNullUUID(""),
		Title:          conv.ToPGText("racing insert"),
		Cwd:            conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)

	chat, err = chatRepo.New(ti.conn).GetChat(ctx, chatRepo.GetChatParams{ID: chatID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.True(t, chat.Cwd.Valid, "a later write without a cwd must not erase the recorded one")
	require.Equal(t, cwd, chat.Cwd.String)
}
