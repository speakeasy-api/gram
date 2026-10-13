package mcpmetadata_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_metadata"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
)

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

	// The toolset_slug save re-keys the toolset-keyed row; the response keeps both ids.
	saved := setBySlug(t, ctx, ti, toolsetKeyed, &gen.SetMcpMetadataPayload{Instructions: new("saved")})
	requireHostedIDs(t, ctx, ti, saved, toolsetKeyed.ID, true)
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
	saved := setBySlug(t, ctx, ti, unwrapped, &gen.SetMcpMetadataPayload{Instructions: new("saved")})
	require.Equal(t, new(unwrapped.ID.String()), saved.ToolsetID)
	require.Nil(t, saved.McpServerID)

	deletedWrapper := createHostedToolset(t, ctx, ti, "ids-deleted-wrapper")
	createCanonicalWrapper(t, ctx, ti, deletedWrapper)
	_, err = mcpservers_repo.New(ti.conn).DeleteMCPServer(ctx, mcpservers_repo.DeleteMCPServerParams{ID: deletedWrapper.ID, ProjectID: deletedWrapper.ProjectID})
	require.NoError(t, err)
	seedToolsetKeyedRow(t, ctx, ti, deletedWrapper, "legacy", uuid.NullUUID{})
	got, err = ti.service.GetMcpMetadata(ctx, &gen.GetMcpMetadataPayload{ToolsetSlug: new(types.Slug(deletedWrapper.Slug))})
	require.NoError(t, err)
	require.Equal(t, new(deletedWrapper.ID.String()), got.Metadata.ToolsetID)
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
