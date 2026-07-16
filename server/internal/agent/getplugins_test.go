package agent_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/plugins/naming"
)

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

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: mockidp.MockUserEmail})
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

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: mockidp.MockUserEmail})
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

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: mockidp.MockUserEmail})
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

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: mockidp.MockUserEmail})
	require.NoError(t, err)
	require.ElementsMatch(t,
		[]string{wantObservability, "user-tool", "role-tool"},
		pluginSlugs(res),
		"user:<id> and role assignments deliver to the resolved member")

	// A non-member email resolves to no user, so it gets neither the user- nor
	// role-scoped plugin — only observability.
	nonMember, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: "stranger@example.com"})
	require.NoError(t, err)
	require.Equal(t, []string{wantObservability}, pluginSlugs(nonMember),
		"user:/role: assignments are withheld from non-members")
}

func TestGetPlugins_UnpublishedProjectExcluded(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	// The project has a plugin but no marketplace_token (never published) — so
	// nothing is installable and the endpoint returns empty.
	seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "unpublished-tool")

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: mockidp.MockUserEmail})
	require.NoError(t, err)

	require.Empty(t, res.Marketplaces)
	require.Empty(t, res.Plugins)
}

// TestGetPlugins_MultiProjectDistinctByDefault covers project-scoped naming: an
// org with multiple published projects surfaces each as its own marketplace. The
// default project (from InitAuthContext, the org's oldest) keeps the bare
// org-derived name; a non-default project is scoped by its slug. Both are
// returned, each with its own token.
func TestGetPlugins_MultiProjectDistinctByDefault(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "default-token")

	adam := seedProject(t, ctx, ti.conn, ti.orgID, "adam")
	publishMarketplace(t, ctx, ti.conn, adam, "adam-token")
	wantAdam := naming.MarketplaceName(mockidp.MockOrgName, "adam", false) // local-dev-org-adam-speakeasy

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: mockidp.MockUserEmail})
	require.NoError(t, err)

	require.Len(t, res.Marketplaces, 2, "distinct project-scoped names do not collapse")
	byName := make(map[string]string, len(res.Marketplaces))
	for _, m := range res.Marketplaces {
		byName[m.Name] = m.URL
	}
	require.Contains(t, byName, wantMarketplace, "default project keeps the bare org name")
	require.Contains(t, byName, wantAdam, "non-default project is scoped by slug")
	require.Contains(t, byName[wantMarketplace], "default-token")
	require.Contains(t, byName[wantAdam], "adam-token")
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

	// "adam" sorts before the default project's "test-<hex>" slug, but overrides
	// its name to collide with the default's bare org name. Its plugin — assigned
	// to the caller so scoping would otherwise deliver it — must NOT leak onto the
	// winning marketplace, since adam's repo isn't the one served under that name.
	adam := seedProject(t, ctx, ti.conn, ti.orgID, "adam")
	setMarketplaceOverride(t, ctx, ti.conn, adam, wantMarketplace)
	publishMarketplace(t, ctx, ti.conn, adam, "adam-token")
	adamTool := seedPlugin(t, ctx, ti.conn, ti.orgID, adam, "adam-only-tool")
	assignPlugin(t, ctx, ti.conn, adamTool, ti.orgID, "*")

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: mockidp.MockUserEmail})
	require.NoError(t, err)

	require.Len(t, res.Marketplaces, 1, "colliding names collapse to one")
	require.Equal(t, wantMarketplace, res.Marketplaces[0].Name)
	require.Contains(t, res.Marketplaces[0].URL, "default-token",
		"the default project's token must win the collision, not the alphabetically-first one")
	require.NotContains(t, res.Marketplaces[0].URL, "adam-token")
	require.NotContains(t, pluginSlugs(res), "adam-only-tool",
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

	// A second project published under a distinct override name.
	adam := seedProject(t, ctx, ti.conn, ti.orgID, "adam")
	setMarketplaceOverride(t, ctx, ti.conn, adam, "team-adam")
	publishMarketplace(t, ctx, ti.conn, adam, "adam-token")

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: mockidp.MockUserEmail})
	require.NoError(t, err)

	require.Len(t, res.Marketplaces, 2, "distinct names must not collapse")

	byName := make(map[string]string, len(res.Marketplaces))
	for _, m := range res.Marketplaces {
		byName[m.Name] = m.URL
	}
	require.Contains(t, byName, wantMarketplace, "default project keeps the org-derived name")
	require.Contains(t, byName, "team-adam", "override project surfaces under its published name")
	require.Contains(t, byName[wantMarketplace], "default-token")
	require.Contains(t, byName["team-adam"], "adam-token")
}

func TestGetPlugins_CrossOrgIsolation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	// Caller's org has its own published marketplace.
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	// A different org has a published marketplace + a wildcard-assigned plugin.
	seedSecondOrg(t, ctx, ti.conn)

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: mockidp.MockUserEmail})
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
	_, err := ti.conn.Exec(ctx,
		`INSERT INTO plugin_assignments (plugin_id, organization_id, principal_urn) VALUES ($1, $2, $3)`,
		stale, "other-org-id", "*")
	require.NoError(t, err)

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: mockidp.MockUserEmail})
	require.NoError(t, err)
	require.Equal(t, []string{wantObservability}, pluginSlugs(res),
		"an assignment row stamped with a different org must not deliver the plugin")
}

func TestGetPlugins_InvalidEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	_, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: ""})
	require.Error(t, err, "empty email must be rejected")
}
