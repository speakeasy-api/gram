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
	wantMarketplace   = naming.MarketplaceName(mockidp.MockOrgName)   // local-dev-org-speakeasy
	wantObservability = naming.ObservabilitySlug(mockidp.MockOrgName) // local-dev-org-observability
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

func TestGetPlugins_EmailAssignment(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	pid := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "engineering-tools")
	assignPlugin(t, ctx, ti.conn, pid, ti.orgID, "email:"+mockidp.MockUserEmail)

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: mockidp.MockUserEmail})
	require.NoError(t, err)

	require.Len(t, res.Marketplaces, 1)
	require.ElementsMatch(t, []string{wantObservability, "engineering-tools"}, pluginSlugs(res))
}

func TestGetPlugins_WildcardAssignment(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	pid := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "org-wide-tool")
	assignPlugin(t, ctx, ti.conn, pid, ti.orgID, "*")

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: mockidp.MockUserEmail})
	require.NoError(t, err)

	require.ElementsMatch(t, []string{wantObservability, "org-wide-tool"}, pluginSlugs(res),
		"a `*` assignment resolves for any email")
}

func TestGetPlugins_UnpublishedProjectExcluded(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	// Plugin assigned, but the project has no marketplace_token (never
	// published) — so nothing is installable and the endpoint returns empty.
	pid := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "unpublished-tool")
	assignPlugin(t, ctx, ti.conn, pid, ti.orgID, "*")

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: mockidp.MockUserEmail})
	require.NoError(t, err)

	require.Empty(t, res.Marketplaces)
	require.Empty(t, res.Plugins)
}

// TestGetPlugins_MultiProjectPrefersDefault pins DNO-228: an org with multiple
// published projects must collapse to the *default* project's marketplace (the
// one created at org setup, lowest id), not the alphabetically-first one.
//
// The default project (from InitAuthContext) has slug "test-<hex>", so a second
// project named "adam" sorts ahead of it alphabetically but is created later, so
// it has a higher id. With the old `ORDER BY pr.slug` "adam" won; with the
// id-ordered query the default project's token is the one returned.
func TestGetPlugins_MultiProjectPrefersDefault(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	// Default project: publish with a recognizable token.
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "default-token")

	// A second project in the same org that sorts first alphabetically.
	adam := seedProject(t, ctx, ti.conn, ti.orgID, "adam")
	publishMarketplace(t, ctx, ti.conn, adam, "adam-token")

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: mockidp.MockUserEmail})
	require.NoError(t, err)

	require.Len(t, res.Marketplaces, 1, "an org's projects collapse to one marketplace")
	require.Equal(t, wantMarketplace, res.Marketplaces[0].Name)
	require.Contains(t, res.Marketplaces[0].URL, "default-token",
		"the default project's marketplace must win, not the alphabetically-first one")
	require.NotContains(t, res.Marketplaces[0].URL, "adam-token")
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

func TestGetPlugins_InvalidEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	_, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: ""})
	require.Error(t, err, "empty email must be rejected")
}
