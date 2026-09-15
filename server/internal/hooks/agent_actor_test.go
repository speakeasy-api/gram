package hooks

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"
	"goa.design/goa/v3/security"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// agentAuthContext clones the human test auth context into an agent-shaped one.
func agentAuthContext(t *testing.T, ctx context.Context, ti *testInstance) (*contextvalues.AuthContext, urn.Principal, string) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID, OwnerUserID: authCtx.UserID, Name: "Hooks agent",
	})
	require.NoError(t, err)

	clone := *authCtx
	clone.UserID = ""
	clone.Email = nil
	clone.APIKeyScopes = nil
	clone.SessionID = nil
	return &clone, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), authCtx.UserID
}

// agentKeyContext makes ctx look like an admitted agent-principal API key.
func agentKeyContext(t *testing.T, ctx context.Context, ti *testInstance) context.Context {
	t.Helper()
	authCtx, actor, ownerUserID := agentAuthContext(t, ctx, ti)
	credential := contextvalues.PrincipalCredential{AuthorizerUserID: ownerUserID, DelegatedGrants: nil, DelegatedGrantsVersion: 0}
	return contextvalues.WithPrincipalAPIKeyAuthorization(ctx, authCtx, actor, credential)
}

func seedHooksIngestGrant(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, principal urn.Principal) {
	t.Helper()
	selectors, err := authz.NewSelector(authz.ScopeOrgHooksIngest, organizationID).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   principal,
		Scope:          string(authz.ScopeOrgHooksIngest),
		Selectors:      selectors,
	})
	require.NoError(t, err)
}

// admittedAgentContext runs real credential admission for an agent whose
// delegated policy grants org:hooks_ingest on policyOrgID.
func admittedAgentContext(t *testing.T, ctx context.Context, ti *testInstance, grantAgent bool, policyOrgID string) (context.Context, error) {
	t.Helper()
	authCtx, actor, ownerUserID := agentAuthContext(t, ctx, ti)
	if grantAgent {
		seedHooksIngestGrant(t, ctx, ti, authCtx.ActiveOrganizationID, actor)
	}
	seedHooksIngestGrant(t, ctx, ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, ownerUserID))

	policy, err := runtimepolicy.NewDelegatedPolicy(runtimepolicy.DelegatedPolicyVersion2, []authz.Grant{authz.NewGrant(authz.ScopeOrgHooksIngest, policyOrgID)})
	require.NoError(t, err)
	raw, err := runtimepolicy.EncodeDelegatedPolicy(runtimepolicy.DelegatedPolicyVersion2, policy)
	require.NoError(t, err)

	ti.service.authz = authz.NewEngine(testenv.NewLogger(t), ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), authz.EngineOpts{
		DevMode:                          false,
		AdmitPrincipalCredential:         runtimepolicy.AdmitPrincipalCredential,
		AdmitPrincipalCredentialWithDBTX: runtimepolicy.AdmitPrincipalCredentialWithDBTX,
	})
	requestCtx := contextvalues.WithPrincipalCredentialAuthorization(t.Context(), authCtx, actor, contextvalues.PrincipalCredential{
		AuthorizerUserID:       ownerUserID,
		DelegatedGrants:        raw,
		DelegatedGrantsVersion: int32(runtimepolicy.DelegatedPolicyVersion2),
	})
	prepared, err := ti.service.authz.PrepareContext(requestCtx)
	if err != nil {
		return requestCtx, fmt.Errorf("admit agent credential: %w", err)
	}
	return prepared, nil
}

func TestRequireAgentHooksIngest_AllowsGrantedAgent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	agentCtx, err := admittedAgentContext(t, ctx, ti, true, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.NoError(t, ti.service.requireAgentHooksIngest(agentCtx))
}

func TestRequireAgentHooksIngest_RejectsAgentWithoutGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	agentCtx, err := admittedAgentContext(t, ctx, ti, false, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, ti.service.requireAgentHooksIngest(agentCtx), &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestRequireAgentHooksIngest_RejectsGrantOnAnotherOrg(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	agentCtx, err := admittedAgentContext(t, ctx, ti, true, "org-other-"+uuid.NewString())
	if err == nil {
		err = ti.service.requireAgentHooksIngest(agentCtx)
	}
	require.Error(t, err)
}

func TestRequireAgentHooksIngest_HumanUnaffected(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	require.NoError(t, ti.service.requireAgentHooksIngest(authztest.WithExactGrants(t, ctx)))
}

func TestResolveCanonicalActor_AgentIgnoresSelfReportedEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	humanID := "user-" + uuid.NewString()
	humanEmail := humanID + "@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, humanID, humanEmail)
	payload := canonicalIngestPayload("claude", "prompt.submitted", "agent-actor-"+uuid.NewString())
	payload.Source.UserEmail = &humanEmail

	require.Equal(t, humanID, ti.service.resolveCanonicalActor(ctx, payload, authCtx).UserID)

	agentCtx := agentKeyContext(t, ctx, ti)
	agentAuth, ok := contextvalues.GetAuthContext(agentCtx)
	require.True(t, ok)
	require.Equal(t, canonicalActor{UserID: "", Email: ""}, ti.service.resolveCanonicalActor(agentCtx, payload, agentAuth))
}

func TestResolveClaudeSessionMetadata_AgentIgnoresCachedHuman(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := "agent-claude-" + uuid.NewString()
	humanEmail := "cached-human@example.com"
	require.NoError(t, ti.service.cache.Set(ctx, sessionCacheKey(sessionID), SessionMetadata{
		SessionID:           sessionID,
		ServiceName:         "claude-code",
		UserEmail:           humanEmail,
		UserID:              authCtx.UserID,
		Provider:            providerAnthropic,
		ExternalOrgID:       "",
		ExternalAccountUUID: "",
		ExternalAccountID:   "",
		DeviceID:            "",
		Hostname:            "",
		Cwd:                 "",
		AccountType:         "",
		BillingMode:         "",
		UserAccountID:       "",
		ObservedUserEmail:   humanEmail,
		GramOrgID:           authCtx.ActiveOrganizationID,
		ProjectID:           authCtx.ProjectID.String(),
	}, time.Hour))

	metadata, err := ti.service.resolveClaudeSessionMetadata(agentKeyContext(t, ctx, ti), sessionID, humanEmail)
	require.NoError(t, err)
	require.Empty(t, metadata.UserEmail)
	require.Empty(t, metadata.UserID)
}

func TestIngest_AgentKeyPersistsChatWithoutHumanIdentity(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	humanID := "user-" + uuid.NewString()
	humanEmail := humanID + "@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, humanID, humanEmail)

	sessionID := "agent-ingest-" + uuid.NewString()
	prompt := "agent prompt " + uuid.NewString()
	payload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	payload.Source.UserEmail = &humanEmail
	payload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}

	_, err := ti.service.Ingest(agentKeyContext(t, ctx, ti), payload)
	require.NoError(t, err)

	chat, err := chatRepo.New(ti.conn).GetChat(t.Context(), chatRepo.GetChatParams{ID: sessionIDToUUID(sessionID), ProjectID: *authCtx.ProjectID})
	require.NoError(t, err, "agent sessions are stored, not skipped")
	require.False(t, chat.UserID.Valid, "a self-reported email never attributes an agent session to a human")
	require.False(t, chat.ExternalUserID.Valid)

	messages, err := chatRepo.New(ti.conn).ListChatMessages(t.Context(), chatRepo.ListChatMessagesParams{ChatID: sessionIDToUUID(sessionID), ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 1)
}

func TestCursor_AgentKeyAcceptedWithoutEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	toolName := "Shell"
	toolUseID := "tool-" + uuid.NewString()
	payload := &gen.CursorPayload{HookEventName: "postToolUse", ToolName: &toolName, ToolUseID: &toolUseID}

	_, err := ti.service.Cursor(ctx, payload)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr, "human requests still require user_email")
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)

	_, err = ti.service.Cursor(agentKeyContext(t, ctx, ti), payload)
	require.NoError(t, err)
}

func TestCodex_AgentKeyAcceptedWithoutEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	sessionID := "agent-codex-" + uuid.NewString()

	_, err := ti.service.Codex(ctx, &gen.CodexPayload{HookEventName: "Stop", SessionID: &sessionID})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr, "human requests still require user_email")
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)

	agentCtx := agentKeyContext(t, ctx, ti)
	_, err = ti.service.Codex(agentCtx, &gen.CodexPayload{HookEventName: "Stop", SessionID: &sessionID})
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(agentCtx)
	require.True(t, ok)
	humanEmail := "codex-human@example.com"
	metadata := ti.service.codexSessionMetadata(agentCtx, &gen.CodexPayload{HookEventName: "Stop", SessionID: &sessionID, UserEmail: &humanEmail}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())
	require.Empty(t, metadata.UserEmail)
	require.Empty(t, metadata.UserID)
}

func TestWithAgentActor_StripsSpoofedActorOnHumanRequests(t *testing.T) {
	t.Parallel()
	attrs := map[attr.Key]any{
		attr.AuthorizationActorTypeKey: "agent",
		attr.AuthorizationActorIDKey:   "spoofed",
	}
	withAgentActor(t.Context(), attrs)
	require.NotContains(t, attrs, attr.AuthorizationActorTypeKey)
	require.NotContains(t, attrs, attr.AuthorizationActorIDKey)
}

func TestAgentSessionView_RejectsOtherTenant(t *testing.T) {
	t.Parallel()
	cached := SessionMetadata{
		SessionID: "s", ServiceName: "cowork", UserEmail: "h@example.com", UserID: "u", Provider: providerAnthropic,
		ExternalOrgID: "eo", ExternalAccountUUID: "ea", ExternalAccountID: "eid", DeviceID: "d", Hostname: "host", Cwd: "/w",
		AccountType: accountTypePersonal, BillingMode: "metered", UserAccountID: "ua", ObservedUserEmail: "h@example.com",
		GramOrgID: "org-a", ProjectID: "proj-a",
	}

	same := agentSessionView(cached, "org-a", "proj-a")
	require.Equal(t, "host", same.Hostname)
	require.Empty(t, same.UserEmail)
	require.Empty(t, same.UserAccountID)
	require.Empty(t, same.DeviceID)

	other := agentSessionView(cached, "org-b", "proj-b")
	require.Empty(t, other.Hostname, "another tenant's cache entry contributes nothing")
	require.Empty(t, other.ServiceName)
	require.Equal(t, "org-b", other.GramOrgID)
}

func TestCodexOTELUserInfo_AgentDropsSelfReportedEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	attrs := map[attr.Key]any{attr.UserEmailKey: "codex-human@example.com"}
	_, email, userID := ti.service.codexOTELUserInfo(agentKeyContext(t, ctx, ti), attrs, map[string]string{}, authCtx.ActiveOrganizationID)
	require.Empty(t, email)
	require.Empty(t, userID)
}

func TestWithAgentActor_StripsSelfReportedIdentityForAgentsOnly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	identity := func() map[attr.Key]any {
		attrs := make(map[attr.Key]any, len(selfReportedIdentityKeys))
		for _, key := range selfReportedIdentityKeys {
			attrs[key] = "payload-" + string(key)
		}
		return attrs
	}

	agentAttrs := withAgentActor(agentKeyContext(t, ctx, ti), identity())
	humanAttrs := withAgentActor(ctx, identity())
	for _, key := range selfReportedIdentityKeys {
		require.NotContains(t, agentAttrs, key, "agent rows drop %s", key)
		require.Equal(t, "payload-"+string(key), humanAttrs[key], "human rows keep %s", key)
	}
	require.Equal(t, "agent", agentAttrs[attr.AuthorizationActorTypeKey])
}

// stubAuthorizer returns the same result for every scheme.
type stubAuthorizer func() (context.Context, error)

func (a stubAuthorizer) Authorize(context.Context, string, *security.APIKeyScheme) (context.Context, error) {
	return a()
}

func claudePluginPrompt(sessionID string) *gen.ClaudePayload {
	key := "gram_local_test"
	slug := "project"
	prompt := "agent prompt"
	return &gen.ClaudePayload{HookEventName: "UserPromptSubmit", SessionID: &sessionID, ApikeyToken: &key, ProjectSlugInput: &slug, Prompt: &prompt}
}

func requireNothingRecorded(t *testing.T, ctx context.Context, ti *testInstance, sessionID string, projectID uuid.UUID) {
	t.Helper()
	var buffered []gen.ClaudePayload
	require.NoError(t, ti.service.cache.ListRange(ctx, hookPendingCacheKey(sessionID), 0, -1, &buffered))
	require.Empty(t, buffered, "a denied agent event is never buffered for OTEL attribution")
	_, err := chatRepo.New(ti.conn).GetChat(ctx, chatRepo.GetChatParams{ID: sessionIDToUUID(sessionID), ProjectID: projectID})
	require.Error(t, err, "a denied agent event is never persisted")
}

func TestClaude_UngrantedAgentKeyRejected(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	deniedCtx, err := admittedAgentContext(t, ctx, ti, false, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	ti.service.auth = stubAuthorizer(func() (context.Context, error) { return deniedCtx, nil })

	sessionID := "agent-denied-" + uuid.NewString()
	_, err = ti.service.Claude(t.Context(), claudePluginPrompt(sessionID))
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	requireNothingRecorded(t, ctx, ti, sessionID, *authCtx.ProjectID)
}

func TestClaude_AgentKeyFailingAdmissionRejected(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	agentCtx := agentKeyContext(t, ctx, ti)
	ti.service.auth = stubAuthorizer(func() (context.Context, error) {
		return agentCtx, errors.New("principal credential admission failed")
	})

	sessionID := "agent-unadmitted-" + uuid.NewString()
	_, err := ti.service.Claude(t.Context(), claudePluginPrompt(sessionID))
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	requireNothingRecorded(t, ctx, ti, sessionID, *authCtx.ProjectID)
}

func TestClaude_InvalidCredentialsStillFallBack(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.auth = stubAuthorizer(func() (context.Context, error) {
		return t.Context(), errors.New("invalid api key")
	})

	sessionID := "invalid-key-" + uuid.NewString()
	_, err := ti.service.Claude(t.Context(), claudePluginPrompt(sessionID))
	require.NoError(t, err, "bad or missing credentials keep the unauthenticated fallback")

	var buffered []gen.ClaudePayload
	require.NoError(t, ti.service.cache.ListRange(ctx, hookPendingCacheKey(sessionID), 0, -1, &buffered))
	require.Len(t, buffered, 1)
}

func TestClearAgentAccountIdentity(t *testing.T) {
	t.Parallel()
	meta := SessionMetadata{
		SessionID: "s", ServiceName: "claude-code", UserEmail: "", UserID: "", Provider: providerAnthropic,
		ExternalOrgID: "eo", ExternalAccountUUID: "ea", ExternalAccountID: "eid", DeviceID: "d", Hostname: "host", Cwd: "/w",
		AccountType: "", BillingMode: "", UserAccountID: "ua", ObservedUserEmail: "h@example.com",
		GramOrgID: "org", ProjectID: "proj",
	}
	clearAgentAccountIdentity(&meta)
	require.Empty(t, meta.ExternalOrgID)
	require.Empty(t, meta.ExternalAccountUUID)
	require.Empty(t, meta.ExternalAccountID)
	require.Empty(t, meta.DeviceID)
	require.Empty(t, meta.UserAccountID)
	require.Empty(t, meta.ObservedUserEmail)
	require.Equal(t, "host", meta.Hostname, "surface fields stay")
}

func teeHasAttr(attrs []*otelv1.InboundLogRecord_KeyValue, key string) bool {
	for _, kv := range attrs {
		if kv.GetKey() == key {
			return true
		}
	}
	return false
}

func TestLogs_AgentEventFeedCopyMatchesSanitizedRows(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	var published []*otelv1.InboundLogRecord
	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		record, ok := args.Get(1).(*otelv1.InboundLogRecord)
		require.True(t, ok)
		published = append(published, record)
	}).Return(gcp.NewSuccessPublishResult())
	ti.service.otelLogPublisher = publisher

	agentCtx := agentKeyContext(t, ctx, ti)
	actor, ok := contextvalues.AuthenticatedActor(agentCtx)
	require.True(t, ok)

	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	err := ti.service.Logs(agentCtx, claudeLogsPayload(
		[]*gen.OTELResourceAttribute{
			resourceStrAttr("service.name", "claude-code"),
			resourceStrAttr("user.email", "resource-human@example.com"),
		},
		&gen.OTELScope{Name: new("claude-code"), Version: new("1.0.0")},
		&gen.OTELLogRecord{
			TimeUnixNano: new(nanoString(timestamp)),
			Body:         &gen.OTELLogBody{StringValue: new("agent request")},
			Attributes: []*gen.OTELAttribute{
				strAttr("session.id", "agent-tee-"+uuid.NewString()),
				strAttr("user.id", "human-id"),
				strAttr("user.email", "human@example.com"),
				strAttr("gram.account_type", "team"),
				strAttr("gram.billing_mode", "metered"),
				strAttr("gram.authorization.actor.id", "spoofed"),
			},
		},
	))
	require.NoError(t, err)
	ti.service.otelTeeDrains.Wait()

	require.Len(t, published, 1)
	record := published[0]
	for _, key := range []string{"user.id", "user.email", "gram.account_type", "gram.billing_mode"} {
		require.False(t, teeHasAttr(record.GetAttributes(), key), "event-feed copy drops %s", key)
	}
	require.False(t, teeHasAttr(record.GetResource().GetAttributes(), "user.email"))
	require.Equal(t, "claude-code", teeAttrByKey(t, record.GetResource().GetAttributes(), "service.name").GetStringValue())
	require.Equal(t, "agent", teeAttrByKey(t, record.GetAttributes(), "gram.authorization.actor.type").GetStringValue())
	require.Equal(t, actor.ID, teeAttrByKey(t, record.GetAttributes(), "gram.authorization.actor.id").GetStringValue())
}

func testMCPEntry(name string) MCPServerEntry {
	return MCPServerEntry{
		RawLine: "", Source: "local", PluginName: "", Name: name, URL: "https://" + name + ".example.test/mcp",
		Command: "", Transport: "HTTP", Status: "connected", StatusRaw: "connected", ConnectorUUID: "", ToolPrefix: name,
	}
}

func TestMCPListSnapshot_BoundToOwner(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	agentCtx := agentKeyContext(t, ctx, ti)

	humanSession := "human-snapshot-" + uuid.NewString()
	ti.service.cacheCanonicalMCPList(ctx, humanSession, []MCPServerEntry{testMCPEntry("human")}, true)

	_, err := ti.service.getCachedMCPList(agentCtx, humanSession)
	require.ErrorIs(t, err, redisCache.ErrCacheMiss, "an agent never reads another actor's snapshot")
	ti.service.cacheCanonicalMCPList(agentCtx, humanSession, []MCPServerEntry{testMCPEntry("agent")}, true)
	humanEntries, err := ti.service.getCachedMCPList(ctx, humanSession)
	require.NoError(t, err)
	require.Equal(t, "human", humanEntries[0].Name, "an agent never overwrites another actor's snapshot")

	agentSession := "agent-snapshot-" + uuid.NewString()
	ti.service.cacheCanonicalMCPList(agentCtx, agentSession, []MCPServerEntry{testMCPEntry("agent")}, true)
	agentEntries, err := ti.service.getCachedMCPList(agentCtx, agentSession)
	require.NoError(t, err, "the owning agent still reads its snapshot")
	require.Equal(t, "agent", agentEntries[0].Name)
	_, err = ti.service.getCachedMCPList(ctx, agentSession)
	require.ErrorIs(t, err, redisCache.ErrCacheMiss, "an agent-owned snapshot is not shared")

	otherOrgSession := "other-org-snapshot-" + uuid.NewString()
	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(otherOrgSession), []MCPServerEntry{testMCPEntry("other")}, time.Hour))
	require.NoError(t, ti.service.cache.Set(ctx, mcpListOwnerCacheKey(otherOrgSession), mcpListOwner{
		OrgID: "org-other-" + uuid.NewString(), ProjectID: uuid.NewString(), Actor: "",
	}, time.Hour))
	_, err = ti.service.getCachedMCPList(ctx, otherOrgSession)
	require.ErrorIs(t, err, redisCache.ErrCacheMiss, "another org's snapshot is ignored")
	require.NotNil(t, authCtx.ProjectID)
}

func TestGetCachedMCPList_OwnerNotFoundIsMiss(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	sessionID := "unowned-snapshot-" + uuid.NewString()
	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(sessionID), []MCPServerEntry{testMCPEntry("legacy")}, time.Hour))

	_, err := ti.service.getCachedMCPList(agentKeyContext(t, ctx, ti), sessionID)
	require.ErrorIs(t, err, redisCache.ErrCacheMiss, "an unowned snapshot is a miss for an agent")
	entries, err := ti.service.getCachedMCPList(ctx, sessionID)
	require.NoError(t, err, "humans keep reading unowned snapshots")
	require.Equal(t, "legacy", entries[0].Name)
}

func TestGetCachedMCPList_OwnerReadErrorFailsClosed(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	agentCtx := agentKeyContext(t, ctx, ti)

	sessionID := "corrupt-owner-" + uuid.NewString()
	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(sessionID), []MCPServerEntry{testMCPEntry("human")}, time.Hour))
	require.NoError(t, ti.service.cache.Set(ctx, mcpListOwnerCacheKey(sessionID), "not-an-owner", time.Hour))

	_, err := ti.service.getCachedMCPList(agentCtx, sessionID)
	require.Error(t, err)
	require.NotErrorIs(t, err, redisCache.ErrCacheMiss, "an owner decode failure is not a miss")

	_, err = ti.service.resolveMCPListForEnforcement(agentCtx, &gen.ClaudePayload{HookEventName: "PreToolUse", SessionID: &sessionID}, sessionID)
	require.Error(t, err, "enforcement fails closed on an owner read failure")
}

// grantedAgentContext admits a real agent credential holding org:hooks_ingest.
func grantedAgentContext(t *testing.T, ctx context.Context, ti *testInstance) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	agentCtx, err := admittedAgentContext(t, ctx, ti, true, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	return agentCtx
}

// requireAgentChat waits for the session's chat and asserts it has no human.
func requireAgentChat(t *testing.T, ti *testInstance, sessionID string, projectID uuid.UUID) {
	t.Helper()
	require.Eventually(t, func() bool {
		chat, err := chatRepo.New(ti.conn).GetChat(t.Context(), chatRepo.GetChatParams{ID: sessionIDToUUID(sessionID), ProjectID: projectID})
		return err == nil && !chat.UserID.Valid && !chat.ExternalUserID.Valid
	}, 2*time.Second, 25*time.Millisecond, "a granted agent's event is stored and attributed to no human")
}

// hooksAPIKeyAuth runs the real hooks APIKeyAuth gate for method with an
// admitted agent credential.
func hooksAPIKeyAuth(t *testing.T, ti *testInstance, agentCtx context.Context, method string) (context.Context, error) {
	t.Helper()
	methodCtx := context.WithValue(agentCtx, goa.MethodKey, method)
	ti.service.auth = stubAuthorizer(func() (context.Context, error) { return methodCtx, nil })
	authed, err := ti.service.APIKeyAuth(t.Context(), "gram_local_test", &security.APIKeyScheme{
		Name: constants.KeySecurityScheme, Scopes: []string{}, RequiredScopes: []string{"hooks"},
	})
	if err != nil {
		return authed, fmt.Errorf("hooks api key auth: %w", err)
	}
	return authed, nil
}

func TestClaude_GrantedAgentKeyAccepted(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	agentCtx := grantedAgentContext(t, ctx, ti)
	ti.service.auth = stubAuthorizer(func() (context.Context, error) { return agentCtx, nil })

	sessionID := "granted-claude-" + uuid.NewString()
	_, err := ti.service.Claude(t.Context(), claudePluginPrompt(sessionID))
	require.NoError(t, err)
	requireAgentChat(t, ti, sessionID, *authCtx.ProjectID)
}

func TestCursor_GrantedAgentKeyAccepted(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	agentCtx := grantedAgentContext(t, ctx, ti)

	_, err := hooksAPIKeyAuth(t, ti, agentCtx, "skillFeedback")
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr, "agent keys only reach ingestion methods")
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	authed, err := hooksAPIKeyAuth(t, ti, agentCtx, "cursor")
	require.NoError(t, err)
	sessionID := "granted-cursor-" + uuid.NewString()
	prompt := "granted cursor prompt"
	_, err = ti.service.Cursor(authed, &gen.CursorPayload{HookEventName: "beforeSubmitPrompt", ConversationID: &sessionID, Prompt: &prompt})
	require.NoError(t, err)
	requireAgentChat(t, ti, sessionID, *authCtx.ProjectID)
}

func TestCodex_GrantedAgentKeyAccepted(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	authed, err := hooksAPIKeyAuth(t, ti, grantedAgentContext(t, ctx, ti), "codex")
	require.NoError(t, err)
	sessionID := "granted-codex-" + uuid.NewString()
	prompt := "granted codex prompt"
	_, err = ti.service.Codex(authed, &gen.CodexPayload{HookEventName: "UserPromptSubmit", SessionID: &sessionID, Prompt: &prompt})
	require.NoError(t, err)
	requireAgentChat(t, ti, sessionID, *authCtx.ProjectID)
}

func TestIngest_GrantedAgentKeyAccepted(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	agentCtx := grantedAgentContext(t, ctx, ti)
	ti.service.auth = stubAuthorizer(func() (context.Context, error) { return agentCtx, nil })

	sessionID := "granted-ingest-" + uuid.NewString()
	key := "gram_local_test"
	slug := "project"
	prompt := "granted ingest prompt"
	payload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	payload.ApikeyToken = &key
	payload.ProjectSlugInput = &slug
	payload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}}

	_, err := ti.service.Ingest(t.Context(), payload)
	require.NoError(t, err)
	requireAgentChat(t, ti, sessionID, *authCtx.ProjectID)
}

func TestResolveUserByEmail_EmptyEmailSkipsLookup(t *testing.T) {
	t.Parallel()
	_, ti := newTestHooksService(t)
	ti.service.db = nil

	require.Empty(t, ti.service.resolveUserByEmail(t.Context(), "", "org-unused"))
}

func TestCanonicalSessionMetadata_AgentIgnoresCachedAccount(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := "agent-cached-account-" + uuid.NewString()
	require.NoError(t, ti.service.cache.Set(ctx, sessionCacheKey(sessionID), SessionMetadata{
		SessionID:           sessionID,
		ServiceName:         "claude",
		UserEmail:           "cached-human@example.com",
		UserID:              authCtx.UserID,
		Provider:            providerAnthropic,
		ExternalOrgID:       "ext-org",
		ExternalAccountUUID: "ext-account-uuid",
		ExternalAccountID:   "ext-account",
		DeviceID:            "device-1",
		Hostname:            "host-1",
		Cwd:                 "/work",
		AccountType:         accountTypePersonal,
		BillingMode:         "metered",
		UserAccountID:       uuid.NewString(),
		ObservedUserEmail:   "cached-human@example.com",
		GramOrgID:           authCtx.ActiveOrganizationID,
		ProjectID:           authCtx.ProjectID.String(),
	}, time.Hour))

	agentCtx := agentKeyContext(t, ctx, ti)
	agentAuth, ok := contextvalues.GetAuthContext(agentCtx)
	require.True(t, ok)
	metadata := ti.service.canonicalSessionMetadata(agentCtx, canonicalIngestPayload("claude", "tool.requested", sessionID), agentAuth, canonicalActor{UserID: "", Email: ""})

	require.Empty(t, metadata.UserEmail)
	require.Empty(t, metadata.UserID)
	require.Empty(t, metadata.UserAccountID)
	require.Empty(t, metadata.DeviceID)
	require.Empty(t, metadata.ExternalAccountUUID)
	require.Empty(t, metadata.AccountType)
	require.Empty(t, metadata.BillingMode)
	require.Empty(t, metadata.ObservedUserEmail)
	require.Equal(t, "host-1", metadata.Hostname, "surface fields still merge")
}

func TestIngest_AgentSessionStartDoesNotSeedSessionCache(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	sessionID := "agent-no-cache-" + uuid.NewString()
	hostname := "agent-host"
	payload := canonicalIngestPayload("claude", "session.started", sessionID)
	payload.Source.Hostname = &hostname

	_, err := ti.service.Ingest(agentKeyContext(t, ctx, ti), payload)
	require.NoError(t, err)

	var cached SessionMetadata
	require.Error(t, ti.service.cache.Get(ctx, sessionCacheKey(sessionID), &cached), "agent sessions never seed the human-keyed cache")
}
