package agent_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/agent"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/plugins/naming"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// ptr takes the address of a literal. conv.PtrEmpty collapses the zero value
// to nil, which is exactly what the blank-serial cases below need to send.
//
//go:fix inline
func ptr[T any](v T) *T { return new(v) }

// wantMarketplace / wantObservability derive the expected names from the same
// helpers the publish path uses, so the test pins the actual cross-surface
// contract rather than a hardcoded string.
var (
	// The test instance's project is its org's default (oldest) project, so it
	// keeps the bare org-derived name; slug is irrelevant when isDefault is true.
	wantMarketplace   = naming.MarketplaceName(mockidp.MockOrgName, "", true) // local-dev-org-speakeasy
	wantObservability = naming.ObservabilitySlug(mockidp.MockOrgName)         // local-dev-org-observability
)

func pluginSlugs(res *gen.GetPluginsResult) []string {
	out := make([]string, 0, len(res.Plugins))
	for _, p := range res.Plugins {
		out = append(out, p.Slug)
	}
	return out
}

func TestGetPlugins_ObservabilityWithoutAssignments(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)

	require.Len(t, res.Marketplaces, 1)
	require.Equal(t, wantMarketplace, res.Marketplaces[0].Name)
	require.Equal(t, []string{wantObservability}, pluginSlugs(res),
		"a published marketplace always yields observability, even with no assignments")
}

// TestGetPlugins_ScopesToAssignedPrincipals pins per-principal scoping (DNO-239):
// a plugin is delivered only when its assignment matches the caller's resolved
// principal set. Unassigned plugins, and plugins assigned to a different
// principal, are withheld; the always-required observability plugin still ships.
func TestGetPlugins_ScopesToAssignedPrincipals(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	// A plugin with no assignment at all — must not be delivered.
	seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "unassigned-tool")
	// A plugin assigned only to a *different* principal — must not be delivered.
	other := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "someone-elses-tool")
	assignPlugin(t, ctx, ti.conn, other, ti.orgID, "email:someone-else@example.com")

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)

	require.Len(t, res.Marketplaces, 1)
	require.Equal(t, []string{wantObservability}, pluginSlugs(res),
		"only observability ships; unassigned and other-principal plugins are withheld")
}

// TestGetPlugins_DeliversEmailAndWildcardAssignments covers the two principal
// forms that work without member resolution: an assignment to the caller's exact
// email and an assignment to the org wildcard both deliver.
func TestGetPlugins_DeliversEmailAndWildcardAssignments(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	emailTool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "email-tool")
	assignPlugin(t, ctx, ti.conn, emailTool, ti.orgID, "email:"+mockidp.MockUserEmail)
	wildcardTool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "wildcard-tool")
	assignPlugin(t, ctx, ti.conn, wildcardTool, ti.orgID, "*")

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)

	require.ElementsMatch(t,
		[]string{wantObservability, "email-tool", "wildcard-tool"},
		pluginSlugs(res),
		"email- and wildcard-scoped assignments both deliver")
}

// TestGetPlugins_DeliversUserAndRoleAssignments covers the RBAC-resolved forms
// (DNO-239): assignments to the member's user:<id> and to a role they belong to
// deliver, and neither reaches a non-member. Observability always ships.
func TestGetPlugins_DeliversUserAndRoleAssignments(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")

	userTool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "user-tool")
	assignPlugin(t, ctx, ti.conn, userTool, ti.orgID, "user:"+mockidp.MockUserID)

	roleTool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "role-tool")
	roleURN := assignUserToRole(t, ctx, ti.conn, ti.orgID, mockidp.MockUserID, "engineering")
	assignPlugin(t, ctx, ti.conn, roleTool, ti.orgID, roleURN)

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)
	require.ElementsMatch(t,
		[]string{wantObservability, "user-tool", "role-tool"},
		pluginSlugs(res),
		"user:<id> and role assignments deliver to the resolved member")

	// A non-member email resolves to no user, so it gets neither the user- nor
	// role-scoped plugin — only observability.
	nonMember, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("stranger@example.com")})
	require.NoError(t, err)
	require.Equal(t, []string{wantObservability}, pluginSlugs(nonMember),
		"user:/role: assignments are withheld from non-members")
}

func TestGetPlugins_DeliversDirectoryGroupAssignmentsToUnlinkedUsers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	group := seedDirectoryGroup(t, ctx, ti.conn, ti.orgID, "group-directory-audience")
	user := seedDirectoryUser(t, ctx, ti.conn, ti.orgID, "user-directory-audience", "Unlinked@Example.com")
	seedDirectoryGroupMembership(t, ctx, ti.conn, user, group, "user-directory-audience", "group-directory-audience")
	plugin := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "directory-group-tool")
	assignPlugin(t, ctx, ti.conn, plugin, ti.orgID, plugins.DirectoryGroupPrincipal(group))

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("unlinked@example.com")})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{wantObservability, "directory-group-tool"}, pluginSlugs(res))
}

func TestGetPlugins_DeliversDirectoryAttributeAssignments(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")

	const email = "directory-attributes@example.com"
	seedDirectoryUserWithAttributes(t, ctx, ti.conn, ti.orgID, "user-directory-attributes-1", email, []byte(`{"department":"Engineering"}`))
	seedDirectoryUserWithAttributes(t, ctx, ti.conn, ti.orgID, "user-directory-attributes-2", email, []byte(`{"manager.email":"lead@example.com"}`))
	seedDirectoryUserWithAttributes(t, ctx, ti.conn, ti.orgID, "user-directory-attributes-3", email, []byte(`{"department":"Engineering"}`))
	seedDirectoryUserWithAttributes(t, ctx, ti.conn, ti.orgID, "user-directory-attributes-null", email, []byte(`{"department":null}`))
	audiences, err := plugins.ResolveDirectoryAudiencePrincipalsByEmails(ctx, ti.conn, ti.orgID, []string{email})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{
		plugins.DirectoryAttributePrincipal("department", "Engineering"),
		plugins.DirectoryAttributePrincipal("manager.email", "lead@example.com"),
	}, audiences[email])
	departmentPlugin := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "department-tool")
	assignPlugin(t, ctx, ti.conn, departmentPlugin, ti.orgID, plugins.DirectoryAttributePrincipal("department", "Engineering"))
	managerPlugin := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "manager-tool")
	assignPlugin(t, ctx, ti.conn, managerPlugin, ti.orgID, plugins.DirectoryAttributePrincipal("manager.email", "lead@example.com"))

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(email)})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{wantObservability, "department-tool", "manager-tool"}, pluginSlugs(res))
}

func TestGetPlugins_RefreshesDirectoryGroupMembership(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")

	const email = "directory-user@example.com"
	alpha := seedDirectoryGroup(t, ctx, ti.conn, ti.orgID, "group-alpha")
	beta := seedDirectoryGroup(t, ctx, ti.conn, ti.orgID, "group-beta")
	gamma := seedDirectoryGroup(t, ctx, ti.conn, ti.orgID, "group-gamma")
	alphaUser := seedDirectoryUser(t, ctx, ti.conn, ti.orgID, "user-alpha", email)
	betaUser := seedDirectoryUser(t, ctx, ti.conn, ti.orgID, "user-beta", email)
	gammaUser := seedDirectoryUser(t, ctx, ti.conn, ti.orgID, "user-gamma", email)
	seedDirectoryGroupMembership(t, ctx, ti.conn, alphaUser, alpha, "user-alpha", "group-alpha")
	seedDirectoryGroupMembership(t, ctx, ti.conn, betaUser, beta, "user-beta", "group-beta")
	seedDirectoryGroupMembership(t, ctx, ti.conn, gammaUser, gamma, "user-gamma", "group-gamma")

	for _, assignment := range []struct {
		group uuid.UUID
		slug  string
	}{{alpha, "alpha-tool"}, {beta, "beta-tool"}, {gamma, "gamma-tool"}} {
		plugin := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, assignment.slug)
		assignPlugin(t, ctx, ti.conn, plugin, ti.orgID, plugins.DirectoryGroupPrincipal(assignment.group))
	}

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(email)})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{wantObservability, "alpha-tool", "beta-tool", "gamma-tool"}, pluginSlugs(res))

	_, err = directoryrepo.New(ti.conn).CloseDirectoryUserGroupMembership(ctx, directoryrepo.CloseDirectoryUserGroupMembershipParams{
		DirectoryUserID:  alphaUser,
		DirectoryGroupID: alpha,
	})
	require.NoError(t, err)
	res, err = ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(email)})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{wantObservability, "beta-tool", "gamma-tool"}, pluginSlugs(res))

	_, err = directoryrepo.New(ti.conn).DeleteDirectoryGroupByWorkOSID(ctx, directoryrepo.DeleteDirectoryGroupByWorkOSIDParams{
		WorkosDirectoryGroupID: "group-beta",
		WorkosDeletedAt:        conv.ToPGTimestamptz(time.Now().UTC()),
		WorkosLastEventID:      conv.ToPGText("event-group-beta-deleted"),
	})
	require.NoError(t, err)
	res, err = ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(email)})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{wantObservability, "gamma-tool"}, pluginSlugs(res))

	_, err = directoryrepo.New(ti.conn).DeleteDirectoryUserByWorkOSID(ctx, directoryrepo.DeleteDirectoryUserByWorkOSIDParams{
		WorkosDirectoryUserID: "user-gamma",
		WorkosDeletedAt:       conv.ToPGTimestamptz(time.Now().UTC()),
		WorkosLastEventID:     conv.ToPGText("event-user-gamma-deleted"),
	})
	require.NoError(t, err)
	res, err = ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(email)})
	require.NoError(t, err)
	require.Equal(t, []string{wantObservability}, pluginSlugs(res))
}

func TestGetPlugins_DirectoryGroupsAreOrganizationScoped(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	seedSecondOrg(t, ctx, ti.conn)

	group := seedDirectoryGroup(t, ctx, ti.conn, "other-org-id", "group-other-org")
	user := seedDirectoryUser(t, ctx, ti.conn, "other-org-id", "user-other-org", "directory-user@example.com")
	seedDirectoryGroupMembership(t, ctx, ti.conn, user, group, "user-other-org", "group-other-org")
	plugin := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "other-org-group-tool")
	assignPlugin(t, ctx, ti.conn, plugin, ti.orgID, plugins.DirectoryGroupPrincipal(group))

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("directory-user@example.com")})
	require.NoError(t, err)
	require.Equal(t, []string{wantObservability}, pluginSlugs(res))
}

func TestGetPlugins_UnpublishedProjectExcluded(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	// The project has a plugin but no marketplace_token (never published) — so
	// nothing is installable and the endpoint returns empty.
	seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "unpublished-tool")

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)

	require.Empty(t, res.Marketplaces)
	require.Empty(t, res.Plugins)
}

// TestGetPlugins_MultiProjectDistinctByDefault covers project-scoped naming: an
// org with multiple published projects surfaces each as its own marketplace. The
// default project (from InitAuthContext, the org's oldest) keeps the bare
// org-derived name; a non-default project is scoped by its slug. Both are
// returned, each with its own token. The non-default project has an assignment
// for the caller so it surfaces at all under the default-plus-assigned scoping.
func TestGetPlugins_MultiProjectDistinctByDefault(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "default-token")

	beta := seedProject(t, ctx, ti.conn, ti.orgID, "beta")
	publishMarketplace(t, ctx, ti.conn, beta, "beta-token")
	// A wildcard-assigned plugin makes the non-default project surface for the
	// caller (a non-default project with no assignment is intentionally hidden).
	betaTool := seedPlugin(t, ctx, ti.conn, ti.orgID, beta, "beta-tool")
	assignPlugin(t, ctx, ti.conn, betaTool, ti.orgID, "*")
	wantBeta := naming.MarketplaceName(mockidp.MockOrgName, "beta", false) // local-dev-org-beta-speakeasy

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)

	require.Len(t, res.Marketplaces, 2, "distinct project-scoped names do not collapse")
	byName := make(map[string]string, len(res.Marketplaces))
	for _, m := range res.Marketplaces {
		byName[m.Name] = m.URL
	}
	require.Contains(t, byName, wantMarketplace, "default project keeps the bare org name")
	require.Contains(t, byName, wantBeta, "non-default project is scoped by slug")
	require.Contains(t, byName[wantMarketplace], "default-token")
	require.Contains(t, byName[wantBeta], "beta-token")
}

// TestGetPlugins_CollidingNamesPreferDefault pins the DNO-228 tiebreak that still
// matters when names genuinely collide: if a non-default project's override is
// set to the default project's name, they can't both exist on the device, so the
// view collapses them and the `ORDER BY pr.id` ordering keeps the *default*
// project's token (not whichever sorts first by slug).
func TestGetPlugins_CollidingNamesPreferDefault(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "default-token")

	// "beta" sorts before the default project's "test-<hex>" slug, but overrides
	// its name to collide with the default's bare org name. Its plugin — assigned
	// to the caller so scoping would otherwise deliver it — must NOT leak onto the
	// winning marketplace, since beta's repo isn't the one served under that name.
	beta := seedProject(t, ctx, ti.conn, ti.orgID, "beta")
	setMarketplaceOverride(t, ctx, ti.conn, beta, wantMarketplace)
	publishMarketplace(t, ctx, ti.conn, beta, "beta-token")
	betaTool := seedPlugin(t, ctx, ti.conn, ti.orgID, beta, "beta-only-tool")
	assignPlugin(t, ctx, ti.conn, betaTool, ti.orgID, "*")

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)

	require.Len(t, res.Marketplaces, 1, "colliding names collapse to one")
	require.Equal(t, wantMarketplace, res.Marketplaces[0].Name)
	require.Contains(t, res.Marketplaces[0].URL, "default-token",
		"the default project's token must win the collision, not the alphabetically-first one")
	require.NotContains(t, res.Marketplaces[0].URL, "beta-token")
	require.NotContains(t, pluginSlugs(res), "beta-only-tool",
		"the collapsed project's plugin must not be attached to the winning marketplace")
}

// TestGetPlugins_DistinctOverridesYieldSeparateMarketplaces covers the
// multi-marketplace path: when projects publish under *distinct* names (via the
// per-project override), each surfaces as its own marketplace instead of
// collapsing. Also pins that the agent honors the override at all — recomputing
// the org default would emit a name the project never published under.
func TestGetPlugins_DistinctOverridesYieldSeparateMarketplaces(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	// Default project keeps the org-derived name.
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "default-token")

	// A second project published under a distinct override name, with a
	// wildcard-assigned plugin so it surfaces under the default-plus-assigned
	// scoping (a non-default project with no assignment is hidden).
	beta := seedProject(t, ctx, ti.conn, ti.orgID, "beta")
	setMarketplaceOverride(t, ctx, ti.conn, beta, "team-beta")
	publishMarketplace(t, ctx, ti.conn, beta, "beta-token")
	betaTool := seedPlugin(t, ctx, ti.conn, ti.orgID, beta, "beta-tool")
	assignPlugin(t, ctx, ti.conn, betaTool, ti.orgID, "*")

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)

	require.Len(t, res.Marketplaces, 2, "distinct names must not collapse")

	byName := make(map[string]string, len(res.Marketplaces))
	for _, m := range res.Marketplaces {
		byName[m.Name] = m.URL
	}
	require.Contains(t, byName, wantMarketplace, "default project keeps the org-derived name")
	require.Contains(t, byName, "team-beta", "override project surfaces under its published name")
	require.Contains(t, byName[wantMarketplace], "default-token")
	require.Contains(t, byName["team-beta"], "beta-token")
}

// TestGetPlugins_NonDefaultProjectWithoutAssignmentHidden pins the
// default-plus-assigned scoping: a published non-default project the caller has
// no assignment in is omitted entirely — its marketplace and always-on
// observability plugin would otherwise flood the device agent — while the
// default project still surfaces as the org-wide baseline.
func TestGetPlugins_NonDefaultProjectWithoutAssignmentHidden(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "default-token")

	// A published non-default project with no assignment for the caller.
	beta := seedProject(t, ctx, ti.conn, ti.orgID, "beta")
	publishMarketplace(t, ctx, ti.conn, beta, "beta-token")
	wantBeta := naming.MarketplaceName(mockidp.MockOrgName, "beta", false)

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)

	require.Len(t, res.Marketplaces, 1, "only the default project surfaces")
	require.Equal(t, wantMarketplace, res.Marketplaces[0].Name)
	names := make([]string, 0, len(res.Marketplaces))
	for _, m := range res.Marketplaces {
		names = append(names, m.Name)
	}
	require.NotContains(t, names, wantBeta, "an unassigned non-default project is hidden")
	require.Equal(t, []string{wantObservability}, pluginSlugs(res),
		"only the default project's observability ships, not beta's")
}

func TestGetPlugins_CrossOrgIsolation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	// Caller's org has its own published marketplace.
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	// A different org has a published marketplace + a wildcard-assigned plugin.
	seedSecondOrg(t, ctx, ti.conn)

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)

	require.Len(t, res.Marketplaces, 1, "only the caller's org marketplace")
	require.Equal(t, wantMarketplace, res.Marketplaces[0].Name)
	for _, m := range res.Marketplaces {
		require.NotEqual(t, "other-org-speakeasy", m.Name)
	}
	require.NotContains(t, pluginSlugs(res), "other-plugin", "another org's plugin must not leak")
}

// TestGetPlugins_IgnoresMismatchedOrgAssignment pins the assignment tenant
// boundary on the read path: a plugin in the caller's org whose only assignment
// row is stamped with a *different* organization_id (a stale or manually
// backfilled anomaly) is not delivered, even though its principal ("*") is in
// every caller's resolved set. The getPlugins EXISTS scopes on
// pa.organization_id, so the mismatched row can't change delivery. The row is
// inserted raw because AddPluginAssignment is org-scoped and refuses to create
// such a row.
func TestGetPlugins_IgnoresMismatchedOrgAssignment(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	// Creates the "other-org-id" org so the assignment's organization_id FK holds.
	seedSecondOrg(t, ctx, ti.conn)

	stale := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "stale-tool")
	// Raw fixture: the org-scoped AddPluginAssignment can't create a row whose
	// organization_id differs from the plugin's org, which is exactly the anomaly
	// under test.
	err := testrepo.New(ti.conn).InsertPluginAssignmentFixture(ctx, testrepo.InsertPluginAssignmentFixtureParams{
		PluginID:       stale,
		OrganizationID: "other-org-id",
		PrincipalUrn:   "*",
	})
	require.NoError(t, err)

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)
	require.Equal(t, []string{wantObservability}, pluginSlugs(res),
		"an assignment row stamped with a different org must not deliver the plugin")
}

func TestGetPlugins_InvalidEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	// The default context is an org install key, so the vouched email is
	// authoritative and a missing or empty one is rejected.
	_, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: nil})
	require.Error(t, err, "absent email header must be rejected")

	_, err = ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("")})
	require.Error(t, err, "empty email header must be rejected")
}

// TestGetPlugins_LegacyEmailParamFallback pins the compatibility contract for
// deployed org-key agents that predate the Gram-User-Email header: the
// deprecated ?email= query param still vouches, and the header wins when both
// are present.
func TestGetPlugins_LegacyEmailParamFallback(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	headerTool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "header-tool")
	assignPlugin(t, ctx, ti.conn, headerTool, ti.orgID, "email:header@example.com")
	legacyTool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "legacy-tool")
	assignPlugin(t, ctx, ti.conn, legacyTool, ti.orgID, "email:legacy@example.com")

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{LegacyEmail: new("legacy@example.com")})
	require.NoError(t, err, "param-only vouching must keep working for deployed agents")
	require.Contains(t, pluginSlugs(res), "legacy-tool")

	res, err = ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{
		Email:       new("header@example.com"),
		LegacyEmail: new("legacy@example.com"),
	})
	require.NoError(t, err)
	require.Contains(t, pluginSlugs(res), "header-tool")
	require.NotContains(t, pluginSlugs(res), "legacy-tool", "the header must win over the deprecated param")
}

// TestGetPlugins_PerUserKeyBindsToOwner pins the DNO-383 scope-aware attribution:
// a per-user `agent_user` key resolves the polling identity from the
// authenticated key owner, so a vouched email header is ignored — a leaked
// per-user key cannot claim another member's plugins.
func TestGetPlugins_PerUserKeyBindsToOwner(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	// A per-user key owned by the mock member; the vouched email below differs.
	ctx = withPerUserKeyAuth(t, ctx, mockidp.MockUserEmail)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("someone-else@example.com")})
	require.NoError(t, err, "per-user key ignores the vouched email and uses its owner")
	require.Len(t, res.Marketplaces, 1)
}

// TestGetPlugins_RecordsDeviceSync covers the per-device heartbeat that lets
// coverage attest a specific machine rather than its assigned user.
func TestGetPlugins_RecordsDeviceSync(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")

	_, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{
		Email:        new(mockidp.MockUserEmail),
		SerialNumber: new("C02XK1ABCDEF"),
		Hostname:     new("dev-macbook-pro"),
	})
	require.NoError(t, err)

	rows, err := testrepo.New(ti.conn).ListDeviceAgentDeviceSyncsFixture(ctx, ti.orgID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "c02xk1abcdef", rows[0].SerialNumber,
		"stored lowercased so the value agrees with its own LOWER() dedup key and the coverage joins")
	require.Equal(t, conv.NormalizeEmail(mockidp.MockUserEmail), rows[0].Email)
	require.Equal(t, "dev-macbook-pro", rows[0].Hostname.String)
}

// TestGetPlugins_DeviceSyncSkippedWithoutSerial pins the graceful-degradation
// contract: agents predating the capability, and machines with no readable
// serial, must still sync and simply fall back to user-level coverage.
func TestGetPlugins_DeviceSyncSkippedWithoutSerial(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")

	for _, serial := range []*string{nil, new(""), new("   ")} {
		_, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{
			Email:        new(mockidp.MockUserEmail),
			SerialNumber: serial,
			Hostname:     nil,
		})
		require.NoError(t, err, "a missing serial must never fail the sync")
	}

	rows, err := testrepo.New(ti.conn).ListDeviceAgentDeviceSyncsFixture(ctx, ti.orgID)
	require.NoError(t, err)
	require.Empty(t, rows, "no serial means no device row")

	// The user-level heartbeat still lands, which is what coverage falls back to.
	userRows, err := agentrepo.New(ti.conn).ListDeviceAgentSyncs(ctx, ti.orgID)
	require.NoError(t, err)
	require.Len(t, userRows, 1)
}

// TestGetPlugins_DeviceSyncReassignmentBeatsThrottle is the regression test for
// the throttle trap: last_seen_at is almost always fresh at a ~60s poll, so a
// guard that only checked it would leave a reassigned machine attributed to its
// previous owner for the whole session.
func TestGetPlugins_DeviceSyncReassignmentBeatsThrottle(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")

	const serial = "C02XK1ABCDEF"
	_, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{
		Email:        new(mockidp.MockUserEmail),
		SerialNumber: ptr(serial),
		Hostname:     new("old-name"),
	})
	require.NoError(t, err)

	rows, err := testrepo.New(ti.conn).ListDeviceAgentDeviceSyncsFixture(ctx, ti.orgID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	firstSeen := rows[0].LastSeenAt.Time

	// Same machine, same user, immediately: the heartbeat throttle holds.
	_, err = ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{
		Email:        new(mockidp.MockUserEmail),
		SerialNumber: ptr(serial),
		Hostname:     new("old-name"),
	})
	require.NoError(t, err)
	rows, err = testrepo.New(ti.conn).ListDeviceAgentDeviceSyncsFixture(ctx, ti.orgID)
	require.NoError(t, err)
	require.Len(t, rows, 1, "same serial must not create a second row")
	require.Equal(t, firstSeen, rows[0].LastSeenAt.Time,
		"an unchanged poll inside the 1-minute guard must not advance last_seen_at")

	// A rename inside the same window must land despite the throttle.
	_, err = ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{
		Email:        new(mockidp.MockUserEmail),
		SerialNumber: ptr(serial),
		Hostname:     new("new-name"),
	})
	require.NoError(t, err)
	rows, err = testrepo.New(ti.conn).ListDeviceAgentDeviceSyncsFixture(ctx, ti.orgID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "new-name", rows[0].Hostname.String,
		"a changed hostname must not wait out the heartbeat throttle")

	// Dropping the hostname must not blank a known one.
	_, err = ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{
		Email:        new(mockidp.MockUserEmail),
		SerialNumber: ptr(serial),
		Hostname:     nil,
	})
	require.NoError(t, err)
	rows, err = testrepo.New(ti.conn).ListDeviceAgentDeviceSyncsFixture(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, "new-name", rows[0].Hostname.String,
		"an agent that stops reporting a hostname must not erase the stored one")
}

// TestGetPlugins_DeviceSyncNormalizesSerialCase pins the write-side
// normalization. The dedup key and every coverage reader compare
// LOWER(serial_number), so a machine reporting two casings must resolve to ONE
// row — otherwise both would match its device and fan out its coverage.
func TestGetPlugins_DeviceSyncNormalizesSerialCase(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")

	for _, reported := range []string{"C02XK1ABCDEF", "c02xk1abcdef", "  C02xk1AbCdEf  "} {
		_, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{
			Email:        new(mockidp.MockUserEmail),
			SerialNumber: new(reported),
			Hostname:     nil,
		})
		require.NoError(t, err)
	}

	rows, err := testrepo.New(ti.conn).ListDeviceAgentDeviceSyncsFixture(ctx, ti.orgID)
	require.NoError(t, err)
	require.Len(t, rows, 1, "casing variants of one machine's serial must collapse to a single row")
	require.Equal(t, "c02xk1abcdef", rows[0].SerialNumber, "stored canonically, matching the LOWER() dedup key")
}

// TestGetPlugins_DeviceSyncRejectsPlaceholderSerials guards the compliance
// claim: white-box hardware reports SMBIOS defaults verbatim, so many DIFFERENT
// machines can share one "serial". Storing a heartbeat under it would let one
// agent install attest all of them as device-verified.
func TestGetPlugins_DeviceSyncRejectsPlaceholderSerials(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")

	for _, placeholder := range []string{
		"To Be Filled By O.E.M.",
		"Default string",
		"System Serial Number",
		"0",
		"none",
		"N/A",
	} {
		_, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{
			Email:        new(mockidp.MockUserEmail),
			SerialNumber: new(placeholder),
			Hostname:     nil,
		})
		require.NoError(t, err, "a placeholder serial must never fail the sync")
	}

	rows, err := testrepo.New(ti.conn).ListDeviceAgentDeviceSyncsFixture(ctx, ti.orgID)
	require.NoError(t, err)
	require.Empty(t, rows, "placeholder serials are not device identities; coverage falls back to the email match")

	// The user-level heartbeat still lands, so these devices stay covered at
	// the weaker attestation rather than dropping out entirely.
	userRows, err := agentrepo.New(ti.conn).ListDeviceAgentSyncs(ctx, ti.orgID)
	require.NoError(t, err)
	require.Len(t, userRows, 1)
}
