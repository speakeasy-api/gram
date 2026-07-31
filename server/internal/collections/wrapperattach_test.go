package collections_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/collections"
	"github.com/speakeasy-api/gram/server/internal/collections/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversRepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
)

// wrapperIDForToolset resolves the wrapper mcp_server provisioned for a
// toolset created through the toolsets service.
func wrapperIDForToolset(t *testing.T, ti *testInstance, toolsetID, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	wrappers, err := mcpserversRepo.New(ti.conn).GetMCPServersByToolsetID(t.Context(), mcpserversRepo.GetMCPServersByToolsetIDParams{
		ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true},
		ProjectID: projectID,
	})
	require.NoError(t, err)
	require.Len(t, wrappers, 1, "expected the toolsets service to provision exactly one wrapper")
	return wrappers[0].ID
}

// Attaching a wrapped toolset by toolset id must write the attachment
// server-keyed on its canonical wrapper, and detaching by toolset id must
// resolve and remove that server-keyed attachment.
func TestCollectionsService_AttachServer_ToolsetResolvesToWrapper(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createMCPEnabledToolset(t, ctx, ti, "Wrapped Toolset", "")
	toolsetID, err := uuid.Parse(toolset.ID)
	require.NoError(t, err)
	wrapperID := wrapperIDForToolset(t, ti, toolsetID, *authCtx.ProjectID)

	collection := createCollection(t, ctx, ti, "Registry", "registry", "com.speakeasy.registry")
	collectionID, err := uuid.Parse(collection.ID)
	require.NoError(t, err)

	_, err = ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		ToolsetID:    &toolset.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	rows, err := repo.New(ti.conn).ListOrganizationMcpCollectionAttachmentRows(ctx, repo.ListOrganizationMcpCollectionAttachmentRowsParams{
		CollectionID:   collectionID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.False(t, rows[0].ToolsetID.Valid, "the attachment must be server-keyed, not toolset-keyed")
	require.Equal(t, uuid.NullUUID{UUID: wrapperID, Valid: true}, rows[0].McpServerID)
	require.False(t, rows[0].DeletedAt.Valid)

	// Detach by toolset id resolves the wrapper and removes the server-keyed
	// attachment.
	require.NoError(t, ti.service.DetachServer(ctx, &gen.DetachServerPayload{
		CollectionID: collection.ID,
		ToolsetID:    &toolset.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	}))

	rows, err = repo.New(ti.conn).ListOrganizationMcpCollectionAttachmentRows(ctx, repo.ListOrganizationMcpCollectionAttachmentRowsParams{
		CollectionID:   collectionID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.True(t, rows[0].DeletedAt.Valid)

	listed, err := ti.service.ListServers(ctx, &gen.ListServersPayload{
		CollectionSlug: collection.Slug,
		SessionToken:   nil,
		ApikeyToken:    nil,
	})
	require.NoError(t, err)
	require.Empty(t, listed.Servers)
}

// A live toolset-keyed attachment left from before the wrapper existed is
// rekeyed in place on the next publish — same row id — instead of a second
// server-keyed row being created.
func TestCollectionsService_AttachServer_MovesToolsetKeyedAttachmentInPlace(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createMCPEnabledToolset(t, ctx, ti, "Rekeyed Toolset", "")
	toolsetID, err := uuid.Parse(toolset.ID)
	require.NoError(t, err)
	wrapperID := wrapperIDForToolset(t, ti, toolsetID, *authCtx.ProjectID)

	collection := createCollection(t, ctx, ti, "Registry", "registry", "com.speakeasy.registry")
	collectionID, err := uuid.Parse(collection.ID)
	require.NoError(t, err)

	// Seed a toolset-keyed attachment directly, as writes before this release
	// created them.
	seeded, err := repo.New(ti.conn).AttachServerToOrganizationMcpCollection(ctx, repo.AttachServerToOrganizationMcpCollectionParams{
		CollectionID:   collectionID,
		OrganizationID: authCtx.ActiveOrganizationID,
		ToolsetID:      uuid.NullUUID{UUID: toolsetID, Valid: true},
		PublishedBy:    conv.ToPGText("user_test_fixture"),
	})
	require.NoError(t, err)

	_, err = ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		ToolsetID:    &toolset.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	rows, err := repo.New(ti.conn).ListOrganizationMcpCollectionAttachmentRows(ctx, repo.ListOrganizationMcpCollectionAttachmentRowsParams{
		CollectionID:   collectionID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1, "the toolset-keyed row must be rekeyed, not duplicated")
	require.Equal(t, seeded.ID, rows[0].ID)
	require.False(t, rows[0].ToolsetID.Valid)
	require.Equal(t, uuid.NullUUID{UUID: wrapperID, Valid: true}, rows[0].McpServerID)
	require.False(t, rows[0].DeletedAt.Valid)
}
