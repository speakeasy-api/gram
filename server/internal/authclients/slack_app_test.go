package authclients_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth_clients"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authclients"
	"github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestCreateSlackAppAuthClientEncryptsSecretsAndRefusesServerAttachment(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	remoteServer := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Private)
	registration := seedRegistration(t, ctx, ti.conn, ti.organizationID, ti.projectID, "self")
	adminCtx := withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)
	readOnlyCtx := withOrgGrants(t, ctx, authz.ScopeOrgRead)

	// Non-admin callers cannot create slack_app auth clients.
	_, err := ti.service.CreateAuthClient(readOnlyCtx, &gen.CreateAuthClientPayload{
		ClientType: authz.AuthClientTypeSlackApp,
		Name:       "No admin",
		ProjectID:  new(ti.projectID.String()),
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	// Invalid slack_app creation cases are rejected.
	invalidCases := map[string]*gen.CreateAuthClientPayload{
		"with oauth registration": {
			ClientType:            authz.AuthClientTypeSlackApp,
			Name:                  "Slack app",
			ProjectID:             new(ti.projectID.String()),
			RemoteSessionClientID: new(registration.String()),
		},
		"offered on mcp server": {
			ClientType:   authz.AuthClientTypeSlackApp,
			Name:         "Slack app",
			ProjectID:    new(ti.projectID.String()),
			McpServerIds: []string{remoteServer.id.String()},
		},
		"non-secret header": {
			ClientType: authz.AuthClientTypeSlackApp,
			Name:       "Slack app",
			ProjectID:  new(ti.projectID.String()),
			Headers:    []*gen.AuthClientHeaderInput{{Name: authclients.SlackHeaderBotToken, IsSecret: false, Value: new("xoxb-plain")}},
		},
		"pass-through header": {
			ClientType: authz.AuthClientTypeSlackApp,
			Name:       "Slack app",
			ProjectID:  new(ti.projectID.String()),
			Headers:    []*gen.AuthClientHeaderInput{{Name: authclients.SlackHeaderBotToken, IsSecret: false, ValueFromRequestHeader: new("X-Slack-Token")}},
		},
		"unknown header key": {
			ClientType: authz.AuthClientTypeSlackApp,
			Name:       "Slack app",
			ProjectID:  new(ti.projectID.String()),
			Headers:    []*gen.AuthClientHeaderInput{{Name: "X-Api-Key", IsSecret: true, Value: new("secret")}},
		},
		"blank whitespace header value": {
			ClientType: authz.AuthClientTypeSlackApp,
			Name:       "Slack app",
			ProjectID:  new(ti.projectID.String()),
			Headers:    []*gen.AuthClientHeaderInput{{Name: authclients.SlackHeaderBotToken, IsSecret: true, Value: new("   \t ")}},
		},
		"control character in header value": {
			ClientType: authz.AuthClientTypeSlackApp,
			Name:       "Slack app",
			ProjectID:  new(ti.projectID.String()),
			Headers:    []*gen.AuthClientHeaderInput{{Name: authclients.SlackHeaderBotToken, IsSecret: true, Value: new("xoxb-bad\ntoken")}},
		},
	}
	for name, payload := range invalidCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ti.service.CreateAuthClient(adminCtx, payload)
			requireOopsCode(t, err, oops.CodeBadRequest)
		})
	}

	// Creating with 0 headers (onboarding before secrets are pasted) works.
	emptyClient, err := ti.service.CreateAuthClient(adminCtx, &gen.CreateAuthClientPayload{
		ClientType: authz.AuthClientTypeSlackApp,
		Name:       "Onboarding Slack app",
		ProjectID:  new(ti.projectID.String()),
	})
	require.NoError(t, err)
	require.Equal(t, authz.AuthClientTypeSlackApp, emptyClient.ClientType)
	require.Empty(t, emptyClient.Headers)

	// Creating with Slack secrets encrypts at rest and never exposes them in API or audit views.
	createdBefore := auditCount(t, ti, audit.ActionAuthClientCreate)
	client, err := ti.service.CreateAuthClient(adminCtx, &gen.CreateAuthClientPayload{
		ClientType: authz.AuthClientTypeSlackApp,
		Name:       "Support Slack app",
		ProjectID:  new(ti.projectID.String()),
		Headers: []*gen.AuthClientHeaderInput{
			{Name: "slack_bot_token", IsSecret: true, Value: new("xoxb-secret-bot-token")},
			{Name: authclients.SlackHeaderSigningSecret, IsSecret: true, Value: new("signing-secret-value")},
		},
	})
	require.NoError(t, err)
	require.Equal(t, authz.AuthClientTypeSlackApp, client.ClientType)
	require.False(t, client.Personal)
	require.Empty(t, client.Servers)
	require.Len(t, client.Headers, 2)
	for _, h := range client.Headers {
		require.True(t, h.IsSecret)
		require.True(t, h.HasStoredValue)
	}

	stored, err := repo.New(ti.conn).ListAuthClientStoredHeaders(adminCtx, repo.ListAuthClientStoredHeadersParams{
		AuthClientID:   uuid.MustParse(client.ID),
		OrganizationID: ti.organizationID,
	})
	require.NoError(t, err)
	require.Len(t, stored, 2)
	storedByName := map[string]string{}
	for _, h := range stored {
		require.True(t, h.IsSecret)
		require.NotEqual(t, "xoxb-secret-bot-token", h.Value.String)
		require.NotEqual(t, "signing-secret-value", h.Value.String)
		plain, decErr := ti.enc.Decrypt(h.Value.String)
		require.NoError(t, decErr)
		storedByName[h.Name] = plain
	}
	require.Equal(t, "xoxb-secret-bot-token", storedByName[authclients.SlackHeaderBotToken])
	require.Equal(t, "signing-secret-value", storedByName[authclients.SlackHeaderSigningSecret])

	// Audit payload must never contain plaintext secrets.
	require.Equal(t, createdBefore+1, auditCount(t, ti, audit.ActionAuthClientCreate))
	latest, err := audittest.LatestAuditLogByAction(adminCtx, ti.conn, audit.ActionAuthClientCreate)
	require.NoError(t, err)
	require.NotContains(t, string(latest.AfterSnapshot), "xoxb-secret-bot-token")
	require.NotContains(t, string(latest.AfterSnapshot), "signing-secret-value")

	// attachTx must refuse offering a slack_app client on any MCP server.
	_, err = ti.service.AttachAuthClientToMcpServer(adminCtx, &gen.AttachAuthClientToMcpServerPayload{
		ID:          client.ID,
		McpServerID: remoteServer.id.String(),
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestSlackAppCredentialStoreResolveForCallerAndLoad(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	authzEngine := authz.NewEngine(
		testenv.NewLogger(t),
		ti.conn,
		authztest.ChallengeLoggingAlwaysDisabled,
		workos.NewStubClient(),
		authz.EngineOpts{
			AdmitPrincipalCredential: runtimepolicy.AdmitPrincipalCredential,
			AdmitWorkloadSession:     runtimepolicy.AdmitWorkloadSession,
		},
	)
	store := authclients.NewSlackAppCredentialStore(ti.conn, authzEngine, ti.enc)

	// Two assistants in the same project with distinct slack_app auth clients.
	clientA, err := ti.service.CreateAuthClient(adminCtx, &gen.CreateAuthClientPayload{
		ClientType: authz.AuthClientTypeSlackApp,
		Name:       "Assistant A Slack",
		ProjectID:  new(ti.projectID.String()),
		Headers: []*gen.AuthClientHeaderInput{
			{Name: authclients.SlackHeaderBotToken, IsSecret: true, Value: new("xoxb-assistant-a")},
			{Name: authclients.SlackHeaderSigningSecret, IsSecret: true, Value: new("sig-a")},
		},
	})
	require.NoError(t, err)
	clientAID := uuid.MustParse(clientA.ID)

	clientB, err := ti.service.CreateAuthClient(adminCtx, &gen.CreateAuthClientPayload{
		ClientType: authz.AuthClientTypeSlackApp,
		Name:       "Assistant B Slack",
		ProjectID:  new(ti.projectID.String()),
		Headers: []*gen.AuthClientHeaderInput{
			{Name: authclients.SlackHeaderBotToken, IsSecret: true, Value: new("xoxb-assistant-b")},
			{Name: authclients.SlackHeaderSigningSecret, IsSecret: true, Value: new("sig-b")},
		},
	})
	require.NoError(t, err)
	clientBID := uuid.MustParse(clientB.ID)

	// Direct loader works for enabled slack_app clients and fails closed when disabled/deleted.
	credsMap, err := store.LoadSlackAppCredentials(ctx, ti.organizationID, ti.projectID, clientAID)
	require.NoError(t, err)
	require.Equal(t, "xoxb-assistant-a", credsMap[authclients.SlackHeaderBotToken])
	require.Equal(t, "sig-a", credsMap[authclients.SlackHeaderSigningSecret])

	// Agent A holds mcp:auth on clientA only; Agent B holds mcp:auth on clientB only;
	// AgentZero holds none; AgentBoth holds both.
	makeAgentCtx := func(name string, clientIDs ...uuid.UUID) context.Context {
		agent, createErr := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
			OrganizationID: ti.organizationID,
			OwnerUserID:    authCtx.UserID,
			ProjectID:      uuid.NullUUID{UUID: ti.projectID, Valid: true},
			Name:           name + "-" + uuid.NewString()[:8],
		})
		require.NoError(t, createErr)
		for _, cid := range clientIDs {
			sel, marshalErr := authz.AuthClientSelector(cid.String(), "").MarshalJSON()
			require.NoError(t, marshalErr)
			_, grantErr := agentsrepo.New(ti.conn).CreateAgentPolicyGrant(ctx, agentsrepo.CreateAgentPolicyGrantParams{
				OrganizationID: ti.organizationID,
				AgentID:        agent.ID,
				Scope:          string(authz.ScopeMCPAuth),
				Selectors:      sel,
			})
			require.NoError(t, grantErr)
		}
		// Grant owner wildcard mcp:auth so owner ceiling allows the agent's grants.
		ownerSel, marshalErr := authz.AuthClientSelector(authz.WildcardResource, "").MarshalJSON()
		require.NoError(t, marshalErr)
		_, _ = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
			OrganizationID: ti.organizationID,
			PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			Scope:          string(authz.ScopeMCPAuth),
			Selectors:      ownerSel,
		})

		agentAuthCtx := &contextvalues.AuthContext{
			ActiveOrganizationID: ti.organizationID,
			ProjectID:            &ti.projectID,
			UserID:               authCtx.UserID,
		}
		return contextvalues.WithPrincipalCredentialAuthorization(
			t.Context(),
			agentAuthCtx,
			urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()),
			contextvalues.PrincipalCredential{
				AuthorizerUserID:         authCtx.UserID,
				DerivesPolicyAtAdmission: true,
			},
		)
	}

	ctxA := makeAgentCtx("agent-a", clientAID)
	ctxB := makeAgentCtx("agent-b", clientBID)
	ctxZero := makeAgentCtx("agent-zero")
	ctxBoth := makeAgentCtx("agent-both", clientAID, clientBID)

	resolvedA, err := store.ResolveForCaller(ctxA, ti.organizationID, ti.projectID)
	require.NoError(t, err)
	require.Equal(t, clientAID, resolvedA.ClientID)
	require.Equal(t, "xoxb-assistant-a", resolvedA.BotToken)

	resolvedB, err := store.ResolveForCaller(ctxB, ti.organizationID, ti.projectID)
	require.NoError(t, err)
	require.Equal(t, clientBID, resolvedB.ClientID)
	require.Equal(t, "xoxb-assistant-b", resolvedB.BotToken)

	_, err = store.ResolveForCaller(ctxZero, ti.organizationID, ti.projectID)
	require.ErrorIs(t, err, authclients.ErrNoAuthorizedSlackAppClient)
	requireOopsCode(t, err, oops.CodeFailedPrecondition)

	_, err = store.ResolveForCaller(ctxBoth, ti.organizationID, ti.projectID)
	require.ErrorIs(t, err, authclients.ErrMultipleAuthorizedSlackAppClients)
	requireOopsCode(t, err, oops.CodeFailedPrecondition)

	// Disabling clientA causes both ResolveForCaller and LoadSlackAppCredentials to fail closed.
	_, err = ti.service.UpdateAuthClient(adminCtx, &gen.UpdateAuthClientPayload{
		ID:      clientA.ID,
		Enabled: new(false),
	})
	require.NoError(t, err)
	_, err = store.ResolveForCaller(ctxA, ti.organizationID, ti.projectID)
	require.ErrorIs(t, err, authclients.ErrNoAuthorizedSlackAppClient)
	_, err = store.LoadSlackAppCredentials(ctx, ti.organizationID, ti.projectID, clientAID)
	require.ErrorIs(t, err, authclients.ErrSlackAppClientDisabled)
	require.ErrorIs(t, store.ValidateSlackAppAuthClient(ctx, ti.organizationID, ti.projectID, clientAID), authclients.ErrSlackAppClientDisabled)

	// Soft-deleting clientB causes LoadSlackAppCredentials and ValidateSlackAppAuthClient to fail closed.
	require.NoError(t, testrepo.New(ti.conn).SoftDeleteAuthClientFixture(ctx, testrepo.SoftDeleteAuthClientFixtureParams{
		ID:             clientBID,
		OrganizationID: ti.organizationID,
	}))
	_, err = store.LoadSlackAppCredentials(ctx, ti.organizationID, ti.projectID, clientBID)
	require.ErrorIs(t, err, authclients.ErrSlackAppClientUnavailable)
	require.ErrorIs(t, store.ValidateSlackAppAuthClient(ctx, ti.organizationID, ti.projectID, clientBID), authclients.ErrSlackAppClientUnavailable)
}
