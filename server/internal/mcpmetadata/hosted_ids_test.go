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
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// requireHostedIDs asserts a hosted backend's metadata reports both ids, equal, and comes from its server-keyed row.
func requireHostedIDs(t *testing.T, ctx context.Context, ti *testInstance, metadata *types.McpMetadata, id uuid.UUID) {
	t.Helper()

	require.Equal(t, new(id.String()), metadata.ToolsetID)
	require.Equal(t, new(id.String()), metadata.McpServerID)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	row, err := repo.New(ti.conn).GetMetadataByBackendID(ctx, repo.GetMetadataByBackendIDParams{ProjectID: *authCtx.ProjectID, BackendID: id, ToolsetBackend: true})
	require.NoError(t, err)
	require.Equal(t, row.ID.String(), metadata.ID)
	require.Equal(t, id, row.McpServerID)
}

// Callers reading toolset_id still get it from a hosted server-keyed row, under every addressing form.
func TestMcpMetadata_HostedRowsReportBothIDs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)

	hosted := createHostedToolset(t, ctx, ti, "ids-server-keyed")
	createCanonicalWrapper(t, ctx, ti, hosted, mcpservers.VisibilityPrivate)
	seedServerKeyedRow(t, ctx, ti, hosted, "seeded")

	for _, payload := range []*gen.GetMcpMetadataPayload{
		{ToolsetSlug: new(types.Slug(hosted.Slug))},
		{McpServerID: new(hosted.ID.String())},
	} {
		got, err := ti.service.GetMcpMetadata(ctx, payload)
		require.NoError(t, err)
		requireHostedIDs(t, ctx, ti, got.Metadata, hosted.ID)
	}

	saved := setBySlug(t, ctx, ti, hosted, &gen.SetMcpMetadataPayload{Instructions: new("saved")})
	requireHostedIDs(t, ctx, ti, saved, hosted.ID)
	saved, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{McpServerID: new(hosted.ID.String()), Instructions: new("saved")})
	require.NoError(t, err)
	requireHostedIDs(t, ctx, ti, saved, hosted.ID)
}

func TestMcpMetadata_NonHostedRowsKeepOneID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)

	// A toolset without a canonical wrapper has no metadata, and a save has no key.
	unwrapped := createHostedToolset(t, ctx, ti, "ids-unwrapped")
	_, err := ti.service.GetMcpMetadata(ctx, &gen.GetMcpMetadataPayload{ToolsetSlug: new(types.Slug(unwrapped.Slug))})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{ToolsetSlug: new(types.Slug(unwrapped.Slug)), Instructions: new("saved")})
	requireOopsCode(t, err, oops.CodeBadRequest)

	// A disabled canonical wrapper still shares its toolset's id.
	disabled := createHostedToolset(t, ctx, ti, "ids-disabled-wrapper")
	createCanonicalWrapper(t, ctx, ti, disabled, mcpservers.VisibilityPrivate)
	require.NoError(t, testrepo.New(ti.conn).SetMCPServerVisibilityFixture(ctx, testrepo.SetMCPServerVisibilityFixtureParams{Visibility: mcpservers.VisibilityDisabled, ID: disabled.ID, ProjectID: disabled.ProjectID}))
	seedServerKeyedRow(t, ctx, ti, disabled, "seeded")
	got, err := ti.service.GetMcpMetadata(ctx, &gen.GetMcpMetadataPayload{ToolsetSlug: new(types.Slug(disabled.Slug))})
	require.NoError(t, err)
	requireHostedIDs(t, ctx, ti, got.Metadata, disabled.ID)

	// A wrapper that outlived its toolset is not a hosted server; its row reports the server id only.
	orphaned := createHostedToolset(t, ctx, ti, "ids-orphaned")
	createCanonicalWrapper(t, ctx, ti, orphaned, mcpservers.VisibilityPrivate)
	seedServerKeyedRow(t, ctx, ti, orphaned, "seeded")
	_, err = toolsets_repo.New(ti.conn).DeleteToolset(ctx, toolsets_repo.DeleteToolsetParams{Slug: orphaned.Slug, ProjectID: orphaned.ProjectID})
	require.NoError(t, err)
	got, err = ti.service.GetMcpMetadata(ctx, &gen.GetMcpMetadataPayload{McpServerID: new(orphaned.ID.String())})
	require.NoError(t, err)
	require.Nil(t, got.Metadata.ToolsetID)
	require.Equal(t, new(orphaned.ID.String()), got.Metadata.McpServerID)

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
