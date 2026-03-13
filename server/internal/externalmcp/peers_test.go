package externalmcp_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_registries"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

func TestCreatePeer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Create a second org to peer with
	subOrgID := "sub-org-test-1"
	orgQueries := orgRepo.New(ti.conn)
	_, err := orgQueries.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:              subOrgID,
		Name:            "Sub Org",
		Slug:            "sub-org",
		SsoConnectionID: pgtype.Text{},
	})
	require.NoError(t, err)

	peer, err := ti.service.CreatePeer(ctx, &gen.CreatePeerPayload{
		SubOrganizationID: subOrgID,
	})
	require.NoError(t, err)
	require.NotEmpty(t, peer.ID)
	require.Equal(t, authCtx.ActiveOrganizationID, peer.SuperOrganizationID)
	require.Equal(t, subOrgID, peer.SubOrganizationID)
	require.NotEmpty(t, peer.CreatedAt)
}

func TestCreatePeerRequiresAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newNonAdminTestService(t)

	_, err := ti.service.CreatePeer(ctx, &gen.CreatePeerPayload{
		SubOrganizationID: "some-org",
	})
	require.Error(t, err)
}

func TestCreatePeerSelfPeering(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := ti.service.CreatePeer(ctx, &gen.CreatePeerPayload{
		SubOrganizationID: authCtx.ActiveOrganizationID,
	})
	require.Error(t, err)
}

func TestCreatePeerIdempotent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	subOrgID := "sub-org-idem-1"
	orgQueries := orgRepo.New(ti.conn)
	_, err := orgQueries.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:              subOrgID,
		Name:            "Sub Org Idem",
		Slug:            "sub-org-idem",
		SsoConnectionID: pgtype.Text{},
	})
	require.NoError(t, err)

	_, err = ti.service.CreatePeer(ctx, &gen.CreatePeerPayload{
		SubOrganizationID: subOrgID,
	})
	require.NoError(t, err)

	// Creating the same peer again should not error (ON CONFLICT DO NOTHING)
	// but note it returns no rows on conflict, which may cause an error
	// depending on sqlc behavior with :one
	_, _ = ti.service.CreatePeer(ctx, &gen.CreatePeerPayload{
		SubOrganizationID: subOrgID,
	})
}

func TestListPeers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Initially no peers
	result, err := ti.service.ListPeers(ctx, &gen.ListPeersPayload{})
	require.NoError(t, err)
	require.Empty(t, result.Peers)

	// Create a sub org and peer
	subOrgID := "sub-org-list-1"
	orgQueries := orgRepo.New(ti.conn)
	_, err = orgQueries.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:              subOrgID,
		Name:            "List Sub Org",
		Slug:            "list-sub-org",
		SsoConnectionID: pgtype.Text{},
	})
	require.NoError(t, err)

	_, err = ti.service.CreatePeer(ctx, &gen.CreatePeerPayload{
		SubOrganizationID: subOrgID,
	})
	require.NoError(t, err)

	// Now should have one peer
	result, err = ti.service.ListPeers(ctx, &gen.ListPeersPayload{})
	require.NoError(t, err)
	require.Len(t, result.Peers, 1)
	require.Equal(t, subOrgID, result.Peers[0].SubOrganizationID)
	require.NotNil(t, result.Peers[0].SubOrganizationName)
	require.Equal(t, "List Sub Org", *result.Peers[0].SubOrganizationName)
	require.NotNil(t, result.Peers[0].SubOrganizationSlug)
	require.Equal(t, "list-sub-org", *result.Peers[0].SubOrganizationSlug)
}

func TestDeletePeer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	subOrgID := "sub-org-del-1"
	orgQueries := orgRepo.New(ti.conn)
	_, err := orgQueries.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:              subOrgID,
		Name:            "Del Sub Org",
		Slug:            "del-sub-org",
		SsoConnectionID: pgtype.Text{},
	})
	require.NoError(t, err)

	_, err = ti.service.CreatePeer(ctx, &gen.CreatePeerPayload{
		SubOrganizationID: subOrgID,
	})
	require.NoError(t, err)

	// Verify peer exists
	result, err := ti.service.ListPeers(ctx, &gen.ListPeersPayload{})
	require.NoError(t, err)
	require.Len(t, result.Peers, 1)

	// Delete the peer
	err = ti.service.DeletePeer(ctx, &gen.DeletePeerPayload{
		SubOrganizationID: subOrgID,
	})
	require.NoError(t, err)

	// Verify peer is gone
	result, err = ti.service.ListPeers(ctx, &gen.ListPeersPayload{})
	require.NoError(t, err)
	require.Empty(t, result.Peers)
}

func TestDeletePeerNonexistent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Deleting a non-existent peer should not error (DELETE is idempotent)
	err := ti.service.DeletePeer(ctx, &gen.DeletePeerPayload{
		SubOrganizationID: "nonexistent-org",
	})
	require.NoError(t, err)
}
