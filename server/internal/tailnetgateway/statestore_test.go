package tailnetgateway_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"tailscale.com/ipn"

	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/tailnetgateway"
	"github.com/speakeasy-api/gram/server/internal/tailnetgateway/repo"
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
	ingressID, orgID := seedIngress(t, db, "state-roundtrip")

	store := newStateStore(t, db, ingressID, orgID, 0)

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
	require.NotEqual(t, string(value), row, "state must not be stored in the clear")
	decrypted, err := gatewayEncryptor(t, db, orgID).Decrypt(ctx, nodeStateColumn, row)
	require.NoError(t, err)
	require.Equal(t, value, decrypted, "the column holds the value sealed under the organization's key")

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
	ingressID, orgID := seedIngress(t, db, "state-slots")

	slot0 := newStateStore(t, db, ingressID, orgID, 0)
	slot1 := newStateStore(t, db, ingressID, orgID, 1)

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
	ingressID, orgID := seedIngress(t, db, "state-registered")
	store := newStateStore(t, db, ingressID, orgID, 0)

	// A failed login leaves a machine key and an empty profile map behind.
	require.NoError(t, store.WriteState(ipn.MachineKeyStateKey, []byte("machine")))
	require.NoError(t, store.WriteState(ipn.KnownProfilesStateKey, []byte("{}")))
	registered, err := store.Registered(ctx)
	require.NoError(t, err)
	require.False(t, registered)

	// tailscaled persists known profiles as a map keyed by profile id, in the
	// shape ipn.LoginProfile serializes to.
	profile := ipn.LoginProfile{ID: "profile-1", Name: "speakeasy-gw-0", Key: ipn.StateKey("profile-1")}
	encoded, err := json.Marshal(map[ipn.ProfileID]ipn.LoginProfile{profile.ID: profile})
	require.NoError(t, err)
	require.NoError(t, store.WriteState(ipn.KnownProfilesStateKey, encoded))
	registered, err = store.Registered(ctx)
	require.NoError(t, err)
	require.True(t, registered)

	// The list shape older builds wrote is still recognised.
	require.NoError(t, store.WriteState(ipn.KnownProfilesStateKey, []byte(`[{"ID":"p1"}]`)))
	registered, err = store.Registered(ctx)
	require.NoError(t, err)
	require.True(t, registered)
	require.NoError(t, store.WriteState(ipn.KnownProfilesStateKey, []byte("not json")))
	_, err = store.Registered(ctx)
	require.Error(t, err)

	require.NoError(t, store.Reset(ctx))
	registered, err = store.Registered(ctx)
	require.NoError(t, err)
	require.False(t, registered)
	_, err = store.ReadState(ipn.MachineKeyStateKey)
	require.ErrorIs(t, err, ipn.ErrStateNotExist)
}

func TestNewPostgresStateStoreRejectsIncompleteIdentity(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "tailnetstateinvalid")
	require.NoError(t, err)
	enc := orgEncryption(t, db).Service
	_, err = tailnetgateway.NewPostgresStateStore(nil, enc, uuid.New(), "org", 0)
	require.Error(t, err)
	_, err = tailnetgateway.NewPostgresStateStore(db, nil, uuid.New(), "org", 0)
	require.Error(t, err, "organization encryption is required")
	_, err = tailnetgateway.NewPostgresStateStore(db, enc, uuid.Nil, "org", 0)
	require.Error(t, err)
	_, err = tailnetgateway.NewPostgresStateStore(db, enc, uuid.New(), "org", -1)
	require.Error(t, err)
}

func TestPostgresStateStoreSharesTheCertificateAcrossSlots(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "tailnetstatecert")
	require.NoError(t, err)
	ingressID, orgID := seedIngress(t, db, "state-cert")
	slot0 := newStateStore(t, db, ingressID, orgID, 0)
	slot1 := newStateStore(t, db, ingressID, orgID, 1)
	const domain = "private.example.ts.net"

	_, _, err = slot1.ReadTLSCertAndKey(domain)
	require.ErrorIs(t, err, ipn.ErrStateNotExist, "no certificate until one slot obtains it")

	require.NoError(t, slot0.WriteTLSCertAndKey(domain, []byte("cert-pem"), []byte("key-pem")))
	cert, key, err := slot1.ReadTLSCertAndKey(domain)
	require.NoError(t, err)
	require.Equal(t, []byte("cert-pem"), cert, "every slot serves the certificate the first one obtained")
	require.Equal(t, []byte("key-pem"), key)
	viaState, err := slot1.ReadState(ipn.StateKey(domain + ".crt"))
	require.NoError(t, err)
	require.Equal(t, []byte("cert-pem"), viaState, "tsnet's per-key reads see the shared certificate too")

	// A slot's own identity stays its own.
	require.NoError(t, slot0.WriteState(ipn.MachineKeyStateKey, []byte("machine-zero")))
	_, err = slot1.ReadState(ipn.MachineKeyStateKey)
	require.ErrorIs(t, err, ipn.ErrStateNotExist)

	// Resetting a slot's identity keeps the connection's certificate.
	require.NoError(t, slot0.Reset(t.Context()))
	_, _, err = slot0.ReadTLSCertAndKey(domain)
	require.NoError(t, err)
}

func TestPostgresStateStoreKeepsTheFirstACMEAccountKey(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "tailnetstateacme")
	require.NoError(t, err)
	ingressID, orgID := seedIngress(t, db, "state-acme")
	slot0 := newStateStore(t, db, ingressID, orgID, 0)
	slot1 := newStateStore(t, db, ingressID, orgID, 1)
	const accountKey = ipn.StateKey("acme-account.key.pem")

	require.NoError(t, slot0.WriteState(accountKey, []byte("account-zero")))
	require.NoError(t, slot1.WriteState(accountKey, []byte("account-one")))

	for _, store := range []*tailnetgateway.PostgresStateStore{slot0, slot1} {
		key, err := store.ReadState(accountKey)
		require.NoError(t, err)
		require.Equal(t, []byte("account-zero"), key, "slots settle on one ACME account, so renewals come from the account that issued the certificate")
	}
}

func TestPostgresStateStoreTreatsAnIncompleteCertificateAsMissing(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "tailnetstatecertpartial")
	require.NoError(t, err)
	ingressID, orgID := seedIngress(t, db, "state-cert-partial")
	slot0 := newStateStore(t, db, ingressID, orgID, 0)
	const domain = "private.example.ts.net"

	require.NoError(t, slot0.WriteState(ipn.StateKey(domain+".crt"), []byte("cert-pem")))
	_, _, err = slot0.ReadTLSCertAndKey(domain)
	require.ErrorIs(t, err, ipn.ErrStateNotExist)
}

func TestPostgresStateStoreSerializesCertificateIssuance(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "tailnetstatecertlock")
	require.NoError(t, err)
	ingressID, orgID := seedIngress(t, db, "state-cert-lock")
	slot0 := newStateStore(t, db, ingressID, orgID, 0)
	slot1 := newStateStore(t, db, ingressID, orgID, 1)

	unlock, err := slot0.LockCertificate(t.Context())
	require.NoError(t, err)

	waitCtx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	_, err = slot1.LockCertificate(waitCtx)
	require.Error(t, err, "another slot waits while one is obtaining the certificate")

	unlock()
	unlockOther, err := slot1.LockCertificate(t.Context())
	require.NoError(t, err, "the lock is free once the holder releases it")
	unlockOther()
}
