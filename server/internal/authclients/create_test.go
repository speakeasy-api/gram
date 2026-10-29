package authclients_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth_clients"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authclients"
	"github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestCreateStaticCredentialEncryptsSecretAndOffersIt(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	first := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Private)
	second := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Private)
	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)

	createdBefore := auditCount(t, ti, audit.ActionAuthClientCreate)
	attachedBefore := auditCount(t, ti, audit.ActionAuthClientAttachMcpServer)

	client, err := ti.service.CreateAuthClient(ctx, &gen.CreateAuthClientPayload{
		ClientType: authz.AuthClientTypeStaticCredential,
		Name:       " Engineering token ",
		Headers: []*gen.AuthClientHeaderInput{
			{Name: "X-Api-Key", IsSecret: true, Value: new("sk-live-value")},
			{Name: "X-Workspace", Value: new("engineering")},
		},
		McpServerIds: []string{first.id.String(), second.id.String(), first.id.String()},
	})
	require.NoError(t, err)
	require.Equal(t, "Engineering token", client.Name)
	require.Equal(t, authclients.SourceAdmin, string(client.Source))
	require.True(t, client.Enabled)
	require.Nil(t, client.ProjectID, "omitting the project shares the client across the organization")
	require.Len(t, client.Servers, 2)
	require.Len(t, client.Headers, 2)
	for _, h := range client.Headers {
		require.True(t, h.HasStoredValue)
	}

	stored, err := repo.New(ti.conn).ListAuthClientStoredHeaders(ctx, repo.ListAuthClientStoredHeadersParams{AuthClientID: uuid.MustParse(client.ID), OrganizationID: ti.organizationID})
	require.NoError(t, err)
	for _, h := range stored {
		switch h.Name {
		case "X-Api-Key":
			require.NotEqual(t, "sk-live-value", h.Value.String, "secret values are stored encrypted")
			plain, err := ti.enc.Decrypt(h.Value.String)
			require.NoError(t, err)
			require.Equal(t, "sk-live-value", plain)
		case "X-Workspace":
			require.Equal(t, "engineering", h.Value.String)
		}
	}

	require.Equal(t, createdBefore+1, auditCount(t, ti, audit.ActionAuthClientCreate))
	require.Equal(t, attachedBefore+2, auditCount(t, ti, audit.ActionAuthClientAttachMcpServer))
}

func TestCreateAuthClientValidatesTypeAndRegistration(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Private)
	personal := seedRegistration(t, ctx, ti.conn, ti.organizationID, ti.projectID, "subject")
	machine := seedRegistration(t, ctx, ti.conn, ti.organizationID, ti.projectID, "self")
	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)

	cases := map[string]*gen.CreateAuthClientPayload{
		"service account over a personal registration": {ClientType: authz.AuthClientTypeServiceAccount, Name: "SA", ProjectID: new(ti.projectID.String()), RemoteSessionClientID: new(personal.String())},
		"personal connection over a service account":   {ClientType: authz.AuthClientTypeRemoteSession, Name: "Mine", ProjectID: new(ti.projectID.String()), RemoteSessionClientID: new(machine.String())},
		"service account without a registration":       {ClientType: authz.AuthClientTypeServiceAccount, Name: "SA"},
		"static credential with a registration":        {ClientType: authz.AuthClientTypeStaticCredential, Name: "Token", RemoteSessionClientID: new(machine.String()), Headers: []*gen.AuthClientHeaderInput{{Name: "X-Key", Value: new("v")}}},
		"static credential without headers":            {ClientType: authz.AuthClientTypeStaticCredential, Name: "Token"},
		"static credential forwarding a header":        {ClientType: authz.AuthClientTypeStaticCredential, Name: "Token", Headers: []*gen.AuthClientHeaderInput{{Name: "X-Key", ValueFromRequestHeader: new("X-Upstream-Key")}}},
		"pass-through with a stored value":             {ClientType: authz.AuthClientTypePassThrough, Name: "Forward", Headers: []*gen.AuthClientHeaderInput{{Name: "X-Key", Value: new("v")}}},
		"pass-through reading Authorization":           {ClientType: authz.AuthClientTypePassThrough, Name: "Forward", Headers: []*gen.AuthClientHeaderInput{{Name: "X-Key", ValueFromRequestHeader: new("Authorization")}}},
		"service account configuring Authorization":    {ClientType: authz.AuthClientTypeServiceAccount, Name: "SA", ProjectID: new(ti.projectID.String()), RemoteSessionClientID: new(machine.String()), Headers: []*gen.AuthClientHeaderInput{{Name: "authorization", Value: new("Bearer x")}}},
		"duplicate header names":                       {ClientType: authz.AuthClientTypeStaticCredential, Name: "Token", Headers: []*gen.AuthClientHeaderInput{{Name: "X-Key", Value: new("a")}, {Name: "x_key", Value: new("b")}}},
		"ema":                                          {ClientType: authz.AuthClientTypeEMA, Name: "EMA"},
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ti.service.CreateAuthClient(ctx, payload)
			requireOopsCode(t, err, oops.CodeBadRequest)
		})
	}

	sa, err := ti.service.CreateAuthClient(ctx, &gen.CreateAuthClientPayload{
		ClientType:            authz.AuthClientTypeServiceAccount,
		Name:                  "Marketing",
		ProjectID:             new(ti.projectID.String()),
		RemoteSessionClientID: new(machine.String()),
		Headers:               []*gen.AuthClientHeaderInput{{Name: "X-Workspace", Value: new("marketing")}},
		McpServerIds:          []string{server.id.String()},
	})
	require.NoError(t, err)
	require.False(t, sa.Personal)
	require.Equal(t, machine.String(), *sa.RemoteSessionClientID)

	mine, err := ti.service.CreateAuthClient(ctx, &gen.CreateAuthClientPayload{
		ClientType:            authz.AuthClientTypeRemoteSession,
		Name:                  "Each person's account",
		ProjectID:             new(ti.projectID.String()),
		RemoteSessionClientID: new(personal.String()),
	})
	require.NoError(t, err)
	require.True(t, mine.Personal)
}

func TestCreateAuthClientRequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead)

	_, err := ti.service.CreateAuthClient(ctx, &gen.CreateAuthClientPayload{
		ClientType: authz.AuthClientTypeStaticCredential,
		Name:       "Token",
		Headers:    []*gen.AuthClientHeaderInput{{Name: "X-Key", Value: new("v")}},
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCreateAuthClientRefusesRegistrationFromAnotherOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	otherOrg, otherProject := seedOtherOrganization(t, ctx, ti.conn)
	foreign := seedRegistration(t, ctx, ti.conn, otherOrg, otherProject, "self")
	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)

	_, err := ti.service.CreateAuthClient(ctx, &gen.CreateAuthClientPayload{
		ClientType:            authz.AuthClientTypeServiceAccount,
		Name:                  "Borrowed",
		RemoteSessionClientID: new(foreign.String()),
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestCreateAuthClientRefusesRegistrationWhoseIssuerIsAnotherProjects(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	other := seedProject(t, ctx, ti)
	queries := remotesessionsrepo.New(ti.conn)
	suffix := uuid.NewString()[:8]
	issuer, err := queries.CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID: uuid.NullUUID{UUID: other, Valid: true}, Slug: "other-" + suffix, Issuer: "https://" + suffix + ".other.example.com",
		AuthorizationEndpoint: conv.ToPGText("https://" + suffix + ".other.example.com/authorize"), TokenEndpoint: conv.ToPGText("https://" + suffix + ".other.example.com/token"),
		ScopesSupported: []string{}, GrantTypesSupported: []string{"client_credentials"}, ResponseTypesSupported: []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post"}, CodeChallengeMethodsSupported: []string{"S256"},
	})
	require.NoError(t, err)
	registration, err := queries.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		OrganizationID: conv.ToPGText(ti.organizationID), RemoteSessionIssuerID: issuer.ID, ClientID: "client-" + suffix,
		ClientIDIssuedAt: conv.ToPGTimestamptz(time.Now()), TokenEndpointAuthMethod: conv.ToPGText("client_secret_post"), CredentialOwner: conv.ToPGText("self"),
	})
	require.NoError(t, err)
	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)

	_, err = ti.service.CreateAuthClient(ctx, &gen.CreateAuthClientPayload{
		ClientType: authz.AuthClientTypeServiceAccount, Name: "Borrowed issuer",
		ProjectID: new(ti.projectID.String()), RemoteSessionClientID: new(registration.ID.String()),
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestAttachAuthClientSharesOneClientAcrossServers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	first := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Private)
	second := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Private)
	clientID := seedStaticClient(t, ctx, ti.conn, ti.organizationID, ti.projectID, "Shared token")
	associate(t, ctx, ti.conn, ti.organizationID, first, clientID)
	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)

	before := auditCount(t, ti, audit.ActionAuthClientAttachMcpServer)
	client, err := ti.service.AttachAuthClientToMcpServer(ctx, &gen.AttachAuthClientToMcpServerPayload{ID: clientID.String(), McpServerID: second.id.String()})
	require.NoError(t, err)
	require.Len(t, client.Servers, 2)
	require.Equal(t, before+1, auditCount(t, ti, audit.ActionAuthClientAttachMcpServer))

	_, err = ti.service.AttachAuthClientToMcpServer(ctx, &gen.AttachAuthClientToMcpServerPayload{ID: clientID.String(), McpServerID: second.id.String()})
	requireOopsCode(t, err, oops.CodeConflict)

	_, err = ti.service.DetachAuthClientFromMcpServer(ctx, &gen.DetachAuthClientFromMcpServerPayload{ID: clientID.String(), McpServerID: second.id.String()})
	require.NoError(t, err)
	client, err = ti.service.AttachAuthClientToMcpServer(ctx, &gen.AttachAuthClientToMcpServerPayload{ID: clientID.String(), McpServerID: second.id.String()})
	require.NoError(t, err, "a detached server can offer the client again")
	require.Len(t, client.Servers, 2)
}

func TestAttachAuthClientRefusesServersItCannotServe(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	_, otherProject := seedOtherOrganization(t, ctx, ti.conn)
	foreignServer := seedServer(t, ctx, ti.conn, otherProject, visibility.Private)
	noAuth := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Private)
	clientID := seedStaticClient(t, ctx, ti.conn, ti.organizationID, ti.projectID, "Project token")
	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)

	_, err := ti.service.SetMcpServerUpstreamAuthRequirement(ctx, &gen.SetMcpServerUpstreamAuthRequirementPayload{McpServerID: noAuth.id.String(), RequiresUpstreamAuth: false})
	require.NoError(t, err)

	_, err = ti.service.AttachAuthClientToMcpServer(ctx, &gen.AttachAuthClientToMcpServerPayload{ID: clientID.String(), McpServerID: foreignServer.id.String()})
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = ti.service.AttachAuthClientToMcpServer(ctx, &gen.AttachAuthClientToMcpServerPayload{ID: clientID.String(), McpServerID: noAuth.id.String()})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestAttachProjectClientRefusesServerInAnotherProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	clientID := seedStaticClient(t, ctx, ti.conn, ti.organizationID, ti.projectID, "Project token")
	otherProject := seedProject(t, ctx, ti)
	server := seedServer(t, ctx, ti.conn, otherProject, visibility.Private)
	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)

	_, err := ti.service.AttachAuthClientToMcpServer(ctx, &gen.AttachAuthClientToMcpServerPayload{ID: clientID.String(), McpServerID: server.id.String()})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestSetAuthClientHeadersRotatesAndKeepsUnchangedSecrets(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)

	client, err := ti.service.CreateAuthClient(ctx, &gen.CreateAuthClientPayload{
		ClientType: authz.AuthClientTypeStaticCredential,
		Name:       "Token",
		Headers: []*gen.AuthClientHeaderInput{
			{Name: "X-Api-Key", IsSecret: true, Value: new("first-secret")},
			{Name: "X-Signing-Key", IsSecret: true, Value: new("signing-secret")},
		},
	})
	require.NoError(t, err)

	before := auditCount(t, ti, audit.ActionAuthClientUpdateHeaders)
	updated, err := ti.service.SetAuthClientHeaders(ctx, &gen.SetAuthClientHeadersPayload{
		ID: client.ID,
		Headers: []*gen.AuthClientHeaderInput{
			{Name: "X-Api-Key", IsSecret: true, Value: new("rotated-secret")},
			{Name: "X-Signing-Key", IsSecret: true},
		},
	})
	require.NoError(t, err)
	require.Len(t, updated.Headers, 2)
	require.Equal(t, before+1, auditCount(t, ti, audit.ActionAuthClientUpdateHeaders))

	stored, err := repo.New(ti.conn).ListAuthClientStoredHeaders(ctx, repo.ListAuthClientStoredHeadersParams{AuthClientID: uuid.MustParse(client.ID), OrganizationID: ti.organizationID})
	require.NoError(t, err)
	values := map[string]string{}
	for _, h := range stored {
		plain, err := ti.enc.Decrypt(h.Value.String)
		require.NoError(t, err)
		values[h.Name] = plain
	}
	require.Equal(t, map[string]string{"X-Api-Key": "rotated-secret", "X-Signing-Key": "signing-secret"}, values)

	_, err = ti.service.SetAuthClientHeaders(ctx, &gen.SetAuthClientHeadersPayload{
		ID:      client.ID,
		Headers: []*gen.AuthClientHeaderInput{{Name: "X-New-Key", IsSecret: true}},
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.SetAuthClientHeaders(ctx, &gen.SetAuthClientHeadersPayload{
		ID:      client.ID,
		Headers: []*gen.AuthClientHeaderInput{{Name: "x_api_key", IsSecret: true}},
	})
	require.NoError(t, err, "a stored secret is kept whichever spelling names it")

	_, err = ti.service.SetAuthClientHeaders(ctx, &gen.SetAuthClientHeadersPayload{ID: client.ID, Headers: []*gen.AuthClientHeaderInput{nil}})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestSetAuthClientHeadersKeepsStoredNonSecretValues(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)

	client, err := ti.service.CreateAuthClient(ctx, &gen.CreateAuthClientPayload{
		ClientType: authz.AuthClientTypeStaticCredential,
		Name:       "Token",
		Headers: []*gen.AuthClientHeaderInput{
			{Name: "X-Api-Key", IsSecret: true, Value: new("api-secret")},
			{Name: "X-Api-Version", Value: new("2026-01-01")},
		},
	})
	require.NoError(t, err)

	_, err = ti.service.SetAuthClientHeaders(ctx, &gen.SetAuthClientHeadersPayload{
		ID: client.ID,
		Headers: []*gen.AuthClientHeaderInput{
			{Name: "X-Api-Key", IsSecret: true},
			{Name: "X-Api-Version"},
		},
	})
	require.NoError(t, err)

	stored, err := repo.New(ti.conn).ListAuthClientStoredHeaders(ctx, repo.ListAuthClientStoredHeadersParams{AuthClientID: uuid.MustParse(client.ID), OrganizationID: ti.organizationID})
	require.NoError(t, err)
	values := map[string]string{}
	for _, h := range stored {
		if h.IsSecret {
			plain, err := ti.enc.Decrypt(h.Value.String)
			require.NoError(t, err)
			values[h.Name] = plain
			continue
		}
		values[h.Name] = h.Value.String
	}
	require.Equal(t, map[string]string{"X-Api-Key": "api-secret", "X-Api-Version": "2026-01-01"}, values)

	for _, flipped := range []*gen.AuthClientHeaderInput{
		{Name: "X-Api-Key"},
		{Name: "X-Api-Version", IsSecret: true},
	} {
		_, err = ti.service.SetAuthClientHeaders(ctx, &gen.SetAuthClientHeadersPayload{ID: client.ID, Headers: []*gen.AuthClientHeaderInput{flipped}})
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
}

func TestSetAuthClientHeadersLeavesBackfilledClientsToServerSettings(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	clientID := seedStaticClient(t, ctx, ti.conn, ti.organizationID, ti.projectID, "Copied token")
	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)

	_, err := ti.service.SetAuthClientHeaders(ctx, &gen.SetAuthClientHeadersPayload{
		ID:      clientID.String(),
		Headers: []*gen.AuthClientHeaderInput{{Name: "Authorization", IsSecret: true, Value: new("Bearer new")}},
	})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestCreateAuthClientLocksServersInOneOrder(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	first := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Private)
	second := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Private)
	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)
	low, high := first.id, second.id
	if bytes.Compare(low[:], high[:]) > 0 {
		low, high = high, low
	}

	// A competing transaction holds the server that sorts first, then reaches
	// for the other once the create waits on it.
	tx, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: the competing row lock is the behavior under test.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	holder := testrepo.New(tx)
	_, err = holder.LockMCPServerForUpdateFixture(ctx, low)
	require.NoError(t, err)
	holderPID, err := holder.BackendPIDFixture(ctx)
	require.NoError(t, err)

	created := make(chan error, 1)
	go func() {
		_, err := ti.service.CreateAuthClient(ctx, &gen.CreateAuthClientPayload{
			ClientType:   authz.AuthClientTypeStaticCredential,
			Name:         "Ordered",
			Headers:      []*gen.AuthClientHeaderInput{{Name: "X-Api-Key", IsSecret: true, Value: new("sk-value")}},
			McpServerIds: []string{high.String(), low.String()},
		})
		created <- err
	}()

	require.Eventually(t, func() bool {
		blocked, err := testrepo.New(ti.conn).CountBackendsBlockedByFixture(ctx, holderPID)
		return err == nil && blocked > 0
	}, 10*time.Second, 20*time.Millisecond, "the create waits on the held server")

	_, err = holder.LockMCPServerForUpdateFixture(ctx, high)
	require.NoError(t, err, "the create holds no server while it waits, so nothing deadlocks")
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, <-created)
}

func TestListMcpServerChoicesOffersWhatAnAgentKeyCanUse(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Private)
	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)

	static, err := ti.service.CreateAuthClient(ctx, &gen.CreateAuthClientPayload{
		ClientType:   authz.AuthClientTypeStaticCredential,
		Name:         "Team token",
		Headers:      []*gen.AuthClientHeaderInput{{Name: "X-Api-Key", IsSecret: true, Value: new("sk-value")}},
		McpServerIds: []string{server.id.String()},
	})
	require.NoError(t, err)
	_, err = ti.service.CreateAuthClient(ctx, &gen.CreateAuthClientPayload{
		ClientType:   authz.AuthClientTypePassThrough,
		Name:         "Caller token",
		Headers:      []*gen.AuthClientHeaderInput{{Name: "Authorization", ValueFromRequestHeader: new("X-Caller-Token")}},
		McpServerIds: []string{server.id.String()},
	})
	require.NoError(t, err)

	personal := seedRegistration(t, ctx, ti.conn, ti.organizationID, ti.projectID, "subject")
	_, err = ti.service.CreateAuthClient(ctx, &gen.CreateAuthClientPayload{
		ClientType:            authz.AuthClientTypeRemoteSession,
		Name:                  "Personal connection",
		ProjectID:             new(ti.projectID.String()),
		RemoteSessionClientID: new(personal.String()),
		McpServerIds:          []string{server.id.String()},
	})
	require.NoError(t, err)
	disabled, err := ti.service.CreateAuthClient(ctx, &gen.CreateAuthClientPayload{
		ClientType:   authz.AuthClientTypeStaticCredential,
		Name:         "Disabled team token",
		Headers:      []*gen.AuthClientHeaderInput{{Name: "X-Api-Key", IsSecret: true, Value: new("sk-disabled")}},
		McpServerIds: []string{server.id.String()},
	})
	require.NoError(t, err)
	_, err = ti.service.UpdateAuthClient(ctx, &gen.UpdateAuthClientPayload{ID: disabled.ID, Enabled: new(false)})
	require.NoError(t, err)

	connectCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeMCPConnect, server.id.String()))
	listed, err := ti.service.ListMcpServerAuthClientChoices(connectCtx, &gen.ListMcpServerAuthClientChoicesPayload{McpServerID: server.id.String()})
	require.NoError(t, err)
	require.Len(t, listed.Choices, 1, "only enabled shared credentials are offered, never personal or pass-through clients")
	require.Equal(t, static.ID, listed.Choices[0].ID)
}
