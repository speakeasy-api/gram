package toolsets_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const publicationRequestedTopic = "gram.plugins.v1.PublicationRequested"

// newPublishingToolsetsService returns a toolsets service that records durable
// plugin publication requests, for a project whose first toolset is MCP-enabled
// and carried by the Default plugin.
func newPublishingToolsetsService(t *testing.T, connected bool) (context.Context, *testInstance, *types.Toolset) {
	t.Helper()

	ctx, ti := newTestToolsetsService(t)
	ti.service.WithPublicationRequests(true)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	carried, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		Name:         "Carried toolset",
		ToolUrns:     []string{},
		ResourceUrns: []string{},
	})
	require.NoError(t, err)
	require.NotNil(t, carried.McpEnabled)
	require.True(t, *carried.McpEnabled, "the first toolset in an organization is enabled and joins the Default plugin")

	if connected {
		_, err = pluginsrepo.New(ti.conn).UpsertGitHubConnection(ctx, pluginsrepo.UpsertGitHubConnectionParams{
			ProjectID:                *authCtx.ProjectID,
			InstallationID:           1,
			RepoOwner:                "marketplace-owner",
			RepoName:                 "marketplace-repo",
			MarketplaceToken:         pgtype.Text{},
			PublishedMcpFingerprints: nil,
			PublishedHooksVersion:    pgtype.Text{},
			PublishedHooksConfig:     nil,
		})
		require.NoError(t, err)
	}
	return ctx, ti, carried
}

func publicationRequests(t *testing.T, ctx context.Context, ti *testInstance) int64 {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	count, err := testrepo.New(ti.conn).CountPublishOutboxRowsByTopic(ctx, testrepo.CountPublishOutboxRowsByTopicParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Topic:          publicationRequestedTopic,
	})
	require.NoError(t, err)
	return count
}

func TestDeleteToolsetRequestsPublicationForCarriedToolset(t *testing.T) {
	t.Parallel()

	ctx, ti, carried := newPublishingToolsetsService(t, true)

	require.NoError(t, ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{Slug: carried.Slug}))

	require.Equal(t, int64(1), publicationRequests(t, ctx, ti))
}

func TestDeleteToolsetRequestsPublicationForWrapperBackedToolset(t *testing.T) {
	t.Parallel()

	ctx, ti, carried := newPublishingToolsetsService(t, true)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolsetID := uuid.MustParse(carried.ID)
	plugins := pluginsrepo.New(ti.conn)
	defaultPlugin, err := plugins.GetDefaultPlugin(ctx, pluginsrepo.GetDefaultPluginParams{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	// Replace the direct attachment with one through the toolset's hosted
	// wrapper, which deleting the toolset removes in the same transaction.
	require.NoError(t, plugins.SoftDeletePluginServers(ctx, defaultPlugin.ID))
	servers := mcpserversrepo.New(ti.conn)
	if _, err := servers.GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: toolsetID, ProjectID: *authCtx.ProjectID}); err != nil {
		_, err = servers.CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
			ID: toolsetID, ProjectID: *authCtx.ProjectID, Name: pgtype.Text{String: carried.Name, Valid: true}, Slug: pgtype.Text{String: string(*carried.McpSlug), Valid: true},
			EnvironmentID: uuid.NullUUID{}, UserSessionIssuerID: uuid.NullUUID{}, RemoteMcpServerID: uuid.NullUUID{}, TunneledMcpServerID: uuid.NullUUID{},
			ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true}, UnproxiedMcpServerID: uuid.NullUUID{}, ToolVariationsGroupID: uuid.NullUUID{},
			Visibility: "private", NetworkAccessMode: pgtype.Text{},
		})
		require.NoError(t, err)
	}
	_, err = plugins.AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{
		PluginID: defaultPlugin.ID, ToolsetID: uuid.NullUUID{}, McpServerID: uuid.NullUUID{UUID: toolsetID, Valid: true},
		DisplayName: "Wrapped toolset", Policy: "required", SortOrder: 0,
	})
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{Slug: carried.Slug}))

	require.Equal(t, int64(1), publicationRequests(t, ctx, ti), "deleting the wrapper must not hide the plugin it reached")
}

func TestDeleteToolsetSkipsToolsetOutsidePackages(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newPublishingToolsetsService(t, true)
	disabled, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		Name:         "Disabled toolset",
		ToolUrns:     []string{},
		ResourceUrns: []string{},
	})
	require.NoError(t, err)
	require.NotNil(t, disabled.McpEnabled)
	require.False(t, *disabled.McpEnabled)

	require.NoError(t, ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{Slug: disabled.Slug}))

	require.Zero(t, publicationRequests(t, ctx, ti), "a toolset that is not an MCP server is in no package")
}

func TestDeleteToolsetSkipsProjectWithoutMarketplace(t *testing.T) {
	t.Parallel()

	ctx, ti, carried := newPublishingToolsetsService(t, false)

	require.NoError(t, ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{Slug: carried.Slug}))

	require.Zero(t, publicationRequests(t, ctx, ti))
}

func TestSetUserSessionIssuerRequestsPublicationWhenOAuthChanges(t *testing.T) {
	t.Parallel()

	ctx, ti, carried := newPublishingToolsetsService(t, true)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	newIssuer := func(slug string) string {
		issuer, err := usersessionsrepo.New(ti.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
			OrganizationID:               pgtype.Text{String: authCtx.ActiveOrganizationID, Valid: true},
			Slug:                         slug,
			AuthnChallengeMode:           "interactive",
			SessionDuration:              pgtype.Interval{Microseconds: 14 * 24 * 60 * 60 * 1_000_000, Valid: true},
			TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		})
		require.NoError(t, err)
		return issuer.ID.String()
	}
	first, second := newIssuer("publication-first"), newIssuer("publication-second")

	_, err := ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{Slug: carried.Slug, UserSessionIssuerID: &first})
	require.NoError(t, err)
	require.Equal(t, int64(1), publicationRequests(t, ctx, ti), "linking an issuer turns the package entry into an OAuth server")

	_, err = ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{Slug: carried.Slug, UserSessionIssuerID: &second})
	require.NoError(t, err)
	require.Equal(t, int64(1), publicationRequests(t, ctx, ti), "swapping issuers keeps the entry an OAuth server")

	_, err = ti.service.SetUserSessionIssuer(ctx, &gen.SetUserSessionIssuerPayload{Slug: carried.Slug, UserSessionIssuerID: nil})
	require.NoError(t, err)
	require.Equal(t, int64(2), publicationRequests(t, ctx, ti))
}
