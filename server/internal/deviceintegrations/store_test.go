package deviceintegrations

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/providers"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestUpsertCreatesConfigWithSchedulesAndSyncs(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	result := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	require.True(t, result.Created)
	require.Equal(t, testProviderID, result.Config.Provider)
	require.Equal(t, "https://example.test", result.Config.Settings["instance_url"])

	rows, err := store.repo.ListSchedulesWithSync(ctx, result.Config.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "testmdm_inventory", rows[0].Schedule)
	require.False(t, rows[0].DisabledAt.Valid, "new schedules start enabled")
}

func TestUpsertRequiresCredentialsOnCreate(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	_, err := upsertTx(t, ctx, conn, store, orgID, nil, validSettings(), true)
	require.Error(t, err)
}

func TestUpsertRejectsUnknownAndMisroutedFields(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	// Unknown credential key.
	_, err := upsertTx(t, ctx, conn, store, orgID, providers.Credentials{"api_key": "x", "bogus": "y"}, validSettings(), true)
	require.Error(t, err)

	// A secret field supplied through settings must be rejected: it would be
	// stored readable.
	_, err = upsertTx(t, ctx, conn, store, orgID, validCreds(), providers.Settings{"instance_url": "https://example.test", "api_key": "leaked"}, true)
	require.Error(t, err)

	// Missing required non-secret field.
	_, err = upsertTx(t, ctx, conn, store, orgID, validCreds(), providers.Settings{}, true)
	require.Error(t, err)
}

func TestUpsertKeepsCredentialsWhenOmitted(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	created := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)

	// Settings-only update: no credentials supplied.
	updated := mustUpsert(t, ctx, conn, store, orgID, nil, providers.Settings{"instance_url": "https://example.test", "note": "updated"}, false)
	require.False(t, updated.Created)
	require.Equal(t, created.Config.ID, updated.Config.ID)
	require.False(t, updated.Config.Enabled)
	require.Equal(t, "updated", updated.Config.Settings["note"])

	_, creds, err := store.LoadConfigWithCredentials(ctx, orgID, testProviderID)
	require.NoError(t, err)
	require.Equal(t, "secret-token", creds["api_key"], "stored secrets survive settings-only updates")
}

func TestCredentialRotationKeepsConfigID(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	created := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)

	rotated := mustUpsert(t, ctx, conn, store, orgID, providers.Credentials{"api_key": "rotated-token"}, validSettings(), true)
	require.False(t, rotated.Created)
	require.Equal(t, created.Config.ID, rotated.Config.ID,
		"rotation must update in place: mdm_devices hang off the config id")

	_, creds, err := store.LoadConfigWithCredentials(ctx, orgID, testProviderID)
	require.NoError(t, err)
	require.Equal(t, "rotated-token", creds["api_key"])
}

func TestCredentialsStoredEncryptedAndNeverReadable(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	result := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)

	stored, err := testrepo.New(conn).GetDeviceIntegrationCredentialsCiphertext(ctx, result.Config.ID)
	require.NoError(t, err)
	require.NotContains(t, stored, "secret-token", "credentials must be encrypted at rest")

	cfg, err := store.LoadConfig(ctx, orgID, testProviderID)
	require.NoError(t, err)
	for _, v := range cfg.Settings {
		require.NotEqual(t, "secret-token", v, "secrets must never appear in readable settings")
	}
}

func TestUpsertClearsAutoPauseButNotUserDisable(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	created := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)

	// Simulate a poller auto-pause and a user disable.
	fixtures := testrepo.New(conn)
	require.NoError(t, fixtures.PauseDeviceIntegrationSyncsFixture(ctx, created.Config.ID))
	require.NoError(t, fixtures.DisableDeviceIntegrationSchedulesFixture(ctx, created.Config.ID))

	// Saving the config is the "try again" signal: auto-pause lifts, the
	// user's disable stays by construction (different table).
	mustUpsert(t, ctx, conn, store, orgID, nil, validSettings(), true)

	rows, err := store.repo.ListSchedulesWithSync(ctx, created.Config.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.False(t, rows[0].AutoPausedAt.Valid, "config save lifts auto-pause")
	require.Zero(t, rows[0].ConsecutiveFailures)
	require.True(t, rows[0].DisabledAt.Valid, "config save must not clear a user's disable")
}

func TestSoftDeleteThenReconnectStartsFresh(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	created := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)

	require.NoError(t, pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		return store.softDeleteWithTx(ctx, tx, orgID, testProviderID)
	}))

	cfg, err := store.LoadConfig(ctx, orgID, testProviderID)
	require.NoError(t, err)
	require.Nil(t, cfg, "soft-deleted config is invisible")

	reconnected := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	require.True(t, reconnected.Created)
	require.NotEqual(t, created.Config.ID, reconnected.Config.ID, "reconnect starts a fresh config")
}

func TestUpsertMergesOmittedOptionalSettings(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	mustUpsert(t, ctx, conn, store, orgID, validCreds(), providers.Settings{"instance_url": "https://example.test", "note": "keep me"}, true)

	// Rotate credentials while omitting the optional "note" key entirely.
	rotated := mustUpsert(t, ctx, conn, store, orgID, providers.Credentials{"api_key": "rotated-token"}, validSettings(), true)
	require.Equal(t, "keep me", rotated.Config.Settings["note"], "omitted optional settings keys keep their stored values")

	// Supplying the key overwrites it.
	updated := mustUpsert(t, ctx, conn, store, orgID, nil, providers.Settings{"instance_url": "https://example.test", "note": "changed"}, true)
	require.Equal(t, "changed", updated.Config.Settings["note"])
}

func TestCredentialRotationResetsSyncState(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	created := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)

	// Simulate accumulated failure state plus a pushed-snapshot digest.
	require.NoError(t, testrepo.New(conn).FailDeviceIntegrationSyncsFixture(ctx, testrepo.FailDeviceIntegrationSyncsFixtureParams{
		ErrorMessage:              conv.ToPGText("unauthorized"),
		LastPushDigest:            conv.ToPGText("stale-digest"),
		DeviceIntegrationConfigID: created.Config.ID,
	}))

	// Settings-only save lifts the pause but keeps failure history.
	mustUpsert(t, ctx, conn, store, orgID, nil, validSettings(), true)
	rows, err := store.repo.ListSchedulesWithSync(ctx, created.Config.ID)
	require.NoError(t, err)
	require.True(t, rows[0].LastPollFailedAt.Valid, "settings-only saves keep failure history")

	// Credential rotation is a fresh start: failure state and digest clear.
	mustUpsert(t, ctx, conn, store, orgID, providers.Credentials{"api_key": "rotated-token"}, validSettings(), true)
	rows, err = store.repo.ListSchedulesWithSync(ctx, created.Config.ID)
	require.NoError(t, err)
	require.False(t, rows[0].LastPollFailedAt.Valid, "rotation clears failure state")
	require.False(t, rows[0].AutoPausedAt.Valid)
	require.Zero(t, rows[0].ConsecutiveFailures)
	digests, err := testrepo.New(conn).GetDeviceIntegrationSyncPushDigests(ctx, created.Config.ID)
	require.NoError(t, err)
	require.Len(t, digests, 1)
	require.False(t, digests[0].Valid, "rotation clears last_push_digest so a repointed sink gets its first push")
}

func TestUpsertReturnsTransactionObservedBefore(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	created := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	require.Nil(t, created.Before, "create has no before state")

	updated := mustUpsert(t, ctx, conn, store, orgID, nil, providers.Settings{"instance_url": "https://changed.test"}, false)
	require.NotNil(t, updated.Before)
	require.Equal(t, "https://example.test", updated.Before.Settings["instance_url"])
	require.True(t, updated.Before.Enabled)
	require.False(t, updated.Config.Enabled)
}
