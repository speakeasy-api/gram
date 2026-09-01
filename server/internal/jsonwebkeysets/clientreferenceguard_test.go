package jsonwebkeysets_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/json_web_key_sets"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// attachClientToSet plants a remote_session_issuer and a remote_session_client
// referencing the set through the shared testenv fixture. The remotesessions
// service that would normally build this lives in another package with its own
// harness, and all this guard reads is the reference itself. Returns the
// client's OAuth client_id, which is what the preflight lists back.
func attachClientToSet(t *testing.T, ctx context.Context, ti *testInstance, setID, clientID string) string {
	t.Helper()

	err := testrepo.New(ti.conn).SeedRemoteSessionClientForKeySetFixture(ctx, testrepo.SeedRemoteSessionClientForKeySetFixtureParams{
		OrganizationID:  conv.ToPGText(ti.orgID),
		IssuerSlug:      "guard-issuer-" + uuid.NewString(),
		ClientID:        clientID,
		JsonWebKeySetID: conv.StringToNullUUID(setID),
	})
	require.NoError(t, err)

	return clientID
}

// TestDeleteSet_RefusedWhileClientReferences is the guard this issue adds.
// Deleting a set a live client signs with would leave that client declaring an
// authentication method it can no longer perform, failing at the counterparty's
// token endpoint rather than here. The database cannot enforce it: the composite
// foreign key is NO ACTION and `deleted` is a generated column, so the soft
// delete never fires it.
func TestDeleteSet_RefusedWhileClientReferences(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "guard-signing-key")
	set := createSet(t, ctx, ti, "guard-primary", ek.ID)

	attachClientToSet(t, ctx, ti, set.ID, "guard-client-id")

	err := ti.service.DeleteSet(adminCtx(t, ctx), &gen.DeleteSetPayload{
		ID:           set.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)

	// The refusal left the set intact, keys and all: a blocked delete must not
	// have cascaded partway.
	fetched, err := ti.service.GetSet(readCtx(t, ctx), &gen.GetSetPayload{
		ID:           set.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, set.ID, fetched.ID)
	require.NotEmpty(t, listKeys(t, ctx, ti, set.ID, false))
}

// TestDeleteSet_AllowedAfterClientDetaches proves the guard reads the live
// reference rather than any reference: clearing json_web_key_set_id releases the
// set, which is the path the dashboard's detach control drives.
func TestDeleteSet_AllowedAfterClientDetaches(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "guard-detach-key")
	set := createSet(t, ctx, ti, "guard-detach-set", ek.ID)

	attachClientToSet(t, ctx, ti, set.ID, "guard-detach-client-id")

	require.NoError(t, testrepo.New(ti.conn).ClearRemoteSessionClientKeySetFixture(ctx, conv.StringToNullUUID(set.ID)))

	require.NoError(t, ti.service.DeleteSet(adminCtx(t, ctx), &gen.DeleteSetPayload{
		ID:           set.ID,
		SessionToken: nil,
	}))
}

// TestDeleteSet_IgnoresSoftDeletedClients keeps a tombstoned client from pinning
// a set forever. It will never authenticate again, so it is not a live
// reference.
func TestDeleteSet_IgnoresSoftDeletedClients(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "guard-tombstone-key")
	set := createSet(t, ctx, ti, "guard-tombstone-set", ek.ID)

	attachClientToSet(t, ctx, ti, set.ID, "guard-tombstone-client-id")

	require.NoError(t, testrepo.New(ti.conn).SoftDeleteRemoteSessionClientsForKeySetFixture(ctx, conv.StringToNullUUID(set.ID)))

	require.NoError(t, ti.service.DeleteSet(adminCtx(t, ctx), &gen.DeleteSetPayload{
		ID:           set.ID,
		SessionToken: nil,
	}))
}

// TestGetSetDeletePreflight reports exactly what DeleteSet refuses on, so the
// dashboard can warn before the administrator confirms rather than after.
func TestGetSetDeletePreflight(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "preflight-key")
	set := createSet(t, ctx, ti, "preflight-set", ek.ID)

	empty, err := ti.service.GetSetDeletePreflight(readCtx(t, ctx), &gen.GetSetDeletePreflightPayload{
		ID:           set.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, 0, empty.ClientCount)
	require.Empty(t, empty.ClientIds)

	attachClientToSet(t, ctx, ti, set.ID, "preflight-client-a")
	attachClientToSet(t, ctx, ti, set.ID, "preflight-client-b")

	loaded, err := ti.service.GetSetDeletePreflight(readCtx(t, ctx), &gen.GetSetDeletePreflightPayload{
		ID:           set.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, 2, loaded.ClientCount)
	// Equal rather than ElementsMatch: the SQL and the Goa attribute both promise
	// oldest first, and a membership assertion cannot fail if the ORDER BY is
	// dropped.
	require.Equal(t, []string{"preflight-client-a", "preflight-client-b"}, loaded.ClientIds)

	// The preflight predicts a real refusal, not an advisory one.
	requireOopsCode(t, ti.service.DeleteSet(adminCtx(t, ctx), &gen.DeleteSetPayload{
		ID:           set.ID,
		SessionToken: nil,
	}), oops.CodeConflict)
}

// TestGetSetDeletePreflight_UnknownSetIsEmpty matches DeleteSet's own idempotent
// treatment of an absent id: neither reports a 404.
func TestGetSetDeletePreflight_UnknownSetIsEmpty(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	result, err := ti.service.GetSetDeletePreflight(readCtx(t, ctx), &gen.GetSetDeletePreflightPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, 0, result.ClientCount)
	require.Empty(t, result.ClientIds)
}
