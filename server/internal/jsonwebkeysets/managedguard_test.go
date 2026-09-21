package jsonwebkeysets_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/json_web_key_sets"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/provisiontest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// provisionManagedSet runs the real identity provider connection provisioner
// against this package's test organization, yielding a set, a key, and a
// backing external key that all carry the managed-by marker.
func provisionManagedSet(t *testing.T, ctx context.Context, ti *testInstance) *provisiontest.Fixture {
	t.Helper()

	issuerID := provisiontest.CreateIssuer(t, ctx, ti.conn, ti.orgID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, provisiontest.TokenEndpoint)

	return provisiontest.Provision(t, ctx, ti.conn, ti.orgID, issuerID, "https://app.getgram.ai")
}

// A managed set is readable through the organization surface but every
// mutation on it, and on its keys, is refused: the backing key is Speakeasy's,
// and the connection owns the lifecycle.
func TestManagedSet_RefusesOrganizationTierMutations(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	fx := provisionManagedSet(t, ctx, ti)
	setID := fx.Client.JSONWebKeySetID.String()
	keyID := fx.Client.ActiveKeyID.UUID.String()

	fetched, err := ti.service.GetSet(readCtx(t, ctx), &gen.GetSetPayload{ID: setID, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, setID, fetched.ID)

	keys := listKeys(t, ctx, ti, setID, false)
	require.Len(t, keys, 1)
	require.Equal(t, "active", keys[0].KeyState)

	_, err = ti.service.PublishKey(adminCtx(t, ctx), &gen.PublishKeyPayload{SetID: setID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)

	_, err = ti.service.ActivateKey(adminCtx(t, ctx), &gen.ActivateKeyPayload{ID: keyID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)

	_, err = ti.service.RetireKey(adminCtx(t, ctx), &gen.RetireKeyPayload{ID: keyID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)

	_, err = ti.service.RevokeKey(adminCtx(t, ctx), &gen.RevokeKeyPayload{ID: keyID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)

	other := createBackedGcpKmsKey(t, ctx, ti, "managed-guard-other")
	_, err = ti.service.UpdateSet(adminCtx(t, ctx), &gen.UpdateSetPayload{ID: setID, Name: "renamed", ExternalKeyID: other.ID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)

	err = ti.service.DeleteSet(adminCtx(t, ctx), &gen.DeleteSetPayload{ID: setID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)

	// Nothing moved: the key is still the active one and the set still lists.
	after := listKeys(t, ctx, ti, setID, true)
	require.Len(t, after, 1)
	require.Equal(t, "active", after[0].KeyState)
	require.Equal(t, keyID, after[0].ID)
}

// An organization must not be able to build its own set on Speakeasy's key:
// the mint path admits the platform credential for that key, so a set backed
// by it would publish Speakeasy-signed keys wherever the organization attached
// it.
func TestCreateSet_RefusesManagedBackingKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	fx := provisionManagedSet(t, ctx, ti)

	_, err := ti.service.CreateSet(adminCtx(t, ctx), &gen.CreateSetPayload{
		SessionToken:  nil,
		Name:          "piggyback",
		ExternalKeyID: fx.Client.ExternalKeyID.String(),
	})
	requireOopsCode(t, err, oops.CodeConflict)

	// Re-pointing an ordinary set at the managed key is the same escape.
	own := createBackedGcpKmsKey(t, ctx, ti, "managed-guard-own")
	set := createSet(t, ctx, ti, "own-set", own.ID)
	_, err = ti.service.UpdateSet(adminCtx(t, ctx), &gen.UpdateSetPayload{ID: set.ID, Name: set.Name, ExternalKeyID: fx.Client.ExternalKeyID.String(), SessionToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)
}

// Once the connection is tombstoned its set drops out of the list and becomes
// deletable after its client is gone, while key publication stays refused.
func TestManagedSet_DeletableOnceConnectionTombstoned(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	fx := provisionManagedSet(t, ctx, ti)
	setID := fx.Client.JSONWebKeySetID.String()

	err := ti.service.DeleteSet(adminCtx(t, ctx), &gen.DeleteSetPayload{ID: setID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)

	provisiontest.SoftDeleteConnection(t, ctx, ti.conn, ti.orgID, fx.ConnectionID)

	require.NotContains(t, listSetIDs(t, ctx, ti), setID, "a leftover set is hidden from the list")
	fetched, err := ti.service.GetSet(readCtx(t, ctx), &gen.GetSetPayload{ID: setID, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, setID, fetched.ID, "the leftover stays reachable by id")

	_, err = ti.service.PublishKey(adminCtx(t, ctx), &gen.PublishKeyPayload{SetID: setID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)
	other := createBackedGcpKmsKey(t, ctx, ti, "managed-tombstone-other")
	_, err = ti.service.UpdateSet(adminCtx(t, ctx), &gen.UpdateSetPayload{ID: setID, Name: "renamed", ExternalKeyID: other.ID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)

	// The leftover client still signs with the set; it goes first.
	err = ti.service.DeleteSet(adminCtx(t, ctx), &gen.DeleteSetPayload{ID: setID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)
	require.ErrorContains(t, err, "still in use by a remote session client")
	affected, err := remotesessionsrepo.New(ti.conn).SoftDeleteRemoteSessionClientFixture(ctx, fx.Client.ClientRowID)
	require.NoError(t, err)
	require.EqualValues(t, 1, affected)

	require.NoError(t, ti.service.DeleteSet(adminCtx(t, ctx), &gen.DeleteSetPayload{ID: setID, SessionToken: nil}))
	_, err = ti.service.GetSet(readCtx(t, ctx), &gen.GetSetPayload{ID: setID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func listSetIDs(t *testing.T, ctx context.Context, ti *testInstance) []string {
	t.Helper()

	result, err := ti.service.ListSets(readCtx(t, ctx), &gen.ListSetsPayload{SessionToken: nil})
	require.NoError(t, err)
	ids := make([]string, 0, len(result.Sets))
	for _, set := range result.Sets {
		ids = append(ids, set.ID)
	}
	return ids
}
