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
	wantMarketplace   = naming.MarketplaceName(mockidp.MockOrgName)   // local-dev-org-gram
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
		require.NotEqual(t, "other-org-gram", m.Name)
	}
	require.NotContains(t, pluginSlugs(res), "other-plugin", "another org's plugin must not leak")
}

func TestGetPlugins_InvalidEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	_, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: ""})
	require.Error(t, err, "empty email must be rejected")
}
