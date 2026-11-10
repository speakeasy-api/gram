package cutover_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/cutover"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestSeal_UsesTheApplicationKeyAloneWhileTheOrganizationIsNotMoved(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)

	sealed, err := ti.sealer.Seal(t.Context(), ti.orgID, column, []byte("sk-secret"))
	require.NoError(t, err)
	require.False(t, sealed.Org.Valid)

	plaintext, err := testenv.NewEncryptionClient(t).Decrypt(sealed.Legacy)
	require.NoError(t, err)
	require.Equal(t, "sk-secret", plaintext)

	_, encryptErr := mustEncryptor(t, ti).Encrypt(t.Context(), column, []byte("probe"))
	require.ErrorIs(t, encryptErr, orgencryption.ErrNotProvisioned, "an organization that is not moved is never provisioned")
}

func TestSeal_SealsBothCopiesForAMovedOrganization(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.move(true)

	sealed, err := ti.sealer.Seal(t.Context(), ti.orgID, column, []byte("sk-secret"))
	require.NoError(t, err)
	require.True(t, sealed.Org.Valid)
	require.NotContains(t, sealed.Org.String, "sk-secret")
	require.Equal(t, "sk-secret", ti.openOrg(t, sealed.Org.String))

	plaintext, err := testenv.NewEncryptionClient(t).Decrypt(sealed.Legacy)
	require.NoError(t, err)
	require.Equal(t, "sk-secret", plaintext, "the application-key copy is kept for rollback")
}

func TestSeal_KeepsTheApplicationKeyCopyWhenOrganizationEncryptionFails(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.move(true)
	require.NoError(t, ti.fixture.Provisioner.EnsureUseCase(t.Context(), ti.orgID, cutover.AIProviderKeys.UseCase, orgencryption.SystemActor()))
	require.NoError(t, ti.fixture.Provisioner.Suspend(t.Context(), ti.orgID, "customer disabled the key", orgencryption.SystemActor()))

	sealed, err := ti.sealer.Seal(t.Context(), ti.orgID, column, []byte("sk-secret"))
	require.NoError(t, err)
	require.False(t, sealed.Org.Valid)

	plaintext, err := testenv.NewEncryptionClient(t).Decrypt(sealed.Legacy)
	require.NoError(t, err)
	require.Equal(t, "sk-secret", plaintext)
}

func TestSealApplicationKey_NeverSealsAnOrganizationCopy(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.move(true)

	sealed, err := ti.sealer.SealApplicationKey([]byte("sk-secret"))
	require.NoError(t, err)
	require.False(t, sealed.Org.Valid)
}

func TestOpen_ReadsTheOrganizationCopyForAMovedOrganization(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.move(true)

	sealed, err := ti.sealer.Seal(t.Context(), ti.orgID, column, []byte("sk-secret"))
	require.NoError(t, err)

	// A legacy value that cannot decrypt proves the organization copy is read.
	sealed.Legacy = "not-a-ciphertext"
	plaintext, err := ti.sealer.Open(t.Context(), ti.orgID, column, sealed, failBackfill(t))
	require.NoError(t, err)
	require.Equal(t, "sk-secret", plaintext)
}

func TestOpen_IgnoresTheOrganizationCopyWhileTheOrganizationIsNotMoved(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.move(true)

	sealed, err := ti.sealer.Seal(t.Context(), ti.orgID, column, []byte("sk-secret"))
	require.NoError(t, err)
	require.True(t, sealed.Org.Valid)

	ti.move(false)
	require.NoError(t, ti.fixture.Provisioner.Suspend(t.Context(), ti.orgID, "customer disabled the key", orgencryption.SystemActor()))

	plaintext, err := ti.sealer.Open(t.Context(), ti.orgID, column, sealed, failBackfill(t))
	require.NoError(t, err, "turning the flag off returns reads to the application-key copy")
	require.Equal(t, "sk-secret", plaintext)
}

func TestOpen_FailsClosedWhenTheOrganizationCopyDoesNotOpen(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.move(true)

	sealed, err := ti.sealer.Seal(t.Context(), ti.orgID, column, []byte("sk-secret"))
	require.NoError(t, err)
	require.NoError(t, ti.fixture.Provisioner.Suspend(t.Context(), ti.orgID, "customer disabled the key", orgencryption.SystemActor()))

	_, err = ti.sealer.Open(t.Context(), ti.orgID, column, sealed, failBackfill(t))
	require.ErrorIs(t, err, orgencryption.ErrSuspended, "a moved organization never falls back to the application key")
}

func TestOpen_RefusesAnOrganizationCopyStoredInAnotherColumn(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.move(true)

	sealed, err := ti.sealer.Seal(t.Context(), ti.orgID, column, []byte("sk-secret"))
	require.NoError(t, err)

	_, err = ti.sealer.Open(t.Context(), ti.orgID, "other_table.secret_org_encrypted", sealed, failBackfill(t))
	require.ErrorIs(t, err, orgencryption.ErrInvalidCiphertext)
}

func TestOpen_BackfillsARowWithoutAnOrganizationCopy(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)

	legacy, err := testenv.NewEncryptionClient(t).Encrypt([]byte("sk-secret"))
	require.NoError(t, err)
	stored := cutover.Ciphertexts{Legacy: legacy, Org: pgtype.Text{String: "", Valid: false}}

	plaintext, err := ti.sealer.Open(t.Context(), ti.orgID, column, stored, failBackfill(t))
	require.NoError(t, err)
	require.Equal(t, "sk-secret", plaintext, "an organization that is not moved is not backfilled")

	ti.move(true)
	var backfilled string
	plaintext, err = ti.sealer.Open(t.Context(), ti.orgID, column, stored, func(_ context.Context, org string) error {
		backfilled = org
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, "sk-secret", plaintext)
	require.NotEmpty(t, backfilled)
	require.Equal(t, "sk-secret", ti.openOrg(t, backfilled))

	stored.Org = conv.ToPGText(backfilled)
	plaintext, err = ti.sealer.Open(t.Context(), ti.orgID, column, stored, failBackfill(t))
	require.NoError(t, err)
	require.Equal(t, "sk-secret", plaintext)
}

func TestOpen_ReadsTheApplicationKeyCopyWithoutOrganizationEncryption(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.move(true)
	sealer := cutover.New(testenv.NewLogger(t), testenv.NewEncryptionClient(t), cutover.AIProviderKeys, nil)

	sealed, err := ti.sealer.Seal(t.Context(), ti.orgID, column, []byte("sk-secret"))
	require.NoError(t, err)
	require.True(t, sealed.Org.Valid)
	require.False(t, sealer.Moved(t.Context(), ti.orgID))

	plaintext, err := sealer.Open(t.Context(), ti.orgID, column, sealed, failBackfill(t))
	require.NoError(t, err)
	require.Equal(t, "sk-secret", plaintext)
}

func failBackfill(t *testing.T) cutover.Backfill {
	t.Helper()

	return func(context.Context, string) error {
		t.Error("backfill must not run")
		return nil
	}
}

func mustEncryptor(t *testing.T, ti *testInstance) *orgencryption.Encryptor {
	t.Helper()

	encryptor, err := ti.fixture.Service.For(ti.orgID, cutover.AIProviderKeys.UseCase)
	require.NoError(t, err)

	return encryptor
}
