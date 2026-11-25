package ingresscredentials_test

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/networkingress/ingresscredentials"
	"github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// credentialColumns are the bare column names of both copies.
var credentialColumns = []string{"credentials_encrypted", "credentials_org_encrypted"}

// credentialQueriesPath is the only queries file allowed to write either
// copy, relative to this package.
const credentialQueriesPath = "../credential_queries.sql"

// document is a credential document as the service stores it.
var document = []byte(`{"client_id":"client-id","client_secret":"client-secret"}`)

func TestCredentialQueriesFenceEveryCopyWrite(t *testing.T) {
	t.Parallel()

	queries, err := os.ReadFile(credentialQueriesPath)
	require.NoError(t, err)

	orgencryptiontest.RequireFencedWrites(t, string(queries), credentialColumns...)
}

func TestSetInPlatformKeyWritesOnlyThePlatformCopy(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ingress := ti.createIngress(t)

	row, err := ti.credentials.Set(t.Context(), ti.conn, ingress.ID, ti.orgID, document)
	require.NoError(t, err)
	require.True(t, row.CredentialsEncrypted.Valid)
	require.False(t, row.CredentialsOrgEncrypted.Valid)
	require.Equal(t, "pending", row.Status)

	opened, err := ti.credentials.Open(t.Context(), ti.conn, ti.orgID, ingresscredentials.Stored(row))
	require.NoError(t, err)
	require.JSONEq(t, string(document), string(opened))
}

func TestSetInDualWriteWritesBothCopies(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	ingress := ti.createIngress(t)

	row, err := ti.credentials.Set(t.Context(), ti.conn, ingress.ID, ti.orgID, document)
	require.NoError(t, err)
	require.True(t, row.CredentialsEncrypted.Valid)
	require.True(t, row.CredentialsOrgEncrypted.Valid)

	plaintext, err := ti.platform.Decrypt(row.CredentialsEncrypted.String)
	require.NoError(t, err)
	require.JSONEq(t, string(document), plaintext, "the platform-key copy stays usable for a rollback")

	orgOnly := orgencryption.Copies{Platform: pgtype.Text{String: "", Valid: false}, Org: row.CredentialsOrgEncrypted}
	opened, err := ti.credentials.Open(t.Context(), ti.conn, ti.orgID, orgOnly)
	require.NoError(t, err)
	require.JSONEq(t, string(document), string(opened), "dual_write reads the organization-key copy")
}

func TestSetAfterFinalizeWritesOnlyTheOrganizationCopy(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	ti.finalize(t)
	ingress := ti.createIngress(t)

	row, err := ti.credentials.Set(t.Context(), ti.conn, ingress.ID, ti.orgID, document)
	require.NoError(t, err)
	require.False(t, row.CredentialsEncrypted.Valid)
	require.True(t, row.CredentialsOrgEncrypted.Valid)

	opened, err := ti.credentials.Open(t.Context(), ti.conn, ti.orgID, ingresscredentials.Stored(row))
	require.NoError(t, err)
	require.JSONEq(t, string(document), string(opened))
}

func TestSetSealsAgainWhenTheTransitionMovesAfterTheDecision(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ingress := ti.createIngress(t)

	// Cache platform_key in this process, then enroll through another
	// process's transition state so the cached decision is stale.
	_, err := ti.credentials.Set(t.Context(), ti.conn, ingress.ID, ti.orgID, document)
	require.NoError(t, err)
	other := orgencryption.NewTransitions(testenv.NewLogger(t), ti.conn, audit.NewLogger(), ti.fixture.Provisioner, orgencryption.TransitionsConfig{ClearSettleDelay: time.Millisecond})
	_, err = other.Enroll(t.Context(), ti.orgID, ingresscredentials.UseCase, orgencryption.SystemActor())
	require.NoError(t, err)

	row, err := ti.credentials.Set(t.Context(), ti.conn, ingress.ID, ti.orgID, document)
	require.NoError(t, err)
	require.True(t, row.CredentialsEncrypted.Valid)
	require.True(t, row.CredentialsOrgEncrypted.Valid, "the fenced write sealed again under dual_write")
}

func TestSetRefusesAnotherOrganizationsIngress(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ingress := ti.createIngress(t)

	_, err := ti.credentials.Set(t.Context(), ti.conn, ingress.ID, "org_"+uuid.NewString(), document)
	require.ErrorIs(t, err, ingresscredentials.ErrIngressNotFound)
}

func TestSetFailsClosedWhenTheOrganizationKeyIsSuspended(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	ingress := ti.createIngress(t)
	require.NoError(t, ti.fixture.Provisioner.Suspend(t.Context(), ti.orgID, "customer disabled the key", orgencryption.SystemActor()))

	_, err := ti.credentials.Set(t.Context(), ti.conn, ingress.ID, ti.orgID, document)
	require.Error(t, err)
	require.True(t, orgencryption.IsKeyProblem(err), "a suspended key is a key problem: %v", err)
	require.False(t, ingresscredentials.Has(ingresscredentials.Stored(ti.stored(t, ingress.ID))), "nothing falls back to the platform key")
}

func TestOpenAfterFinalizeRefusesAPlatformOnlyCopy(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	ti.finalize(t)
	ingress := ti.createIngress(t)

	platform, err := ti.platform.Encrypt(document)
	require.NoError(t, err)
	row, err := repo.New(ti.conn).SetNetworkIngressCredentialCopiesFixture(t.Context(), repo.SetNetworkIngressCredentialCopiesFixtureParams{
		PlatformCopy: pgtype.Text{String: platform, Valid: true}, OrgCopy: pgtype.Text{String: "", Valid: false}, ID: ingress.ID, OrganizationID: ti.orgID,
	})
	require.NoError(t, err)

	_, err = ti.credentials.Open(t.Context(), ti.conn, ti.orgID, ingresscredentials.Stored(row))
	require.True(t, orgencryption.IsUnreadable(err), "org_kms never reads a platform-key copy: %v", err)
}

func TestBackfillVerifyAndFinalizeMoveCredentialsToTheOrganizationKey(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	live := ti.createIngress(t)

	liveRow, err := ti.credentials.Set(t.Context(), ti.conn, live.ID, ti.orgID, document)
	require.NoError(t, err)
	_, err = repo.New(ti.conn).SoftDeleteNetworkIngress(t.Context(), ti.orgID)
	require.NoError(t, err)
	deleted := ti.stored(t, live.ID)
	require.True(t, deleted.Deleted, "a tombstone keeps its credentials for cleanup")

	store := ingresscredentials.NewCopyStore(ti.conn)
	counts, err := store.Count(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.False(t, counts.LastWrittenAt.IsZero())
	require.Equal(t, orgencryption.CopyCounts{Waiting: 1, Unreadable: 0, Unattributed: 0, PlatformCopies: 1, Values: 1, LastWrittenAt: counts.LastWrittenAt}, counts)

	ti.enroll(t)
	result, err := orgencryption.NewRunner(testenv.NewLogger(t), ti.conn, ti.cipher, testenv.NewMeterProvider(t)).Run(t.Context(), ti.orgID, ingresscredentials.UseCase, []orgencryption.CopyStore{store})
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Converted, "soft-deleted ingresses are backfilled too")

	sealed := ti.stored(t, live.ID)
	require.True(t, sealed.CredentialsOrgEncrypted.Valid)
	require.Equal(t, deleted.UpdatedAt, sealed.UpdatedAt, "the backfill is not new desired state for the reconciler")

	ti.finalize(t)

	finalized := ti.stored(t, live.ID)
	require.False(t, finalized.CredentialsEncrypted.Valid)
	require.Equal(t, deleted.UpdatedAt, finalized.UpdatedAt, "clearing the platform-key copies is not new desired state")
	require.NotEqual(t, liveRow.CredentialsOrgEncrypted, finalized.CredentialsOrgEncrypted)

	opened, err := ti.credentials.Open(t.Context(), ti.conn, ti.orgID, ingresscredentials.Stored(finalized))
	require.NoError(t, err)
	require.JSONEq(t, string(document), string(opened))
}

func TestCopyStoreCountsUnreadableCredentials(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ingress := ti.createIngress(t)
	_, err := repo.New(ti.conn).SetNetworkIngressCredentialCopiesFixture(t.Context(), repo.SetNetworkIngressCredentialCopiesFixtureParams{
		PlatformCopy: pgtype.Text{String: "not a platform ciphertext", Valid: true}, OrgCopy: pgtype.Text{String: "", Valid: false}, ID: ingress.ID, OrganizationID: ti.orgID,
	})
	require.NoError(t, err)

	ti.enroll(t)
	store := ingresscredentials.NewCopyStore(ti.conn)
	result, err := orgencryption.NewRunner(testenv.NewLogger(t), ti.conn, ti.cipher, testenv.NewMeterProvider(t)).Run(t.Context(), ti.orgID, ingresscredentials.UseCase, []orgencryption.CopyStore{store})
	require.NoError(t, err)
	require.Equal(t, int64(0), result.Converted)

	counts, err := store.Count(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.False(t, counts.LastWrittenAt.IsZero())
	require.Equal(t, orgencryption.CopyCounts{Waiting: 0, Unreadable: 1, Unattributed: 0, PlatformCopies: 1, Values: 1, LastWrittenAt: counts.LastWrittenAt}, counts)
}

func TestCopyStoreReportsStoredCredentials(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	none := pgtype.Text{String: "", Valid: false}
	plant := func(ingress repo.NetworkIngress, platform, org pgtype.Text) {
		_, err := repo.New(ti.conn).SetNetworkIngressCredentialCopiesFixture(t.Context(), repo.SetNetworkIngressCredentialCopiesFixtureParams{
			PlatformCopy: platform, OrgCopy: org, ID: ingress.ID, OrganizationID: ti.orgID,
		})
		require.NoError(t, err)
	}

	deleted := ti.createIngress(t)
	plant(deleted, pgtype.Text{String: "platform-copy", Valid: true}, none)
	_, err := repo.New(ti.conn).SoftDeleteNetworkIngress(t.Context(), ti.orgID)
	require.NoError(t, err)
	live := ti.createIngress(t)
	plant(live, none, pgtype.Text{String: "org-copy", Valid: true})

	lastWrittenAt := ti.stored(t, deleted.ID).UpdatedAt.Time
	if liveAt := ti.stored(t, live.ID).UpdatedAt.Time; liveAt.After(lastWrittenAt) {
		lastWrittenAt = liveAt
	}

	store := ingresscredentials.NewCopyStore(ti.conn)
	listed, err := store.ListOrganizationsWithValues(t.Context())
	require.NoError(t, err)
	var organization orgencryption.OrganizationValues
	for _, h := range listed {
		if h.OrganizationID == ti.orgID {
			organization = h
		}
	}
	require.Equal(t, int64(2), organization.Values, "the soft-deleted ingress keeps its credentials")
	require.True(t, lastWrittenAt.Equal(organization.LastWrittenAt), "listed %v, stored %v", organization.LastWrittenAt, lastWrittenAt)

	counted, err := store.Count(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.Equal(t, organization.Values, counted.Values)
	require.True(t, organization.LastWrittenAt.Equal(counted.LastWrittenAt))

	other := "org_" + uuid.NewString()
	empty, err := store.Count(t.Context(), other)
	require.NoError(t, err)
	require.Zero(t, empty.Values)
	require.True(t, empty.LastWrittenAt.IsZero())
}
