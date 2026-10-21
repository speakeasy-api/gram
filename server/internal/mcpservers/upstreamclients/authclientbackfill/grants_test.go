package authclientbackfill

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func (f *fixture) grant(principal urn.Principal, scope authz.Scope, selector authz.Selector) {
	f.t.Helper()
	_, err := authz.InsertPrincipalGrantsIfAbsentTx(f.t.Context(), f.pool, f.orgID, principal, []*authz.RoleGrant{{Scope: string(scope), Selectors: []authz.Selector{selector}}})
	require.NoError(f.t, err)
}

func (f *fixture) grantsOf(principal urn.Principal) []authz.Grant {
	f.t.Helper()
	grants, err := authz.LoadGrants(f.t.Context(), f.pool, f.orgID, []urn.Principal{principal})
	require.NoError(f.t, err)
	return grants
}

func (f *fixture) apiKey(projectID uuid.NullUUID, scopes ...string) uuid.UUID {
	f.t.Helper()
	creator := "user_" + uuid.NewString()
	_, err := usersrepo.New(f.pool).UpsertUser(f.t.Context(), usersrepo.UpsertUserParams{ID: creator, Email: creator + "@example.com", DisplayName: creator, PhotoUrl: pgtype.Text{String: "", Valid: false}, Admin: false})
	require.NoError(f.t, err)
	key, err := keysrepo.New(f.pool).CreateAPIKey(f.t.Context(), keysrepo.CreateAPIKeyParams{
		OrganizationID: f.orgID, ProjectID: projectID, CreatedByUserID: creator,
		Name: "k-" + uuid.NewString()[:8], KeyPrefix: "gram_test", KeyHash: "hash-" + uuid.NewString(), Scopes: scopes,
	})
	require.NoError(f.t, err)
	return key.ID
}

// sharedClient links a new server to a new service account client and
// records it, returning the server and its auth client.
func (f *fixture) sharedClient() (serverID, authClientID uuid.UUID) {
	f.t.Helper()
	serverID = f.server()
	registration := f.client("self")
	f.associate(serverID, registration)
	f.run(true)
	for _, row := range f.authClients() {
		if row.RemoteSessionClientID == registration {
			return serverID, row.ID
		}
	}
	f.t.Fatal("service account client not recorded")
	return uuid.Nil, uuid.Nil
}

func mandatory(t *testing.T, ctx context.Context, f *fixture, checks ...authz.Check) authz.MandatoryDecision {
	t.Helper()
	engine := authz.NewEngine(testenv.NewLogger(t), f.pool, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())
	decision, err := engine.AuthorizeMandatory(ctx, checks...)
	require.NoError(t, err)
	return decision
}

func TestRun_GrantsThePersonalDefaultOnce(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	dry := f.run(false)
	require.Equal(t, 1, dry.PendingGrantsByKind[GrantPersonalDefault])
	require.Empty(t, f.grantsOf(authz.AllUsersPrincipal()), "a dry run writes nothing")

	applied := f.run(true)
	require.Equal(t, 1, applied.GrantsWritten)
	grants := f.grantsOf(authz.AllUsersPrincipal())
	require.Len(t, grants, 1)
	require.Equal(t, authz.PersonalAuthClientSelector(), grants[0].Selector)

	again := f.run(true)
	require.Zero(t, again.PendingGrants)
	require.Zero(t, again.GrantsWritten)
}

func TestRun_SharedClientGrantsMirrorServerAccess(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	serverID, clientID := f.sharedClient()
	project := f.projectID.String()

	connects := urn.NewPrincipal(urn.PrincipalTypeRole, "connects")
	writes := urn.NewPrincipal(urn.PrincipalTypeRole, "writes")
	oneTool := urn.NewPrincipal(urn.PrincipalTypeUser, "one_tool")
	elsewhere := urn.NewPrincipal(urn.PrincipalTypeUser, "elsewhere")
	f.grant(connects, authz.ScopeMCPConnect, authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: serverID.String()})
	f.grant(writes, authz.ScopeMCPWrite, authz.ProjectServersSelector(project))
	f.grant(oneTool, authz.ScopeMCPConnect, authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: serverID.String(), authz.SelectorKeyTool: "search"})
	f.grant(elsewhere, authz.ScopeMCPConnect, authz.ProjectServersSelector(uuid.NewString()))

	report := f.run(true)
	require.Equal(t, 3, report.PendingGrantsByKind[GrantSharedClient])

	concrete := authz.AuthClientSelector(clientID.String(), "")
	require.Contains(t, f.grantsOf(connects), authz.Grant{PrincipalUrn: connects.String(), Scope: authz.ScopeMCPAuth, Selector: concrete})
	require.Contains(t, f.grantsOf(oneTool), authz.Grant{PrincipalUrn: oneTool.String(), Scope: authz.ScopeMCPAuth, Selector: authz.AuthClientTypeSelector(TypeServiceAccount, project)},
		"a user never gets a concrete client grant, which would outrank inherited blocks")
	require.Contains(t, f.grantsOf(writes), authz.Grant{PrincipalUrn: writes.String(), Scope: authz.ScopeMCPAuth, Selector: authz.AuthClientTypeSelector(TypeServiceAccount, project)},
		"a wildcard grant is mirrored as a wildcard, never as a concrete client")
	for _, grant := range f.grantsOf(elsewhere) {
		require.NotEqual(t, authz.ScopeMCPAuth, grant.Scope, "no access to the server, no client")
	}
	require.Zero(t, f.run(false).PendingGrants)
}

func TestRun_LegacyKeysGetTheirCurrentReach(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	serverID, clientID := f.sharedClient()
	second := uuid.New()
	require.NoError(t, f.q.SeedProjectFixture(t.Context(), SeedProjectFixtureParams{ID: second, Slug: "p-" + second.String()[:8], OrganizationID: f.orgID}))

	orgWide := f.apiKey(uuid.NullUUID{}, "consumer")
	bound := f.apiKey(uuid.NullUUID{UUID: second, Valid: true}, "consumer")
	explicit := f.apiKey(uuid.NullUUID{}, "consumer")
	hooksOnly := f.apiKey(uuid.NullUUID{}, "hooks")
	explicitPolicy := authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: serverID.String(), authz.SelectorKeyProjectID: f.projectID.String()}
	f.grant(urn.NewAPIKeyPrincipal(explicit), authz.ScopeMCPConnect, explicitPolicy)

	keyCtx := func(id uuid.UUID) context.Context {
		return contextvalues.WithLegacyAPIKeyAuthorization(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: f.orgID, APIKeyID: id.String(), AccountType: "enterprise"})
	}
	connect := authz.MCPCheck(authz.ScopeMCPConnect, serverID.String(), f.projectID.String())
	useClient := authz.MCPAuthClientCheck(clientID.String(), authz.MCPAuthClientDimensions{ClientType: TypeServiceAccount, ProjectID: f.projectID.String(), ConnectionOwner: ""})
	require.False(t, mandatory(t, keyCtx(orgWide), f, connect).Allowed, "no policy before the backfill")

	report := f.run(true)
	require.Equal(t, 4, report.PendingGrantsByKind[GrantAPIKey], "org-wide: two projects and one client; bound: its project")

	require.True(t, mandatory(t, keyCtx(orgWide), f, connect, useClient).Allowed)
	require.True(t, mandatory(t, keyCtx(orgWide), f, authz.MCPCheck(authz.ScopeMCPConnect, uuid.NewString(), second.String())).Allowed)
	require.True(t, mandatory(t, keyCtx(bound), f, authz.MCPCheck(authz.ScopeMCPConnect, uuid.NewString(), second.String())).Allowed)
	require.False(t, mandatory(t, keyCtx(bound), f, connect).Allowed, "a bound key keeps its project")

	require.Equal(t, []authz.Grant{{PrincipalUrn: urn.NewAPIKeyPrincipal(explicit).String(), Scope: authz.ScopeMCPConnect, Selector: explicitPolicy}}, f.grantsOf(urn.NewAPIKeyPrincipal(explicit)), "an explicit policy is never widened")
	require.Empty(t, f.grantsOf(urn.NewAPIKeyPrincipal(hooksOnly)), "a key that cannot reach MCP gets nothing")
	require.Zero(t, f.run(false).PendingGrants)
}

func TestRun_GrantsStayInTheirOrganization(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	other := newFixtureIn(t, f.pool)
	other.apiKey(uuid.NullUUID{}, "consumer")

	f.run(true)

	require.Empty(t, other.grantsOf(authz.AllUsersPrincipal()))
	require.Equal(t, 2, other.run(false).PendingGrants, "the other organization still needs its own")
}

func TestRun_InferredGrantsNeverOutrankBlocks(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	serverID, clientID := f.sharedClient()
	user := urn.NewPrincipal(urn.PrincipalTypeUser, "user_blocked")
	role := urn.NewPrincipal(urn.PrincipalTypeRole, "blocks")
	f.grant(user, authz.ScopeMCPConnect, authz.ProjectServersSelector(f.projectID.String()))
	f.grant(user, authz.ScopeMCPConnect, authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: serverID.String()})
	f.grant(role, authz.ScopeMCPBlockedAuth, authz.AuthClientSelector(clientID.String(), ""))

	f.run(true)

	grants := append(f.grantsOf(user), f.grantsOf(role)...)
	check := authz.MCPAuthClientCheck(clientID.String(), authz.MCPAuthClientDimensions{ClientType: TypeServiceAccount, ProjectID: f.projectID.String(), ConnectionOwner: ""})
	session := "session_1"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: f.orgID, UserID: "user_blocked", SessionID: &session, AccountType: "enterprise"})
	require.False(t, mandatory(t, authz.GrantsToContext(ctx, grants), f, check).Allowed, "the inherited block still applies")
}

func TestRun_AgentsGetThePersonalDefault(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	agent := urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString())
	f.grant(agent, authz.ScopeMCPConnect, authz.ProjectServersSelector(f.projectID.String()))

	report := f.run(true)

	require.Equal(t, 2, report.PendingGrantsByKind[GrantPersonalDefault], "user:all and the agent")
	require.Contains(t, f.grantsOf(agent), authz.Grant{PrincipalUrn: agent.String(), Scope: authz.ScopeMCPAuth, Selector: authz.PersonalAuthClientSelector()})
}

func TestRun_KeyPoliciesAreWrittenOnce(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	key := f.apiKey(uuid.NullUUID{}, "consumer")
	f.run(true)

	// A later client is not added: a provisioned policy cannot be told from
	// one an admin wrote with the same shape.
	serverID, clientID := f.sharedClient()
	ctx := contextvalues.WithLegacyAPIKeyAuthorization(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: f.orgID, APIKeyID: key.String(), AccountType: "enterprise"})
	use := authz.MCPAuthClientCheck(clientID.String(), authz.MCPAuthClientDimensions{ClientType: TypeServiceAccount, ProjectID: f.projectID.String(), ConnectionOwner: ""})
	require.True(t, mandatory(t, ctx, f, authz.MCPCheck(authz.ScopeMCPConnect, serverID.String(), f.projectID.String())).Allowed)
	require.False(t, mandatory(t, ctx, f, use).Allowed)
	require.Zero(t, f.run(false).PendingGrantsByKind[GrantAPIKey])
}

func TestRun_KeyPoliciesWaitForEveryClientPhase(t *testing.T) { //nolint:paralleltest // swaps a package seam
	f := newFixture(t)
	key := f.apiKey(uuid.NullUUID{}, "consumer")
	original := beforePhase
	beforePhase = func(name string) error {
		if name == PhaseHeaders {
			return errors.New("injected")
		}
		return nil
	}
	t.Cleanup(func() { beforePhase = original })

	report := f.run(true)
	require.Contains(t, report.PhaseErrors, PhaseHeaders)
	require.Zero(t, report.PendingGrantsByKind[GrantAPIKey], "no key policy from a run that missed clients")
	require.Empty(t, f.grantsOf(urn.NewAPIKeyPrincipal(key)))

	beforePhase = original
	f.run(true)
	require.NotEmpty(t, f.grantsOf(urn.NewAPIKeyPrincipal(key)))
}

func TestRunGrants_KeyPoliciesWaitForRecordedClients(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	key := f.apiKey(uuid.NullUUID{}, "consumer")
	serverID := f.server()
	registration := f.client("self")
	f.associate(serverID, registration)

	early, err := RunGrants(t.Context(), f.pool, Options{OrganizationID: f.orgID, Apply: true, PageSize: 0})
	require.NoError(t, err)
	require.True(t, early.APIKeysWaiting)
	require.Empty(t, f.grantsOf(urn.NewAPIKeyPrincipal(key)), "no key policy before its clients are recorded")

	f.run(true)
	var clientID uuid.UUID
	for _, row := range f.authClients() {
		if row.RemoteSessionClientID == registration {
			clientID = row.ID
		}
	}
	ctx := contextvalues.WithLegacyAPIKeyAuthorization(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: f.orgID, APIKeyID: key.String(), AccountType: "enterprise"})
	use := authz.MCPAuthClientCheck(clientID.String(), authz.MCPAuthClientDimensions{ClientType: TypeServiceAccount, ProjectID: f.projectID.String(), ConnectionOwner: ""})
	require.True(t, mandatory(t, ctx, f, authz.MCPCheck(authz.ScopeMCPConnect, serverID.String(), f.projectID.String()), use).Allowed)

	settled, err := RunGrants(t.Context(), f.pool, Options{OrganizationID: f.orgID, Apply: false, PageSize: 0})
	require.NoError(t, err)
	require.False(t, settled.APIKeysWaiting)
	require.Zero(t, settled.PendingGrants)
}

func TestRun_UserServerGrantsKeepInheritedBlocks(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	serverID, clientID := f.sharedClient()
	user := urn.NewPrincipal(urn.PrincipalTypeUser, "user_concrete")
	f.grant(user, authz.ScopeMCPConnect, authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: serverID.String()})
	f.run(true)

	session := "session_1"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: f.orgID, UserID: "user_concrete", SessionID: &session, AccountType: "enterprise"})
	check := authz.MCPAuthClientCheck(clientID.String(), authz.MCPAuthClientDimensions{ClientType: TypeServiceAccount, ProjectID: f.projectID.String(), ConnectionOwner: ""})
	require.True(t, mandatory(t, authz.GrantsToContext(ctx, f.grantsOf(user)), f, check).Allowed, "the user keeps today's reach")

	role := urn.NewPrincipal(urn.PrincipalTypeRole, "blocks_concrete")
	f.grant(role, authz.ScopeMCPBlockedAuth, authz.AuthClientSelector(clientID.String(), ""))
	grants := append(f.grantsOf(user), f.grantsOf(role)...)
	require.False(t, mandatory(t, authz.GrantsToContext(ctx, grants), f, check).Allowed, "a role's block still applies")
}
