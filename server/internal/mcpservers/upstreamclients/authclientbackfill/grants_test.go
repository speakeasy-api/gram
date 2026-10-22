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

func (f *fixture) provisions() []ListAuthPermissionProvisionsFixtureRow {
	f.t.Helper()
	rows, err := f.q.ListAuthPermissionProvisionsFixture(f.t.Context(), f.orgID)
	require.NoError(f.t, err)
	return rows
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
	require.Contains(t, f.grantsOf(connects), authz.Grant{PrincipalUrn: connects.String(), Scope: authz.ScopeMCPAuth, Selector: concrete, Inferred: true})
	require.Contains(t, f.grantsOf(oneTool), authz.Grant{PrincipalUrn: oneTool.String(), Scope: authz.ScopeMCPAuth, Selector: concrete, Inferred: true},
		"a user's grant names the client and is marked inferred, so it never outranks inherited blocks")
	require.Contains(t, f.grantsOf(writes), authz.Grant{PrincipalUrn: writes.String(), Scope: authz.ScopeMCPAuth, Selector: concrete, Inferred: true},
		"a wildcard grant names the clients it reaches today, never a whole client type")
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
	require.Contains(t, f.grantsOf(agent), authz.Grant{PrincipalUrn: agent.String(), Scope: authz.ScopeMCPAuth, Selector: authz.PersonalAuthClientSelector(), Inferred: true})
}

func TestRun_ProvisionedKeyPoliciesGetLaterClients(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	key := f.apiKey(uuid.NullUUID{}, "consumer")
	f.run(true)

	serverID, clientID := f.sharedClient()
	ctx := contextvalues.WithLegacyAPIKeyAuthorization(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: f.orgID, APIKeyID: key.String(), AccountType: "enterprise"})
	use := authz.MCPAuthClientCheck(clientID.String(), authz.MCPAuthClientDimensions{ClientType: TypeServiceAccount, ProjectID: f.projectID.String(), ConnectionOwner: ""})
	require.True(t, mandatory(t, ctx, f, authz.MCPCheck(authz.ScopeMCPConnect, serverID.String(), f.projectID.String()), use).Allowed,
		"a policy the backfill wrote gets clients recorded since")
	require.Zero(t, f.run(false).PendingGrants)
}

func TestRun_ExplicitAndTakenOverKeyPoliciesStayPut(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	explicit := f.apiKey(uuid.NullUUID{}, "consumer")
	f.grant(urn.NewAPIKeyPrincipal(explicit), authz.ScopeMCPConnect, authz.ProjectServersSelector(f.projectID.String()))
	adopted := f.apiKey(uuid.NullUUID{}, "consumer")
	f.run(true)

	// An admin writing the provisioned grant takes it over.
	require.NoError(t, authz.PatchPrincipalGrants(t.Context(), f.pool, f.orgID, urn.NewAPIKeyPrincipal(adopted),
		[]*authz.RoleGrant{{Scope: string(authz.ScopeMCPConnect), Selectors: []authz.Selector{authz.ProjectServersSelector(f.projectID.String())}}}, nil))
	for _, row := range f.provisions() {
		if row.PrincipalUrn == urn.NewAPIKeyPrincipal(adopted).String() {
			require.True(t, row.TakenOver, "the admin's grant is no longer provisioned")
		}
	}

	_, clientID := f.sharedClient()
	for _, key := range []uuid.UUID{explicit, adopted} {
		for _, grant := range f.grantsOf(urn.NewAPIKeyPrincipal(key)) {
			require.NotEqual(t, authz.AuthClientSelector(clientID.String(), f.projectID.String()), grant.Selector, "an admin's policy is never widened")
		}
	}
}

func TestRun_NeverRestoresARemovedDefault(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	key := f.apiKey(uuid.NullUUID{}, "consumer")
	f.run(true)
	require.NotEmpty(t, f.provisions())

	personal := []*authz.RoleGrant{{Scope: string(authz.ScopeMCPAuth), Selectors: []authz.Selector{authz.PersonalAuthClientSelector()}}}
	require.NoError(t, authz.PatchPrincipalGrants(t.Context(), f.pool, f.orgID, authz.AllUsersPrincipal(), nil, personal))
	keyPolicy := []*authz.RoleGrant{{Scope: string(authz.ScopeMCPConnect), Selectors: []authz.Selector{authz.ProjectServersSelector(f.projectID.String())}}}
	require.NoError(t, authz.PatchPrincipalGrants(t.Context(), f.pool, f.orgID, urn.NewAPIKeyPrincipal(key), nil, keyPolicy))

	again := f.run(true)
	require.Zero(t, again.GrantsWritten)
	require.Empty(t, f.grantsOf(authz.AllUsersPrincipal()), "a removed default stays removed")
	require.Empty(t, f.grantsOf(urn.NewAPIKeyPrincipal(key)), "a removed key policy stays removed")
}

func TestRun_RecordsWhatItProvisions(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.apiKey(uuid.NullUUID{}, "consumer")
	report := f.run(true)

	kinds := map[string]int{}
	for _, row := range f.provisions() {
		kinds[row.Kind]++
	}
	require.Equal(t, map[string]int{string(GrantPersonalDefault): 1, string(GrantAPIKey): 1}, kinds)
	require.Equal(t, 2, report.GrantsWritten)
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

func TestRun_SharedClientGrantsNeverReachOtherClients(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	project := f.projectID.String()
	serverA, clientA := f.sharedClient()
	serverB, clientB := f.sharedClient()

	concreteUser := urn.NewPrincipal(urn.PrincipalTypeUser, "user_concrete_a")
	wildcardUser := urn.NewPrincipal(urn.PrincipalTypeUser, "user_project_wide")
	orgRole := urn.NewPrincipal(urn.PrincipalTypeRole, "org_wide")
	f.grant(concreteUser, authz.ScopeMCPConnect, authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: serverA.String()})
	f.grant(wildcardUser, authz.ScopeMCPConnect, authz.ProjectServersSelector(project))
	f.grant(orgRole, authz.ScopeMCPConnect, authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: authz.WildcardResource})
	f.run(true)

	session := "session_1"
	userCtx := func(principal urn.Principal, grants []authz.Grant) context.Context {
		ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: f.orgID, UserID: principal.ID, SessionID: &session, AccountType: "enterprise"})
		return authz.GrantsToContext(ctx, grants)
	}
	useClient := func(clientID uuid.UUID) authz.Check {
		return authz.MCPAuthClientCheck(clientID.String(), authz.MCPAuthClientDimensions{ClientType: TypeServiceAccount, ProjectID: project, ConnectionOwner: ""})
	}
	// A client created after the backfill, which no run has seen.
	later := uuid.New()

	for _, principal := range []urn.Principal{concreteUser, wildcardUser, orgRole} {
		for _, grant := range f.grantsOf(principal) {
			if grant.Scope == authz.ScopeMCPAuth {
				require.NotEqual(t, authz.WildcardResource, grant.Selector.ResourceID(), "%s: provisioned grants name clients", principal)
			}
		}
	}

	require.True(t, mandatory(t, userCtx(concreteUser, f.grantsOf(concreteUser)), f, useClient(clientA)).Allowed, "the user keeps today's reach")
	require.False(t, mandatory(t, userCtx(concreteUser, f.grantsOf(concreteUser)), f, useClient(clientB)).Allowed, "access to server A is not use of server B's client")
	require.False(t, mandatory(t, userCtx(concreteUser, f.grantsOf(concreteUser)), f, useClient(later)).Allowed, "nor of a client created later")

	require.True(t, mandatory(t, userCtx(wildcardUser, f.grantsOf(wildcardUser)), f, useClient(clientA), useClient(clientB)).Allowed, "a project-wide grant reaches today's clients")
	require.False(t, mandatory(t, userCtx(wildcardUser, f.grantsOf(wildcardUser)), f, useClient(later)).Allowed, "a wildcard grant never pre-authorizes later clients")

	member := urn.NewPrincipal(urn.PrincipalTypeUser, "member")
	roleGrants := f.grantsOf(orgRole)
	require.True(t, mandatory(t, userCtx(member, roleGrants), f, useClient(clientA), useClient(clientB)).Allowed)
	require.False(t, mandatory(t, userCtx(member, roleGrants), f, useClient(later)).Allowed, "an organization-wide grant never becomes a client-type grant")

	// Granting server access afterwards grants no use of its client.
	f.grant(concreteUser, authz.ScopeMCPConnect, authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: serverB.String()})
	grants := f.grantsOf(concreteUser)
	require.True(t, mandatory(t, userCtx(concreteUser, grants), f, authz.MCPCheck(authz.ScopeMCPConnect, serverB.String(), project)).Allowed)
	require.False(t, mandatory(t, userCtx(concreteUser, grants), f, useClient(clientB)).Allowed, "connect access to B does not grant B's upstream identity")
	require.False(t, mandatory(t, userCtx(concreteUser, grants), f, useClient(later)).Allowed)
}

func TestRun_TakenOverUserGrantsOutrankInheritedBlocks(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	serverID, clientID := f.sharedClient()
	user := urn.NewPrincipal(urn.PrincipalTypeUser, "user_taken_over")
	role := urn.NewPrincipal(urn.PrincipalTypeRole, "blocks_taken_over")
	f.grant(user, authz.ScopeMCPConnect, authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: serverID.String()})
	f.run(true)
	f.grant(role, authz.ScopeMCPBlockedAuth, authz.AuthClientSelector(clientID.String(), ""))

	session := "session_1"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: f.orgID, UserID: user.ID, SessionID: &session, AccountType: "enterprise"})
	check := authz.MCPAuthClientCheck(clientID.String(), authz.MCPAuthClientDimensions{ClientType: TypeServiceAccount, ProjectID: f.projectID.String(), ConnectionOwner: ""})
	require.False(t, mandatory(t, authz.GrantsToContext(ctx, append(f.grantsOf(user), f.grantsOf(role)...)), f, check).Allowed, "the provisioned grant yields to the block")

	// An administrator granting the same client to the user by name makes the
	// grant theirs, and a direct grant outranks the inherited block.
	explicit := []*authz.RoleGrant{{Scope: string(authz.ScopeMCPAuth), Selectors: []authz.Selector{authz.AuthClientSelector(clientID.String(), "")}}}
	require.NoError(t, authz.PatchPrincipalGrants(t.Context(), f.pool, f.orgID, user, explicit, nil))
	require.True(t, mandatory(t, authz.GrantsToContext(ctx, append(f.grantsOf(user), f.grantsOf(role)...)), f, check).Allowed)
}

func TestRun_NeverRestoresAGrantTakenOverThenRemoved(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	key := f.apiKey(uuid.NullUUID{}, "consumer")
	f.run(true)

	for principal, grant := range map[urn.Principal]*authz.RoleGrant{
		authz.AllUsersPrincipal():   {Scope: string(authz.ScopeMCPAuth), Selectors: []authz.Selector{authz.PersonalAuthClientSelector()}},
		urn.NewAPIKeyPrincipal(key): {Scope: string(authz.ScopeMCPConnect), Selectors: []authz.Selector{authz.ProjectServersSelector(f.projectID.String())}},
	} {
		grants := []*authz.RoleGrant{grant}
		require.NoError(t, authz.PatchPrincipalGrants(t.Context(), f.pool, f.orgID, principal, grants, nil))
		require.NoError(t, authz.PatchPrincipalGrants(t.Context(), f.pool, f.orgID, principal, nil, grants))
	}

	f.run(true)
	require.Empty(t, f.grantsOf(authz.AllUsersPrincipal()))
	require.Empty(t, f.grantsOf(urn.NewAPIKeyPrincipal(key)))
}

func TestRun_TrimmedKeyPoliciesAreNotToppedUp(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	key := f.apiKey(uuid.NullUUID{}, "consumer")
	_, clientID := f.sharedClient()
	principal := urn.NewAPIKeyPrincipal(key)
	trimmed := []*authz.RoleGrant{{Scope: string(authz.ScopeMCPAuth), Selectors: []authz.Selector{authz.AuthClientSelector(clientID.String(), f.projectID.String())}}}
	require.NoError(t, authz.PatchPrincipalGrants(t.Context(), f.pool, f.orgID, principal, nil, trimmed))
	before := f.grantsOf(principal)
	require.Len(t, before, 1)

	f.sharedClient()
	require.Equal(t, before, f.grantsOf(principal), "an admin trimmed this policy, so it is theirs")
}

func TestRun_NeverRestoresARemovedCreationDefault(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	require.NoError(t, authz.SeedPersonalAuthClientDefaultTx(t.Context(), f.pool, f.orgID))
	personal := []*authz.RoleGrant{{Scope: string(authz.ScopeMCPAuth), Selectors: []authz.Selector{authz.PersonalAuthClientSelector()}}}
	require.NoError(t, authz.PatchPrincipalGrants(t.Context(), f.pool, f.orgID, authz.AllUsersPrincipal(), nil, personal))

	f.run(true)
	require.Empty(t, f.grantsOf(authz.AllUsersPrincipal()), "the creation-time default is provisioned too, so its removal sticks")
}
