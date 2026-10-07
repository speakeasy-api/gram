package agent_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/plugins/installmode"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

func pluginsBySlug(res *gen.GetPluginsResult) map[string]*gen.AgentPlugin {
	out := make(map[string]*gen.AgentPlugin, len(res.Plugins))
	for _, p := range res.Plugins {
		out[p.Slug] = p
	}
	return out
}

func assignPluginWithMode(t *testing.T, ti *testInstance, pluginID uuid.UUID, principalURN string, mode installmode.Mode) {
	t.Helper()
	_, err := pluginsrepo.New(ti.conn).AddPluginAssignment(t.Context(), pluginsrepo.AddPluginAssignmentParams{
		PluginID:       pluginID,
		OrganizationID: ti.orgID,
		PrincipalUrn:   principalURN,
		InstallMode:    string(mode),
	})
	require.NoError(t, err)
}

func TestGetPlugins_ObservabilityIsRequired(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)

	observability := pluginsBySlug(res)[wantObservability]
	require.NotNil(t, observability)
	require.Equal(t, string(installmode.Required), observability.InstallMode)
	require.Nil(t, observability.Name)
}

func TestGetPlugins_AssignmentWithoutModeIsDefault(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	tool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "tool")
	assignPlugin(t, ctx, ti.conn, tool, ti.orgID, "*")

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)

	got := pluginsBySlug(res)["tool"]
	require.NotNil(t, got)
	require.Equal(t, string(installmode.Default), got.InstallMode)
	require.Equal(t, new("tool"), got.Name)
}

func TestGetPlugins_DeliversEachInstallMode(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	for _, mode := range installmode.Values() {
		plugin := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, string(mode)+"-tool")
		assignPluginWithMode(t, ti, plugin, "*", mode)
	}

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)

	plugins := pluginsBySlug(res)
	for _, mode := range installmode.Values() {
		got := plugins[string(mode)+"-tool"]
		require.NotNil(t, got, "an %s plugin is still listed", mode)
		require.Equal(t, string(mode), got.InstallMode)
	}
}

func TestGetPlugins_StrictestMatchingModeWins(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	email := "email:" + mockidp.MockUserEmail

	required := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "required-for-me")
	assignPluginWithMode(t, ti, required, "*", installmode.Available)
	assignPluginWithMode(t, ti, required, email, installmode.Required)

	defaulted := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "default-for-me")
	assignPluginWithMode(t, ti, defaulted, "*", installmode.Available)
	assignPluginWithMode(t, ti, defaulted, email, installmode.Default)

	// A stricter assignment to someone else must not leak into this caller.
	available := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "available-for-me")
	assignPluginWithMode(t, ti, available, "*", installmode.Available)
	assignPluginWithMode(t, ti, available, "email:someone-else@example.com", installmode.Required)

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)

	plugins := pluginsBySlug(res)
	require.Equal(t, string(installmode.Required), plugins["required-for-me"].InstallMode)
	require.Equal(t, string(installmode.Default), plugins["default-for-me"].InstallMode)
	require.Equal(t, string(installmode.Available), plugins["available-for-me"].InstallMode)
}

func TestGetPlugins_InstallModeChangeChangesETag(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	tool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "tool")
	assignPluginWithMode(t, ti, tool, "*", installmode.Default)

	before, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)

	assignPluginWithMode(t, ti, tool, "email:"+mockidp.MockUserEmail, installmode.Required)

	after, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)
	require.Equal(t, string(installmode.Required), pluginsBySlug(after)["tool"].InstallMode)
	require.NotEqual(t, before.Etag, after.Etag)
}
