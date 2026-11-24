package mcp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authenforcement"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/executionprofiles"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestAssistantProfileSelectionsStayPinnedAndFailClosedPerServer(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "assistant_profile_resolution")
	require.NoError(t, err)
	ctx := t.Context()
	f := newAuthClientFixture(t, pool)
	q := executionprofiles.New(pool)
	assistantID, err := q.SeedAssistantFixture(ctx, executionprofiles.SeedAssistantFixtureParams{OrganizationID: f.orgID, ProjectID: f.projectID, Name: "assistant"})
	require.NoError(t, err)
	triggerID, err := q.SeedTriggerFixture(ctx, executionprofiles.SeedTriggerFixtureParams{OrganizationID: f.orgID, ProjectID: f.projectID, Name: "trigger"})
	require.NoError(t, err)
	openServer, _ := f.server()
	for _, serverID := range []uuid.UUID{f.serverID, openServer} {
		require.NoError(t, q.SeedAssistantServerFixture(ctx, executionprofiles.SeedAssistantServerFixtureParams{AssistantID: assistantID, ProjectID: f.projectID, McpServerID: serverID}))
	}
	personal := f.adminClient(authClientTypeRemoteSession, validUUID(f.registration))

	bind := func(execCtx executionprofiles.Context) *upstreamSelector {
		t.Helper()
		binding, err := executionprofiles.Bind(ctx, pool, executionprofiles.Target{
			OrganizationID: f.orgID, ProjectID: f.projectID, AssistantID: assistantID, TriggerID: triggerID, Context: execCtx,
		}, executionprofiles.SourceIssuance)
		require.NoError(t, err)
		selector := f.service.profileSelector(principalcredential.Credential{
			OrganizationID: f.orgID, ProjectID: f.projectID, SelectionProfileVersionID: validUUID(binding.VersionID),
		}, &ResolvedMcpEndpoint{OrganizationID: f.orgID, ProjectID: f.projectID})
		require.NotNil(t, selector)
		return selector
	}
	human := bind(executionprofiles.ContextUserBacked)
	// Check the saved row separately from resolution: no-auth is an explicit
	// choice in the pin, not an inferred fallback when no choice was saved.
	choices, err := q.ListVersionChoices(ctx, executionprofiles.ListVersionChoicesParams{VersionID: human.credentialID, OrganizationID: f.orgID})
	require.NoError(t, err)
	require.Contains(t, choices, executionprofiles.ListVersionChoicesRow{McpServerID: openServer, NoAuth: true, Source: executionprofiles.SourceIssuance})
	userlessGap := bind(executionprofiles.ContextUserless)
	_, err = productfeaturesrepo.New(pool).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{
		OrganizationID: f.orgID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced),
	})
	require.NoError(t, err)
	f.service.authEnforcement = authenforcement.NewGate(pool)
	require.Equal(t, personal, f.resolve(human).clientID)
	require.Equal(t, authClientReasonMissingChoice, f.resolve(userlessGap).reason)

	shared := f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{})
	userless := bind(executionprofiles.ContextUserless)
	require.Equal(t, authClientResolved, f.resolve(userless).outcome)
	require.Equal(t, shared, f.resolve(userless).clientID)
	require.Equal(t, authClientReasonMissingChoice, f.resolve(userlessGap).reason, "a running turn never gains a choice")
	require.Equal(t, personal, f.resolve(human).clientID, "a new offer does not replace a pinned choice")

	require.NoError(t, f.q.SetAuthClientEnabledFixture(ctx, authclientbackfill.SetAuthClientEnabledFixtureParams{ID: personal, Enabled: false}))
	stale := f.resolve(human)
	require.Equal(t, authClientGap, stale.outcome)
	require.Equal(t, authClientReasonClientDisabled, stale.reason, "never substitute another offer for a stale choice")
	open, err := human.resolveAuthClient(ctx, openServer)
	require.NoError(t, err)
	require.Equal(t, authClientNoAuth, open.outcome, "a stale choice does not stop a different server from working")
}

func TestUserBackedAssistantProfileUsesOnlyTheActingUsersConnection(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "assistant_profile_connections")
	require.NoError(t, err)
	f := newEnforcedCallerFixture(t, pool)
	f.service.siteURL = &url.URL{Scheme: "https", Host: "platform.example.invalid"}
	ctx := t.Context()
	client := f.adminClient(authClientTypeRemoteSession, validUUID(f.registration))
	f.service.authz = authz.NewEngine(f.service.logger, pool, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient(), authz.EngineOpts{
		AdmitPrincipalCredential: runtimepolicy.AdmitPrincipalCredential,
	})
	f.service.authClientPermitter = mandatoryPermitter{engine: f.service.authz, visibility: f.service.mandatoryServerVisibility}

	access := accessrepo.New(pool)
	readProject, err := authz.NewSelector(authz.ScopeProjectRead, f.projectID.String()).MarshalJSON()
	require.NoError(t, err)
	useClient, err := (authz.Selector{
		authz.SelectorKeyResourceKind:    authz.ResourceKindMCPAuthClient,
		authz.SelectorKeyResourceID:      client.String(),
		authz.SelectorKeyClientType:      authz.AuthClientTypeRemoteSession,
		authz.SelectorKeyConnectionOwner: authz.SelectorValueRepresentedUser,
	}).MarshalJSON()
	require.NoError(t, err)
	for _, user := range []string{"owner", "alice", "bob"} {
		_, err := usersrepo.New(pool).UpsertUser(ctx, usersrepo.UpsertUserParams{ID: user, Email: user + "@example.invalid", DisplayName: user})
		require.NoError(t, err)
		_, err = orgrepo.New(pool).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: f.orgID, UserID: pgtype.Text{String: user, Valid: true}})
		require.NoError(t, err)
		principal := urn.NewPrincipal(urn.PrincipalTypeUser, user)
		_, err = access.UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: f.orgID, PrincipalUrn: principal, Scope: string(authz.ScopeProjectRead), Selectors: readProject})
		require.NoError(t, err)
		_, err = access.UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: f.orgID, PrincipalUrn: principal, Scope: string(authz.ScopeMCPAuth), Selectors: useClient})
		require.NoError(t, err)
	}
	agents := agentrepo.New(pool)
	agent, err := agents.CreateAgent(ctx, agentrepo.CreateAgentParams{OrganizationID: f.orgID, ProjectID: validUUID(f.projectID), OwnerUserID: "owner", Name: "Assistant agent"})
	require.NoError(t, err)
	_, err = agents.CreateAgentPolicyGrant(ctx, agentrepo.CreateAgentPolicyGrantParams{OrganizationID: f.orgID, AgentID: agent.ID, Scope: string(authz.ScopeMCPAuth), Selectors: useClient})
	require.NoError(t, err)

	q := executionprofiles.New(pool)
	assistant, err := q.SeedAssistantFixture(ctx, executionprofiles.SeedAssistantFixtureParams{OrganizationID: f.orgID, ProjectID: f.projectID, Name: "assistant"})
	require.NoError(t, err)
	trigger, err := q.SeedTriggerFixture(ctx, executionprofiles.SeedTriggerFixtureParams{OrganizationID: f.orgID, ProjectID: f.projectID, Name: "trigger"})
	require.NoError(t, err)
	require.NoError(t, q.SeedAssistantServerFixture(ctx, executionprofiles.SeedAssistantServerFixtureParams{AssistantID: assistant, ProjectID: f.projectID, McpServerID: f.serverID}))
	binding, err := executionprofiles.Bind(ctx, pool, executionprofiles.Target{OrganizationID: f.orgID, ProjectID: f.projectID, AssistantID: assistant, TriggerID: trigger, Context: executionprofiles.ContextUserBacked}, executionprofiles.SourceIssuance)
	require.NoError(t, err)
	require.Empty(t, binding.Gaps)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	private, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	signer, err := mcpauthz.New(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})), string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})), "https://platform.example.invalid", false)
	require.NoError(t, err)
	issuer := principalcredential.New(signer, pool)
	principal := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String())
	mint := func(user string) string {
		t.Helper()
		token, _, err := issuer.Mint(principalcredential.Credential{
			OrganizationID: f.orgID, ProjectID: f.projectID, Principal: principal, AuthorizerUserID: user, SelectionProfileVersionID: validUUID(binding.VersionID),
		})
		require.NoError(t, err)
		return token
	}
	serve := func(token string) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
		t.Helper()
		requestCtx, err := issuer.Authenticate(ctx, token)
		require.NoError(t, err)
		requestCtx, err = f.service.authz.AdmitPrincipalCredential(requestCtx)
		require.NoError(t, err)
		authentication := f.authentication(urn.NewAgentSubject(agent.ID))
		choice, selected, err := f.service.directUpstreamChoice(requestCtx, httptest.NewRecorder(), authentication)
		require.True(t, selected, "a pinned profile never falls back to issuer-derived selection")
		if err != nil {
			return nil, err
		}
		require.Equal(t, validUUID(f.registration), choice.clientID)
		require.NotNil(t, authentication.authClient)
		require.Equal(t, client, authentication.authClient.clientID)
		return f.service.resolveAccessTokens(requestCtx, authentication, false)
	}
	alice, bob := mint("alice"), mint("bob")
	f.connect(urn.NewUserSubject("owner"), "owner-token")
	f.connect(urn.NewUserSubject("alice"), "alice-token")
	tokens, err := serve(alice)
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	require.Equal(t, "alice-token", tokens[f.issuerID].Token)

	tokens, err = serve(bob)
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken, "neither the owner nor Alice lends Bob their connection")
	require.Empty(t, tokens)
	f.connect(urn.NewUserSubject("bob"), "bob-token")
	tokens, err = serve(bob)
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	require.Equal(t, "bob-token", tokens[f.issuerID].Token)

	removed, err := access.DeletePrincipalGrantByIdentity(ctx, accessrepo.DeletePrincipalGrantByIdentityParams{
		OrganizationID: f.orgID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, "bob"), Scope: string(authz.ScopeMCPAuth), Selectors: useClient,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, removed)
	tokens, err = serve(bob)
	require.ErrorIs(t, err, errAuthClientNoAccess, "Bob's saved connection does not bypass the live auth grant check")
	require.Empty(t, tokens)
	tokens, err = serve(alice)
	require.NoError(t, err)
	require.Equal(t, "alice-token", tokens[f.issuerID].Token)
}
