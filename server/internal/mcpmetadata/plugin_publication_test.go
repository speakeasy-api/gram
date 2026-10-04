package mcpmetadata_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_metadata"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// capturePublishSignaler records the post-commit marketplace republishes in
// place of the real Temporal signal.
type capturePublishSignaler struct {
	mu      sync.Mutex
	signals []uuid.UUID
}

func (c *capturePublishSignaler) SignalPluginPublish(_ context.Context, projectID uuid.UUID, _ string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.signals = append(c.signals, projectID)
	return nil
}

func (c *capturePublishSignaler) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.signals)
}

type publishingMetadataFixture struct {
	ti        *testInstance
	publisher *capturePublishSignaler
	orgID     string
	plugin    pluginsrepo.Plugin
}

// newPublishingMetadataService returns a metadata service that both records
// durable publication requests and signals the publisher, for a project with a
// marketplace connection and one plugin.
func newPublishingMetadataService(t *testing.T) (context.Context, publishingMetadataFixture) {
	t.Helper()

	ctx, ti := newTestMCPMetadataService(t)
	publisher := &capturePublishSignaler{mu: sync.Mutex{}, signals: nil}
	ti.service.WithPluginPublication(true, publisher)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := pluginsrepo.New(ti.conn).UpsertGitHubConnection(ctx, pluginsrepo.UpsertGitHubConnectionParams{
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
	plugin, err := pluginsrepo.New(ti.conn).CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           "Metadata plugin",
		Slug:           "metadata-plugin",
		Description:    pgtype.Text{},
	})
	require.NoError(t, err)

	return ctx, publishingMetadataFixture{ti: ti, publisher: publisher, orgID: authCtx.ActiveOrganizationID, plugin: plugin}
}

func (f publishingMetadataFixture) carry(t *testing.T, ctx context.Context, toolsetID, mcpServerID uuid.NullUUID) {
	t.Helper()

	_, err := pluginsrepo.New(f.ti.conn).AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{
		PluginID:    f.plugin.ID,
		ToolsetID:   toolsetID,
		McpServerID: mcpServerID,
		DisplayName: "Carried server",
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)
}

// requireRepublished asserts both the durable request and the post-commit
// signal fired the given number of times.
func (f publishingMetadataFixture) requireRepublished(t *testing.T, ctx context.Context, want int, msg string) {
	t.Helper()

	requests, err := testrepo.New(f.ti.conn).CountPublishOutboxRowsByTopic(ctx, testrepo.CountPublishOutboxRowsByTopicParams{
		OrganizationID: f.orgID,
		Topic:          "gram.plugins.v1.PublicationRequested",
	})
	require.NoError(t, err)
	require.Equal(t, int64(want), requests, msg)
	require.Equal(t, want, f.publisher.count(), msg)
}

// createMCPToolset creates an MCP-enabled toolset, the only kind a package
// lists directly.
func createMCPToolset(t *testing.T, ctx context.Context, ti *testInstance, slug string) toolsets_repo.Toolset {
	t.Helper()

	toolset := createTestToolset(t, ctx, ti, slug)
	require.NoError(t, toolsets_repo.New(ti.conn).SetToolsetMCPEnabledByID(ctx, toolsets_repo.SetToolsetMCPEnabledByIDParams{
		McpEnabled: true,
		ID:         toolset.ID,
		ProjectID:  toolset.ProjectID,
	}))
	toolset.McpEnabled = true
	return toolset
}

// createPublicToolset creates an MCP-enabled toolset whose package entry lists
// its user-supplied headers.
func createPublicToolset(t *testing.T, ctx context.Context, ti *testInstance, slug string) toolsets_repo.Toolset {
	t.Helper()

	toolset := createMCPToolset(t, ctx, ti, slug)
	require.NoError(t, toolsets_repo.New(ti.conn).SetToolsetMCPPublicByID(ctx, toolsets_repo.SetToolsetMCPPublicByIDParams{
		McpIsPublic: true,
		ID:          toolset.ID,
		ProjectID:   toolset.ProjectID,
	}))
	toolset.McpIsPublic = true
	return toolset
}

func userHeader(name, displayName string) *types.McpEnvironmentConfigInput {
	return &types.McpEnvironmentConfigInput{VariableName: name, HeaderDisplayName: &displayName, ProvidedBy: "user"}
}

func TestSetMcpMetadataRepublishesWhenUserHeadersChange(t *testing.T) {
	t.Parallel()

	ctx, f := newPublishingMetadataService(t)
	toolset := createPublicToolset(t, ctx, f.ti, "metadata-carried")
	f.carry(t, ctx, uuid.NullUUID{UUID: toolset.ID, Valid: true}, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	slug := types.Slug(toolset.Slug)

	_, err := f.ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:        &slug,
		EnvironmentConfigs: []*types.McpEnvironmentConfigInput{userHeader("API_TOKEN", "X-Api-Token")},
	})
	require.NoError(t, err)
	f.requireRepublished(t, ctx, 1, "a new user-supplied header changes the package")

	_, err = f.ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:        &slug,
		Instructions:       new("Install instructions only"),
		EnvironmentConfigs: []*types.McpEnvironmentConfigInput{userHeader("API_TOKEN", "X-Api-Token")},
	})
	require.NoError(t, err)
	f.requireRepublished(t, ctx, 1, "rewriting the same headers changes nothing a package renders")

	_, err = f.ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug: &slug,
		EnvironmentConfigs: []*types.McpEnvironmentConfigInput{
			userHeader("API_TOKEN", "X-Api-Token"),
			{VariableName: "SYSTEM_SECRET", HeaderDisplayName: nil, ProvidedBy: "system"},
		},
	})
	require.NoError(t, err)
	f.requireRepublished(t, ctx, 1, "a system-provided variable never reaches a package")

	_, err = f.ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:        &slug,
		EnvironmentConfigs: []*types.McpEnvironmentConfigInput{userHeader("API_TOKEN", "X-Renamed-Token")},
	})
	require.NoError(t, err)
	f.requireRepublished(t, ctx, 2, "a renamed header changes the package")

	_, err = f.ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:        &slug,
		EnvironmentConfigs: []*types.McpEnvironmentConfigInput{{VariableName: "API_TOKEN", HeaderDisplayName: nil, ProvidedBy: "user"}},
	})
	require.NoError(t, err)
	f.requireRepublished(t, ctx, 3, "an unset display name leaves the header unresolved")

	_, err = f.ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:        &slug,
		EnvironmentConfigs: []*types.McpEnvironmentConfigInput{userHeader("API_TOKEN", "")},
	})
	require.NoError(t, err)
	f.requireRepublished(t, ctx, 4, "an empty display name resolves the header")
}

func TestSetMcpMetadataPrivateToolsetRepublishesOnlyWhenHeadersAppearOrVanish(t *testing.T) {
	t.Parallel()

	ctx, f := newPublishingMetadataService(t)
	toolset := createMCPToolset(t, ctx, f.ti, "metadata-private")
	f.carry(t, ctx, uuid.NullUUID{UUID: toolset.ID, Valid: true}, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	slug := types.Slug(toolset.Slug)

	_, err := f.ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:        &slug,
		EnvironmentConfigs: []*types.McpEnvironmentConfigInput{userHeader("API_TOKEN", "X-Api-Token")},
	})
	require.NoError(t, err)
	f.requireRepublished(t, ctx, 1, "a required header removes the plugin from the shared Agent Plugins package")

	_, err = f.ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:        &slug,
		EnvironmentConfigs: []*types.McpEnvironmentConfigInput{userHeader("API_TOKEN", "X-Renamed-Token")},
	})
	require.NoError(t, err)
	f.requireRepublished(t, ctx, 1, "a private server's package never lists its headers")

	_, err = f.ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:        &slug,
		EnvironmentConfigs: []*types.McpEnvironmentConfigInput{},
	})
	require.NoError(t, err)
	f.requireRepublished(t, ctx, 2, "dropping the last header returns the plugin to the shared package")
}

func TestSetMcpMetadataSkipsServerOutsidePackages(t *testing.T) {
	t.Parallel()

	ctx, f := newPublishingMetadataService(t)
	toolset := createTestToolset(t, ctx, f.ti, "metadata-uncarried")
	slug := types.Slug(toolset.Slug)

	_, err := f.ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:        &slug,
		EnvironmentConfigs: []*types.McpEnvironmentConfigInput{userHeader("API_TOKEN", "X-Api-Token")},
	})
	require.NoError(t, err)
	f.requireRepublished(t, ctx, 0, "a server no plugin carries is in no package")
}

func TestSetMcpMetadataSkipsAttachedToolsetWithoutMCP(t *testing.T) {
	t.Parallel()

	ctx, f := newPublishingMetadataService(t)
	toolset := createTestToolset(t, ctx, f.ti, "metadata-not-mcp")
	f.carry(t, ctx, uuid.NullUUID{UUID: toolset.ID, Valid: true}, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	slug := types.Slug(toolset.Slug)

	_, err := f.ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		ToolsetSlug:        &slug,
		EnvironmentConfigs: []*types.McpEnvironmentConfigInput{userHeader("API_TOKEN", "X-Api-Token")},
	})
	require.NoError(t, err)
	f.requireRepublished(t, ctx, 0, "package generation leaves out an attached toolset that is not an MCP server")
}

func TestSetMcpMetadataRepublishesCarriedMCPServer(t *testing.T) {
	t.Parallel()

	ctx, f := newPublishingMetadataService(t)
	server, _ := createMcpServerWithEndpoint(t, ctx, f.ti, mcpServerFixtureOptions{})
	f.carry(t, ctx, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, uuid.NullUUID{UUID: server.ID, Valid: true})
	serverID := server.ID.String()

	_, err := f.ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{
		McpServerID:        &serverID,
		EnvironmentConfigs: []*types.McpEnvironmentConfigInput{userHeader("API_TOKEN", "X-Api-Token")},
	})
	require.NoError(t, err)
	f.requireRepublished(t, ctx, 1, "a header the server now requires changes its package entry")
}
