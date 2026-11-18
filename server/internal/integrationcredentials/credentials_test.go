package integrationcredentials_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/integrationcredentials"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

func TestQueriesFenceEveryCredentialWrite(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"queries.sql",
		"../slackdirectoryconnections/queries.sql",
		"../deviceintegrations/queries.sql",
		"../networkingress/queries.sql",
	} {
		queries, err := os.ReadFile(path)
		require.NoError(t, err)
		orgencryptiontest.RequireFencedWrites(t, string(queries), "credentials_encrypted", "credentials_org_encrypted")
	}
}

func TestStoreInPlatformKeyWritesOnlyThePlatformCopy(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	config := ti.createConfig(t)

	require.NoError(t, ti.credentials.Store(t.Context(), ti.conn, ti.orgID, integrationcredentials.DeviceIntegrationConfigs, []byte("token"), ti.writeConfig(config.ID)))

	stored := ti.stored(t, config.ID)
	require.True(t, stored.Platform.Valid)
	require.False(t, stored.Org.Valid)

	opened, err := ti.credentials.Open(t.Context(), nil, ti.orgID, integrationcredentials.DeviceIntegrationConfigs, stored)
	require.NoError(t, err)
	require.Equal(t, "token", string(opened))
}

func TestStoreInDualWriteWritesBothCopies(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	config := ti.createConfig(t)

	require.NoError(t, ti.credentials.Store(t.Context(), nil, ti.orgID, integrationcredentials.DeviceIntegrationConfigs, []byte("token"), ti.writeConfig(config.ID)))

	stored := ti.stored(t, config.ID)
	require.True(t, stored.Platform.Valid)
	require.True(t, stored.Org.Valid)
}

func TestStoreSealsAgainWhenTheTransitionMoves(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	config := ti.createConfig(t)
	write := ti.writeConfig(config.ID)
	attempts := 0

	err := ti.credentials.Store(t.Context(), ti.conn, ti.orgID, integrationcredentials.DeviceIntegrationConfigs, []byte("token"), func(ctx context.Context, copies orgencryption.Copies, generation int64) (bool, error) {
		attempts++
		if attempts == 1 {
			ti.enroll(t)
		}
		return write(ctx, copies, generation)
	})
	require.NoError(t, err)
	require.Equal(t, 2, attempts)

	stored := ti.stored(t, config.ID)
	require.True(t, stored.Org.Valid, "the second attempt seals for dual_write")
}

func TestStoreReportsAMissingRow(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)

	err := ti.credentials.Store(t.Context(), ti.conn, ti.orgID, integrationcredentials.DeviceIntegrationConfigs, []byte("token"), ti.writeConfig(uuid.New()))
	require.ErrorIs(t, err, integrationcredentials.ErrNotStored)
}

func TestSuspendedKeyIsACustomerKeyProblemNotUnreadable(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	config := ti.createConfig(t)
	require.NoError(t, ti.credentials.Store(t.Context(), ti.conn, ti.orgID, integrationcredentials.DeviceIntegrationConfigs, []byte("token"), ti.writeConfig(config.ID)))
	ti.finalize(t)
	require.NoError(t, ti.fixture.Provisioner.Suspend(t.Context(), ti.orgID, "customer disabled the key", orgencryption.SystemActor()))

	_, err := ti.credentials.Open(t.Context(), ti.conn, ti.orgID, integrationcredentials.DeviceIntegrationConfigs, ti.stored(t, config.ID))
	require.Error(t, err)
	require.False(t, integrationcredentials.IsUnreadable(err), "a suspended key never asks for new credentials: %v", err)
	require.True(t, integrationcredentials.IsCustomerKeyProblem(err), "%v", err)

	err = ti.credentials.RequireUsable(t.Context(), ti.conn, ti.orgID)
	require.Error(t, err)

	other := ti.createConfig(t)
	err = ti.credentials.Store(t.Context(), ti.conn, ti.orgID, integrationcredentials.DeviceIntegrationConfigs, []byte("token"), ti.writeConfig(other.ID))
	require.Error(t, err)
	require.False(t, integrationcredentials.Has(ti.stored(t, other.ID)), "nothing falls back to the platform key")
}

func TestBackfillAndFinalizeMoveCredentialsToTheOrganizationKey(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	config := ti.createConfig(t)
	require.NoError(t, ti.credentials.Store(t.Context(), ti.conn, ti.orgID, integrationcredentials.DeviceIntegrationConfigs, []byte("token"), ti.writeConfig(config.ID)))

	stores := integrationcredentials.CopyStores(ti.conn)
	var waiting int64
	for _, store := range stores {
		counts, err := store.Count(t.Context(), ti.orgID)
		require.NoError(t, err)
		waiting += counts.Waiting
	}
	require.Equal(t, int64(1), waiting)

	ti.enroll(t)
	result, err := orgencryption.NewRunner(testenv.NewLogger(t), ti.conn, ti.cipher, testenv.NewMeterProvider(t)).Run(t.Context(), ti.orgID, orgencryption.UseCaseIntegrationCredentials, stores)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Converted)

	ti.finalize(t)

	stored := ti.stored(t, config.ID)
	require.False(t, stored.Platform.Valid, "the platform-key copy is cleared")
	require.True(t, stored.Org.Valid)

	opened, err := ti.credentials.Open(t.Context(), ti.conn, ti.orgID, integrationcredentials.DeviceIntegrationConfigs, stored)
	require.NoError(t, err)
	require.Equal(t, "token", string(opened))
}
