package tailnetgateway_test

import (
	"encoding/base64"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"tailscale.com/ipn"

	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/tailnetgateway"
	"github.com/speakeasy-api/gram/server/internal/tailnetgateway/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// seedIngress creates an organization and a gateway-runtime ingress row and
// returns the ingress id and organization id.
func seedIngress(t *testing.T, db *pgxpool.Pool, slug string) (uuid.UUID, string) {
	t.Helper()
	ctx := t.Context()
	orgID := "org_" + uuid.NewString()
	_, err := orgrepo.New(db).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: orgID, Name: "Tailnet Gateway Test", Slug: slug + "-" + uuid.NewString()[:8], WorkosID: conv.ToPGText(orgID), Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	id := uuid.New()
	require.NoError(t, testrepo.New(db).InsertNetworkIngressFixture(ctx, testrepo.InsertNetworkIngressFixtureParams{
		ID: id, OrganizationID: orgID, DnsName: pgtype.Text{String: "speakeasy.example.ts.net", Valid: true},
	}))
	_, err = repo.New(db).MarkNetworkIngressRuntime(ctx, repo.MarkNetworkIngressRuntimeParams{Runtime: pgtype.Text{String: "gateway", Valid: true}, ID: id, OrganizationID: orgID})
	require.NoError(t, err)
	return id, orgID
}

func TestPostgresStateStoreRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "tailnetstate")
	require.NoError(t, err)
	enc := testenv.NewEncryptionClient(t)
	ingressID, orgID := seedIngress(t, db, "state-roundtrip")

	store, err := tailnetgateway.NewPostgresStateStore(db, enc, ingressID, orgID, 0)
	require.NoError(t, err)

	registered, err := store.Registered(ctx)
	require.NoError(t, err)
	require.False(t, registered)

	_, err = store.ReadState(ipn.MachineKeyStateKey)
	require.ErrorIs(t, err, ipn.ErrStateNotExist)

	value := []byte{0x00, 0xff, 'p', 'r', 'i', 'v', 'k', 'e', 'y', '\n'}
	require.NoError(t, store.WriteState(ipn.MachineKeyStateKey, value))

	got, err := store.ReadState(ipn.MachineKeyStateKey)
	require.NoError(t, err)
	require.Equal(t, value, got)

	registered, err = store.Registered(ctx)
	require.NoError(t, err)
	require.False(t, registered, "a machine key alone is not a completed login")

	row, err := repo.New(db).GetNetworkIngressNodeState(ctx, repo.GetNetworkIngressNodeStateParams{
		NetworkIngressID: ingressID, OrganizationID: orgID, Slot: 0, StateKey: string(ipn.MachineKeyStateKey),
	})
	require.NoError(t, err)
	require.NotEqual(t, base64.StdEncoding.EncodeToString(value), row, "state must not be stored as plain base64")
	decrypted, err := enc.Decrypt(row)
	require.NoError(t, err)
	decoded, err := base64.StdEncoding.DecodeString(decrypted)
	require.NoError(t, err)
	require.Equal(t, value, decoded, "the column holds the application-encrypted value")

	require.NoError(t, store.WriteState(ipn.MachineKeyStateKey, []byte("second")))
	got, err = store.ReadState(ipn.MachineKeyStateKey)
	require.NoError(t, err)
	require.Equal(t, []byte("second"), got)

	require.NoError(t, store.WriteState(ipn.MachineKeyStateKey, nil))
	_, err = store.ReadState(ipn.MachineKeyStateKey)
	require.ErrorIs(t, err, ipn.ErrStateNotExist)
}

func TestPostgresStateStoreIsolatedBySlot(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "tailnetstateslots")
	require.NoError(t, err)
	enc := testenv.NewEncryptionClient(t)
	ingressID, orgID := seedIngress(t, db, "state-slots")

	slot0, err := tailnetgateway.NewPostgresStateStore(db, enc, ingressID, orgID, 0)
	require.NoError(t, err)
	slot1, err := tailnetgateway.NewPostgresStateStore(db, enc, ingressID, orgID, 1)
	require.NoError(t, err)

	require.NoError(t, slot0.WriteState(ipn.CurrentProfileStateKey, []byte("zero")))
	_, err = slot1.ReadState(ipn.CurrentProfileStateKey)
	require.ErrorIs(t, err, ipn.ErrStateNotExist)

	registered, err := slot1.Registered(t.Context())
	require.NoError(t, err)
	require.False(t, registered)
}

func TestPostgresStateStoreRegisteredAndReset(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "tailnetstateregistered")
	require.NoError(t, err)
	enc := testenv.NewEncryptionClient(t)
	ingressID, orgID := seedIngress(t, db, "state-registered")
	store, err := tailnetgateway.NewPostgresStateStore(db, enc, ingressID, orgID, 0)
	require.NoError(t, err)

	// A failed login leaves a machine key and an empty profile list behind.
	require.NoError(t, store.WriteState(ipn.MachineKeyStateKey, []byte("machine")))
	require.NoError(t, store.WriteState(ipn.KnownProfilesStateKey, []byte("[]")))
	registered, err := store.Registered(ctx)
	require.NoError(t, err)
	require.False(t, registered)

	require.NoError(t, store.WriteState(ipn.KnownProfilesStateKey, []byte(`[{"ID":"p1","Name":"speakeasy-gw-0"}]`)))
	registered, err = store.Registered(ctx)
	require.NoError(t, err)
	require.True(t, registered)

	require.NoError(t, store.Reset(ctx))
	registered, err = store.Registered(ctx)
	require.NoError(t, err)
	require.False(t, registered)
	_, err = store.ReadState(ipn.MachineKeyStateKey)
	require.ErrorIs(t, err, ipn.ErrStateNotExist)
}

func TestNewPostgresStateStoreRejectsIncompleteIdentity(t *testing.T) {
	t.Parallel()
	enc := testenv.NewEncryptionClient(t)
	_, err := tailnetgateway.NewPostgresStateStore(nil, enc, uuid.New(), "org", 0)
	require.Error(t, err)
	db, err := infra.CloneTestDatabase(t, "tailnetstateinvalid")
	require.NoError(t, err)
	_, err = tailnetgateway.NewPostgresStateStore(db, enc, uuid.Nil, "org", 0)
	require.Error(t, err)
	_, err = tailnetgateway.NewPostgresStateStore(db, enc, uuid.New(), "org", -1)
	require.Error(t, err)
}
