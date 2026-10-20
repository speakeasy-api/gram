package mcpmetadata_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_metadata"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// requireHostedIDs asserts a hosted backend's metadata reports both ids, equal, and which key the row it came from is stored under.
func requireHostedIDs(t *testing.T, ctx context.Context, ti *testInstance, metadata *types.McpMetadata, id uuid.UUID, serverKeyed bool) {
	t.Helper()

	require.Equal(t, new(id.String()), metadata.ToolsetID)
	require.Equal(t, new(id.String()), metadata.McpServerID)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	row, err := repo.New(ti.conn).GetMetadataByBackendID(ctx, repo.GetMetadataByBackendIDParams{ProjectID: *authCtx.ProjectID, BackendID: id, ToolsetBackend: true})
	require.NoError(t, err)
	require.Equal(t, row.ID.String(), metadata.ID)
	require.Equal(t, serverKeyed, row.McpServerID.Valid)
	require.Equal(t, !serverKeyed, row.ToolsetID.Valid)
}

// Callers reading toolset_id keep it once a hosted row moves to its canonical server, under every addressing form.
func TestMcpMetadata_HostedRowsReportBothIDs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)

	serverKeyed := createHostedToolset(t, ctx, ti, "ids-server-keyed")
	createCanonicalWrapper(t, ctx, ti, serverKeyed)
	seedToolsetKeyedRow(t, ctx, ti, serverKeyed, "moved", uuid.NullUUID{})
	rekeyToServer(t, ctx, ti, serverKeyed)

	toolsetKeyed := createHostedToolset(t, ctx, ti, "ids-toolset-keyed")
	createCanonicalWrapper(t, ctx, ti, toolsetKeyed)
	seedToolsetKeyedRow(t, ctx, ti, toolsetKeyed, "legacy", uuid.NullUUID{})

	for name, tc := range map[string]struct {
		id          uuid.UUID
		slug        string
		serverKeyed bool
	}{
		"server-keyed":  {id: serverKeyed.ID, slug: serverKeyed.Slug, serverKeyed: true},
		"toolset-keyed": {id: toolsetKeyed.ID, slug: toolsetKeyed.Slug, serverKeyed: false},
	} {
		for _, payload := range []*gen.GetMcpMetadataPayload{
			{ToolsetSlug: new(types.Slug(tc.slug))},
			{McpServerID: new(tc.id.String())},
		} {
			got, err := ti.service.GetMcpMetadata(ctx, payload)
			require.NoError(t, err, name)
			requireHostedIDs(t, ctx, ti, got.Metadata, tc.id, tc.serverKeyed)
		}
	}

	// A save moves the toolset-keyed row onto the server key.
	saved := setBySlug(t, ctx, ti, toolsetKeyed, &gen.SetMcpMetadataPayload{Instructions: new("saved")})
	requireHostedIDs(t, ctx, ti, saved, toolsetKeyed.ID, true)
	saved, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{McpServerID: new(serverKeyed.ID.String()), Instructions: new("saved")})
	require.NoError(t, err)
	requireHostedIDs(t, ctx, ti, saved, serverKeyed.ID, true)
}

func TestMcpMetadata_NonHostedRowsKeepOneID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)

	unwrapped := createHostedToolset(t, ctx, ti, "ids-unwrapped")
	seedToolsetKeyedRow(t, ctx, ti, unwrapped, "legacy", uuid.NullUUID{})
	got, err := ti.service.GetMcpMetadata(ctx, &gen.GetMcpMetadataPayload{ToolsetSlug: new(types.Slug(unwrapped.Slug))})
	require.NoError(t, err)
	require.Equal(t, new(unwrapped.ID.String()), got.Metadata.ToolsetID)
	require.Nil(t, got.Metadata.McpServerID)
	_, err = ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{ToolsetSlug: new(types.Slug(unwrapped.Slug)), Instructions: new("saved")})
	requireOopsCode(t, err, oops.CodeBadRequest)

	deletedWrapper := createHostedToolset(t, ctx, ti, "ids-deleted-wrapper")
	createCanonicalWrapper(t, ctx, ti, deletedWrapper)
	_, err = mcpservers_repo.New(ti.conn).DeleteMCPServer(ctx, mcpservers_repo.DeleteMCPServerParams{ID: deletedWrapper.ID, ProjectID: deletedWrapper.ProjectID})
	require.NoError(t, err)
	seedToolsetKeyedRow(t, ctx, ti, deletedWrapper, "legacy", uuid.NullUUID{})
	got, err = ti.service.GetMcpMetadata(ctx, &gen.GetMcpMetadataPayload{ToolsetSlug: new(types.Slug(deletedWrapper.Slug))})
	require.NoError(t, err)
	require.Equal(t, new(deletedWrapper.ID.String()), got.Metadata.ToolsetID)
	require.Nil(t, got.Metadata.McpServerID)

	// A disabled canonical wrapper still shares its toolset's id.
	disabled := createHostedToolset(t, ctx, ti, "ids-disabled-wrapper")
	createCanonicalWrapper(t, ctx, ti, disabled)
	require.NoError(t, testrepo.New(ti.conn).SetMCPServerVisibilityFixture(ctx, testrepo.SetMCPServerVisibilityFixtureParams{Visibility: mcpservers.VisibilityDisabled, ID: disabled.ID, ProjectID: disabled.ProjectID}))
	seedToolsetKeyedRow(t, ctx, ti, disabled, "legacy", uuid.NullUUID{})
	got, err = ti.service.GetMcpMetadata(ctx, &gen.GetMcpMetadataPayload{ToolsetSlug: new(types.Slug(disabled.Slug))})
	require.NoError(t, err)
	requireHostedIDs(t, ctx, ti, got.Metadata, disabled.ID, false)

	// A wrapper that outlived its toolset is not a hosted server.
	orphaned := createHostedToolset(t, ctx, ti, "ids-orphaned")
	createCanonicalWrapper(t, ctx, ti, orphaned)
	seedToolsetKeyedRow(t, ctx, ti, orphaned, "legacy", uuid.NullUUID{})
	_, err = toolsets_repo.New(ti.conn).DeleteToolset(ctx, toolsets_repo.DeleteToolsetParams{Slug: orphaned.Slug, ProjectID: orphaned.ProjectID})
	require.NoError(t, err)
	got, err = ti.service.GetMcpMetadata(ctx, &gen.GetMcpMetadataPayload{McpServerID: new(orphaned.ID.String())})
	require.NoError(t, err)
	require.Equal(t, new(orphaned.ID.String()), got.Metadata.ToolsetID)
	require.Nil(t, got.Metadata.McpServerID)

	// A fresh-id server backed by a toolset is not that toolset's canonical wrapper.
	server, _ := createMcpServerWithEndpoint(t, ctx, ti, mcpServerFixtureOptions{visibility: mcpservers.VisibilityPrivate})
	set, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{McpServerID: new(server.ID.String()), Instructions: new("fresh")})
	require.NoError(t, err)
	require.Nil(t, set.ToolsetID)
	require.Equal(t, new(server.ID.String()), set.McpServerID)
	fetched, err := ti.service.GetMcpMetadata(ctx, &gen.GetMcpMetadataPayload{McpServerID: new(server.ID.String())})
	require.NoError(t, err)
	require.Nil(t, fetched.Metadata.ToolsetID)
	require.Equal(t, new(server.ID.String()), fetched.Metadata.McpServerID)
}
