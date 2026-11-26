package orgencryption_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
)

// dueAt is a moment when a DEK unwrapped now is due for refresh but not yet
// expired from the process cache.
func dueAt() time.Time {
	return time.Now().Add(orgencryption.DataKeyCacheTTL - time.Minute)
}

func warmKey(t *testing.T, ti *testInstance) (*orgencryption.Service, int) {
	t.Helper()
	ti.provision(t)
	service, _ := transactionService(t, ti)
	encryptor, err := service.For(ti.orgID, useCase)
	require.NoError(t, err)
	require.NoError(t, encryptor.RequireUsable(t.Context()))
	return service, ti.fixture.KMS.Decrypts()
}

func TestKeepWarmRefreshesAUsedKeyBeforeItExpires(t *testing.T) {
	t.Parallel()
	ti := newTestInstance(t)
	service, before := warmKey(t, ti)

	require.Zero(t, service.RefreshDue(t.Context(), time.Now(), useCase), "a fresh key is not due")
	require.Equal(t, 1, service.RefreshDue(t.Context(), dueAt(), useCase))
	require.Equal(t, before+1, ti.fixture.KMS.Decrypts(), "a refresh is a full KMS unwrap")
	require.Zero(t, service.RefreshDue(t.Context(), time.Now(), useCase), "the refreshed key starts a new period")

	encryptor, err := service.For(ti.orgID, useCase)
	require.NoError(t, err)
	_, err = encryptor.Encrypt(t.Context(), column, []byte("warm secret"))
	require.NoError(t, err)
	require.Equal(t, before+1, ti.fixture.KMS.Decrypts(), "callers use the refreshed key")
}

func TestKeepWarmSkipsOtherUseCases(t *testing.T) {
	t.Parallel()
	ti := newTestInstance(t)
	service, before := warmKey(t, ti)

	require.Zero(t, service.RefreshDue(t.Context(), dueAt(), orgencryption.UseCaseChatTranscripts))
	require.Equal(t, before, ti.fixture.KMS.Decrypts())
}

func TestKeepWarmLetsAnIdleKeyExpire(t *testing.T) {
	t.Parallel()
	ti := newTestInstance(t)
	service, before := warmKey(t, ti)

	require.Zero(t, service.RefreshDue(t.Context(), time.Now().Add(time.Hour), useCase))
	require.Equal(t, before, ti.fixture.KMS.Decrypts())
}

// A refresh reads the committed configuration, so a suspension stops it and
// the cached key expires on schedule instead of being extended.
func TestKeepWarmDoesNotRefreshASuspendedOrganization(t *testing.T) {
	t.Parallel()
	ti := newTestInstance(t)
	service, before := warmKey(t, ti)
	_, err := repo.New(ti.conn).SuspendEncryptionConfig(t.Context(), repo.SuspendEncryptionConfigParams{OrganizationID: ti.orgID, SuspendedReason: pgtype.Text{String: "keep warm test", Valid: true}})
	require.NoError(t, err)

	require.Zero(t, service.RefreshDue(t.Context(), dueAt(), useCase))
	require.Equal(t, before, ti.fixture.KMS.Decrypts())
}
