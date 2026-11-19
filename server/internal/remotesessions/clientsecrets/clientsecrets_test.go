package clientsecrets_test

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/clientsecrets"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// secretColumns are the bare column names of both copies.
var secretColumns = []string{"client_secret_encrypted", "client_secret_org_encrypted"}

// clientSecretQueriesPath is the only queries file allowed to write either
// copy, relative to this package.
const clientSecretQueriesPath = "../clientsecret_queries.sql"

func noExpiry() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
}

func TestClientSecretQueriesFenceEveryCopyWrite(t *testing.T) {
	t.Parallel()

	queries, err := os.ReadFile(clientSecretQueriesPath)
	require.NoError(t, err)

	orgencryptiontest.RequireFencedWrites(t, string(queries), secretColumns...)
}

func TestSetInPlatformKeyWritesOnlyThePlatformCopy(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	owner := ti.projectOwner()
	client := ti.createClient(t, owner)

	row, err := ti.secrets.Set(t.Context(), ti.conn, client.ID, owner, []byte("s3cret"), noExpiry())
	require.NoError(t, err)
	require.True(t, row.ClientSecretEncrypted.Valid)
	require.False(t, row.ClientSecretOrgEncrypted.Valid)
	require.Equal(t, client.ClientSecretGeneration+1, row.ClientSecretGeneration)

	opened, err := ti.secrets.Open(t.Context(), ti.conn, owner, clientsecrets.Stored(row.ClientSecretEncrypted, row.ClientSecretOrgEncrypted))
	require.NoError(t, err)
	require.Equal(t, "s3cret", opened)
}

func TestSetInDualWriteWritesBothCopies(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	owner := ti.organizationOwner()
	client := ti.createClient(t, owner)

	row, err := ti.secrets.Set(t.Context(), ti.conn, client.ID, owner, []byte("s3cret"), noExpiry())
	require.NoError(t, err)
	require.True(t, row.ClientSecretEncrypted.Valid)
	require.True(t, row.ClientSecretOrgEncrypted.Valid)

	plaintext, err := ti.platform.Decrypt(row.ClientSecretEncrypted.String)
	require.NoError(t, err)
	require.Equal(t, "s3cret", plaintext, "the platform-key copy stays usable for a rollback")

	opened, err := ti.secrets.Open(t.Context(), ti.conn, owner, clientsecrets.Stored(pgtype.Text{String: "", Valid: false}, row.ClientSecretOrgEncrypted))
	require.NoError(t, err)
	require.Equal(t, "s3cret", opened, "dual_write reads the organization-key copy")
}

func TestSetAfterFinalizeWritesOnlyTheOrganizationCopy(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	ti.finalize(t)
	owner := ti.projectOwner()
	client := ti.createClient(t, owner)

	row, err := ti.secrets.Set(t.Context(), ti.conn, client.ID, owner, []byte("s3cret"), noExpiry())
	require.NoError(t, err)
	require.False(t, row.ClientSecretEncrypted.Valid)
	require.True(t, row.ClientSecretOrgEncrypted.Valid)

	opened, err := ti.secrets.Open(t.Context(), ti.conn, owner, clientsecrets.Stored(row.ClientSecretEncrypted, row.ClientSecretOrgEncrypted))
	require.NoError(t, err)
	require.Equal(t, "s3cret", opened)
}

func TestSetSealsAgainWhenTheTransitionMovesAfterTheDecision(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	owner := ti.organizationOwner()
	client := ti.createClient(t, owner)

	// Cache platform_key in this process, then enroll through another
	// process's transition state so the cached decision is stale.
	require.NoError(t, ti.secrets.RequireUsable(t.Context(), ti.conn, owner))
	other := orgencryption.NewTransitions(testenv.NewLogger(t), ti.conn, audit.NewLogger(), ti.fixture.Provisioner, orgencryption.TransitionsConfig{ClearSettleDelay: time.Millisecond})
	_, err := other.Enroll(t.Context(), ti.orgID, orgencryption.UseCaseOAuthClientSecrets, orgencryption.SystemActor())
	require.NoError(t, err)

	row, err := ti.secrets.Set(t.Context(), ti.conn, client.ID, owner, []byte("s3cret"), noExpiry())
	require.NoError(t, err)
	require.True(t, row.ClientSecretEncrypted.Valid)
	require.True(t, row.ClientSecretOrgEncrypted.Valid, "the fenced write sealed again under dual_write")
	require.Equal(t, client.ClientSecretGeneration+1, row.ClientSecretGeneration, "a retried write is one revision")
}

func TestSetReportsAMissingClient(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	owner := ti.organizationOwner()
	client := ti.createClient(t, owner)

	_, err := repo.New(ti.conn).SoftDeleteRemoteSessionClientFixture(t.Context(), client.ID)
	require.NoError(t, err)

	_, err = ti.secrets.Set(t.Context(), ti.conn, client.ID, owner, []byte("s3cret"), noExpiry())
	require.ErrorIs(t, err, clientsecrets.ErrClientNotFound)
}

func TestSetRefusesAnotherOrganizationsClient(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	client := ti.createClient(t, ti.organizationOwner())
	other := clientsecrets.OwnerOf(conv.ToPGText("org_"+uuid.NewString()), uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	_, err := ti.secrets.Set(t.Context(), ti.conn, client.ID, other, []byte("s3cret"), noExpiry())
	require.ErrorIs(t, err, clientsecrets.ErrClientNotFound)
}

func TestSetFailsClosedWhenTheOrganizationKeyIsSuspended(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	owner := ti.organizationOwner()
	client := ti.createClient(t, owner)
	require.NoError(t, ti.fixture.Provisioner.Suspend(t.Context(), ti.orgID, "customer disabled the key", orgencryption.SystemActor()))

	_, err := ti.secrets.Set(t.Context(), ti.conn, client.ID, owner, []byte("s3cret"), noExpiry())
	require.Error(t, err)
	require.True(t, orgencryption.IsKeyProblem(err), "a suspended key is a retryable key problem: %v", err)

	stored := ti.stored(t, client.ID)
	require.False(t, clientsecrets.Has(clientsecrets.Stored(stored.ClientSecretEncrypted, stored.ClientSecretOrgEncrypted)), "nothing falls back to the platform key")
}

func TestClearErasesBothCopies(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	owner := ti.organizationOwner()
	client := ti.createClient(t, owner)

	set, err := ti.secrets.Set(t.Context(), ti.conn, client.ID, owner, []byte("s3cret"), noExpiry())
	require.NoError(t, err)

	cleared, err := ti.secrets.Clear(t.Context(), ti.conn, client.ID, owner)
	require.NoError(t, err)
	require.False(t, cleared.ClientSecretEncrypted.Valid)
	require.False(t, cleared.ClientSecretOrgEncrypted.Valid)
	require.Equal(t, set.ClientSecretGeneration+1, cleared.ClientSecretGeneration)
	require.False(t, clientsecrets.Has(clientsecrets.Stored(cleared.ClientSecretEncrypted, cleared.ClientSecretOrgEncrypted)))
}

func TestGlobalClientStaysOnThePlatformKey(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	owner := clientsecrets.OwnerOf(pgtype.Text{String: "", Valid: false}, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	client := ti.createClient(t, owner)

	row, err := ti.secrets.Set(t.Context(), ti.conn, client.ID, owner, []byte("s3cret"), noExpiry())
	require.NoError(t, err)
	require.True(t, row.ClientSecretEncrypted.Valid)
	require.False(t, row.ClientSecretOrgEncrypted.Valid)

	opened, err := ti.secrets.Open(t.Context(), ti.conn, owner, clientsecrets.Stored(row.ClientSecretEncrypted, row.ClientSecretOrgEncrypted))
	require.NoError(t, err)
	require.Equal(t, "s3cret", opened)
}

func TestProjectClientWithoutOrganizationStaysOnThePlatformKeyAndBlocksFinalize(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	owner := clientsecrets.OwnerOf(pgtype.Text{String: "", Valid: false}, uuid.NullUUID{UUID: ti.projectID, Valid: true})
	client := ti.createClient(t, owner)

	row, err := ti.secrets.Set(t.Context(), ti.conn, client.ID, owner, []byte("s3cret"), noExpiry())
	require.NoError(t, err, "while the project's organization is in platform_key the secret is stored as before")
	require.True(t, row.ClientSecretEncrypted.Valid)
	require.False(t, row.ClientSecretOrgEncrypted.Valid, "a row without an organization cannot be sealed for one")

	opened, err := ti.secrets.Open(t.Context(), ti.conn, owner, clientsecrets.Stored(row.ClientSecretEncrypted, row.ClientSecretOrgEncrypted))
	require.NoError(t, err)
	require.Equal(t, "s3cret", opened)

	ti.enroll(t)
	counts, err := clientsecrets.NewCopyStore(ti.conn).Count(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.Equal(t, int64(1), counts.Unattributed, "the project's organization cannot finalize past it")

	_, err = ti.secrets.Set(t.Context(), ti.conn, client.ID, owner, []byte("replaced"), noExpiry())
	require.ErrorIs(t, err, clientsecrets.ErrUnattributed, "once enrolled, the secret is not replaced on the platform key")
	require.False(t, orgencryption.IsKeyProblem(err))
}

func TestFirstSecretForAProjectClientWithoutOrganizationIsRefusedAfterFinalize(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	owner := clientsecrets.OwnerOf(pgtype.Text{String: "", Valid: false}, uuid.NullUUID{UUID: ti.projectID, Valid: true})
	client := ti.createClient(t, owner)

	// A client without a secret never blocks finalize.
	ti.enroll(t)
	ti.finalize(t)

	_, err := ti.secrets.Set(t.Context(), ti.conn, client.ID, owner, []byte("s3cret"), noExpiry())
	require.ErrorIs(t, err, clientsecrets.ErrUnattributed)
	require.ErrorIs(t, ti.secrets.RequireUsable(t.Context(), ti.conn, owner), clientsecrets.ErrUnattributed, "a registration for it is refused before the provider is asked")

	stored := ti.stored(t, client.ID)
	require.False(t, stored.ClientSecretEncrypted.Valid, "no platform-key copy appears after finalize")
	require.False(t, stored.ClientSecretOrgEncrypted.Valid)
}

func TestBackfillVerifyAndFinalizeMoveSecretsToTheOrganizationKey(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	owner := ti.projectOwner()
	live := ti.createClient(t, owner)
	deleted := ti.createClient(t, owner)

	liveRow, err := ti.secrets.Set(t.Context(), ti.conn, live.ID, owner, []byte("live secret"), noExpiry())
	require.NoError(t, err)
	_, err = ti.secrets.Set(t.Context(), ti.conn, deleted.ID, owner, []byte("deleted secret"), noExpiry())
	require.NoError(t, err)
	_, err = repo.New(ti.conn).SoftDeleteRemoteSessionClientFixture(t.Context(), deleted.ID)
	require.NoError(t, err)

	store := clientsecrets.NewCopyStore(ti.conn)
	counts, err := store.Count(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.Equal(t, orgencryption.CopyCounts{Waiting: 2, Unreadable: 0, Unattributed: 0, PlatformCopies: 2}, counts)

	ti.enroll(t)
	result, err := orgencryption.NewRunner(testenv.NewLogger(t), ti.conn, ti.cipher, testenv.NewMeterProvider(t)).Run(t.Context(), ti.orgID, orgencryption.UseCaseOAuthClientSecrets, []orgencryption.CopyStore{store})
	require.NoError(t, err)
	require.Equal(t, int64(2), result.Converted, "soft-deleted clients are backfilled too")

	sealed := ti.stored(t, live.ID)
	require.True(t, sealed.ClientSecretOrgEncrypted.Valid)
	require.Equal(t, liveRow.ClientSecretGeneration, sealed.ClientSecretGeneration, "sealing the same secret again is not a new revision")
	require.Equal(t, liveRow.UpdatedAt, sealed.UpdatedAt, "the backfill changes nothing else about the client, so nothing derived from the row moves")

	ti.finalize(t)

	finalized := ti.stored(t, live.ID)
	require.False(t, finalized.ClientSecretEncrypted.Valid)
	require.Equal(t, liveRow.ClientSecretGeneration, finalized.ClientSecretGeneration)
	require.Equal(t, liveRow.UpdatedAt, finalized.UpdatedAt, "clearing the platform-key copies changes nothing else about the client")

	opened, err := ti.secrets.Open(t.Context(), ti.conn, owner, clientsecrets.Stored(finalized.ClientSecretEncrypted, finalized.ClientSecretOrgEncrypted))
	require.NoError(t, err)
	require.Equal(t, "live secret", opened)
}

func TestCopyStoreCountsUnreadableSecrets(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	owner := ti.organizationOwner()
	client := ti.createClient(t, owner)

	// A platform-key copy the platform key cannot open, as a restored or
	// rotated platform key would leave behind.
	_, err := repo.New(ti.conn).SetRemoteSessionClientSecret(t.Context(), repo.SetRemoteSessionClientSecretParams{
		PlatformCopy:          conv.ToPGText("not-a-platform-ciphertext"),
		OrgCopy:               pgtype.Text{String: "", Valid: false},
		ClientSecretExpiresAt: noExpiry(),
		ID:                    client.ID,
		OrganizationID:        owner.OrganizationID,
		DecidedGeneration:     0,
	})
	require.NoError(t, err)

	ti.enroll(t)
	store := clientsecrets.NewCopyStore(ti.conn)
	result, err := orgencryption.NewRunner(testenv.NewLogger(t), ti.conn, ti.cipher, testenv.NewMeterProvider(t)).Run(t.Context(), ti.orgID, orgencryption.UseCaseOAuthClientSecrets, []orgencryption.CopyStore{store})
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Unreadable)

	counts, err := store.Count(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.Equal(t, orgencryption.CopyCounts{Waiting: 0, Unreadable: 1, Unattributed: 0, PlatformCopies: 1}, counts)
}

// otherProcessSecrets is Secrets in another process: same database and keys,
// its own transition state cache.
func (ti *testInstance) otherProcessSecrets(t *testing.T) *clientsecrets.Secrets {
	t.Helper()

	transitions := orgencryption.NewTransitions(testenv.NewLogger(t), ti.conn, audit.NewLogger(), ti.fixture.Provisioner, orgencryption.TransitionsConfig{ClearSettleDelay: time.Millisecond})
	return clientsecrets.New(testenv.NewLogger(t), orgencryption.NewCipher(testenv.NewLogger(t), ti.platform, ti.fixture.Service, transitions), ti.platform)
}

func TestSetWithAStaleDualWriteDecisionWritesNoPlatformCopyAfterFinalize(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	owner := ti.organizationOwner()
	client := ti.createClient(t, owner)
	ti.enroll(t)

	// Another process caches dual_write, then this one finalizes.
	stale := ti.otherProcessSecrets(t)
	require.NoError(t, stale.RequireUsable(t.Context(), ti.conn, owner))
	ti.finalize(t)

	row, err := stale.Set(t.Context(), ti.conn, client.ID, owner, []byte("s3cret"), noExpiry())
	require.NoError(t, err)
	require.False(t, row.ClientSecretEncrypted.Valid, "a writer that decided under dual_write never writes a platform-key copy after finalize")
	require.True(t, row.ClientSecretOrgEncrypted.Valid)
}

func TestSetInsideATransactionSealsAgainAfterTheTransitionMoves(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	owner := ti.organizationOwner()
	client := ti.createClient(t, owner)

	require.NoError(t, ti.secrets.RequireUsable(t.Context(), ti.conn, owner))
	_, err := ti.otherProcessTransitions(t).Enroll(t.Context(), ti.orgID, orgencryption.UseCaseOAuthClientSecrets, orgencryption.SystemActor())
	require.NoError(t, err)

	tx := testenv.BeginTx(t, t.Context(), ti.conn)
	row, err := ti.secrets.Set(t.Context(), tx, client.ID, owner, []byte("s3cret"), noExpiry())
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	require.True(t, row.ClientSecretEncrypted.Valid)
	require.True(t, row.ClientSecretOrgEncrypted.Valid, "the fenced write sealed again under dual_write inside the transaction")

	stored := ti.stored(t, client.ID)
	require.Equal(t, row.ClientSecretOrgEncrypted, stored.ClientSecretOrgEncrypted)
}

// otherProcessTransitions is transition state in another process.
func (ti *testInstance) otherProcessTransitions(t *testing.T) *orgencryption.Transitions {
	t.Helper()

	return orgencryption.NewTransitions(testenv.NewLogger(t), ti.conn, audit.NewLogger(), ti.fixture.Provisioner, orgencryption.TransitionsConfig{ClearSettleDelay: time.Millisecond})
}

func TestOpenFailsClosedWhenTheOrganizationKeyIsSuspended(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	owner := ti.organizationOwner()
	client := ti.createClient(t, owner)
	row, err := ti.secrets.Set(t.Context(), ti.conn, client.ID, owner, []byte("s3cret"), noExpiry())
	require.NoError(t, err)
	require.NoError(t, ti.fixture.Provisioner.Suspend(t.Context(), ti.orgID, "customer disabled the key", orgencryption.SystemActor()))

	_, err = ti.secrets.Open(t.Context(), ti.conn, owner, clientsecrets.Stored(row.ClientSecretEncrypted, row.ClientSecretOrgEncrypted))
	require.Error(t, err, "nothing falls back to the platform-key copy")
	require.False(t, orgencryption.IsUnreadable(err), "a suspended key is retryable, not a broken secret: %v", err)
	require.True(t, orgencryption.IsKeyProblem(err))
}

func TestCopyOpensTheSourceAndSealsForTheTarget(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	global := clientsecrets.OwnerOf(pgtype.Text{String: "", Valid: false}, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	source := ti.createClient(t, global)
	sourceRow, err := ti.secrets.Set(t.Context(), ti.conn, source.ID, global, []byte("s3cret"), noExpiry())
	require.NoError(t, err)

	ti.enroll(t)
	owner := ti.organizationOwner()
	target := ti.createClient(t, owner)
	row, copied, err := ti.secrets.Copy(t.Context(), ti.conn, global, clientsecrets.Stored(sourceRow.ClientSecretEncrypted, sourceRow.ClientSecretOrgEncrypted), target.ID, owner, noExpiry())
	require.NoError(t, err)
	require.True(t, copied)
	require.True(t, row.ClientSecretOrgEncrypted.Valid, "a global client's secret is sealed again for the organization")
	require.NotEqual(t, sourceRow.ClientSecretEncrypted, row.ClientSecretEncrypted, "a ciphertext is never copied as stored")

	opened, err := ti.secrets.Open(t.Context(), ti.conn, owner, clientsecrets.Stored(row.ClientSecretEncrypted, row.ClientSecretOrgEncrypted))
	require.NoError(t, err)
	require.Equal(t, "s3cret", opened)

	_, copied, err = ti.secrets.Copy(t.Context(), ti.conn, global, clientsecrets.Stored(pgtype.Text{}, pgtype.Text{}), target.ID, owner, noExpiry())
	require.NoError(t, err)
	require.False(t, copied, "a source without a secret copies nothing")
}

func TestCopyStorePagesWaitingSecretsByID(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	owner := ti.organizationOwner()
	for range 3 {
		client := ti.createClient(t, owner)
		_, err := ti.secrets.Set(t.Context(), ti.conn, client.ID, owner, []byte("s3cret"), noExpiry())
		require.NoError(t, err)
	}

	store := clientsecrets.NewCopyStore(ti.conn)
	first, err := store.ListWaiting(t.Context(), ti.orgID, "", 2)
	require.NoError(t, err)
	require.Len(t, first, 2)
	rest, err := store.ListWaiting(t.Context(), ti.orgID, first[1].RowID, 2)
	require.NoError(t, err)
	require.Len(t, rest, 1)
	require.NotContains(t, []string{first[0].RowID, first[1].RowID}, rest[0].RowID)
}

func TestCopyStoreSealWaitingLandsOnlyOnTheCopyItRead(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	owner := ti.organizationOwner()
	client := ti.createClient(t, owner)
	_, err := ti.secrets.Set(t.Context(), ti.conn, client.ID, owner, []byte("old"), noExpiry())
	require.NoError(t, err)

	store := clientsecrets.NewCopyStore(ti.conn)
	waiting, err := store.ListWaiting(t.Context(), ti.orgID, "", 10)
	require.NoError(t, err)
	require.Len(t, waiting, 1)

	// The secret is replaced between the backfill's read and its write.
	_, err = ti.secrets.Set(t.Context(), ti.conn, client.ID, owner, []byte("new"), noExpiry())
	require.NoError(t, err)

	landed, err := store.SealWaiting(t.Context(), ti.orgID, 0, waiting[0], "organization-copy")
	require.NoError(t, err)
	require.False(t, landed, "a seal of a replaced platform-key copy never lands")
	require.False(t, ti.stored(t, client.ID).ClientSecretOrgEncrypted.Valid)
}

func TestCopyStoreClearPlatformCopiesIsFencedOnGeneration(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	owner := ti.organizationOwner()
	client := ti.createClient(t, owner)
	ti.enroll(t)
	_, err := ti.secrets.Set(t.Context(), ti.conn, client.ID, owner, []byte("s3cret"), noExpiry())
	require.NoError(t, err)

	cleared, err := clientsecrets.NewCopyStore(ti.conn).ClearPlatformCopies(t.Context(), ti.orgID, 0, 10)
	require.NoError(t, err)
	require.Zero(t, cleared, "a clear decided under a stale generation changes nothing")
	require.True(t, ti.stored(t, client.ID).ClientSecretEncrypted.Valid)
}
