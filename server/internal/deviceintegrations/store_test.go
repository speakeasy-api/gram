package deviceintegrations

import (
	"context"
	"maps"
	"testing"
	"time"

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

func TestUpsertRejectsMalformedURLSetting(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	// URL-kind fields are syntax-checked at save time so a typo'd instance
	// URL fails at the sheet, not as a deterministic sync failure an hour
	// later.
	for _, bad := range []string{"tenant.example.test", "http://tenant.example.test", "https://"} {
		_, err := upsertTx(t, ctx, conn, store, orgID, validCreds(), providers.Settings{"instance_url": bad}, true)
		require.ErrorContains(t, err, "https URL", "value %q must be rejected", bad)
	}

	mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
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

func TestEnableTransitionMarksSyncsDue(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	created := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	require.True(t, created.SyncsMadeDue, "creation seeds schedules due")

	disabled := mustUpsert(t, ctx, conn, store, orgID, nil, validSettings(), false)
	require.False(t, disabled.SyncsMadeDue, "disabling makes nothing due")

	// Simulate a config disabled mid-interval: the sync already ran, so its
	// next poll sits in the future.
	require.NoError(t, testrepo.New(conn).DeferDeviceIntegrationSyncsFixture(ctx, created.Config.ID))

	// Re-enabling means "sync now": the schedules must be due again or the
	// immediate-sync trigger kicks a coordinator that finds nothing.
	reenabled := mustUpsert(t, ctx, conn, store, orgID, nil, validSettings(), true)
	require.True(t, reenabled.SyncsMadeDue, "an enable transition makes schedules due")
	rows, err := store.repo.ListSchedulesWithSync(ctx, created.Config.ID)
	require.NoError(t, err)
	require.LessOrEqual(t, rows[0].NextPollAfter.Time, time.Now().UTC().Add(time.Minute), "next poll is due now")

	// An enabled→enabled save with nothing else changed makes nothing due.
	steady := mustUpsert(t, ctx, conn, store, orgID, nil, validSettings(), true)
	require.False(t, steady.SyncsMadeDue, "a no-op save must not re-kick the sync machinery")
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

func TestSettingsRepointResetsSyncState(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	created := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)

	// Simulate a previously pushed snapshot digest.
	require.NoError(t, testrepo.New(conn).FailDeviceIntegrationSyncsFixture(ctx, testrepo.FailDeviceIntegrationSyncsFixtureParams{
		ErrorMessage:              conv.ToPGText("unauthorized"),
		LastPushDigest:            conv.ToPGText("stale-digest"),
		DeviceIntegrationConfigID: created.Config.ID,
	}))

	// A settings change without credentials is how the dashboard repoints a
	// push destination (secrets are write-only and not resupplied). The
	// coverage digest hashes only the fleet, so the stored digest MUST clear
	// or the newly pointed-at account receives nothing until the fleet
	// changes.
	repointed := mustUpsert(t, ctx, conn, store, orgID, nil, providers.Settings{"instance_url": "https://repointed.test"}, true)
	require.True(t, repointed.SyncsMadeDue, "a repoint resets sync state, so the caller must kick a sync rather than wait out the interval")
	digests, err := testrepo.New(conn).GetDeviceIntegrationSyncPushDigests(ctx, created.Config.ID)
	require.NoError(t, err)
	require.Len(t, digests, 1)
	require.False(t, digests[0].Valid, "a settings repoint clears last_push_digest so the new destination gets its first push")
	rows, err := store.repo.ListSchedulesWithSync(ctx, created.Config.ID)
	require.NoError(t, err)
	require.False(t, rows[0].LastPollFailedAt.Valid, "a repoint is a fresh start like a rotation")
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

func TestCredentialRotationMergesSavedSecrets(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)

	created, err := upsertProviderTx(t, ctx, conn, store, orgID, testRotateProviderID,
		providers.Credentials{"client_id": "original-id", "client_secret": "original-secret"},
		providers.Settings{"instance_url": "https://example.test"}, true)
	require.NoError(t, err)
	require.True(t, created.Created)

	// Rotate ONE secret: the omitted secret keeps its stored value, matching
	// the dashboard's "•••••• (saved)" placeholders.
	_, err = upsertProviderTx(t, ctx, conn, store, orgID, testRotateProviderID,
		providers.Credentials{"client_secret": "rotated-secret"},
		providers.Settings{}, true)
	require.NoError(t, err, "single-secret rotation must not demand every secret")

	_, creds, err := store.LoadConfigWithCredentials(ctx, orgID, testRotateProviderID)
	require.NoError(t, err)
	require.Equal(t, "original-id", creds["client_id"], "omitted secret keeps its stored value")
	require.Equal(t, "rotated-secret", creds["client_secret"])

	// A blank supplied value also keeps the stored secret rather than
	// clobbering it with an empty string.
	_, err = upsertProviderTx(t, ctx, conn, store, orgID, testRotateProviderID,
		providers.Credentials{"client_id": "", "client_secret": "rotated-again!"},
		providers.Settings{}, true)
	require.NoError(t, err)

	_, creds, err = store.LoadConfigWithCredentials(ctx, orgID, testRotateProviderID)
	require.NoError(t, err)
	require.Equal(t, "original-id", creds["client_id"], "blank supplied secret keeps the stored value")
	require.Equal(t, "rotated-again!", creds["client_secret"])
}

// TestProvisionRunsOnMergedConfig covers the P1: provisioning must see the
// fully-merged effective config, not the sparse client payload, so a partial
// update (a stored secret/setting the client omitted) still provisions.
func TestProvisionRunsOnMergedConfig(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)

	desc, ok := providers.Lookup(testProviderID)
	require.True(t, ok)

	var seenCreds providers.Credentials
	var seenSettings providers.Settings
	provision := func(_ context.Context, creds providers.Credentials, settings providers.Settings) (providers.Settings, error) {
		seenCreds = maps.Clone(creds)
		seenSettings = maps.Clone(settings)
		out := maps.Clone(settings)
		out["note"] = "provisioned"
		return out, nil
	}

	// Partial update: settings-only, no credentials — the client relies on the
	// stored api_key surviving the merge.
	err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		_, err := store.upsertWithTx(ctx, tx, desc, orgID, nil, providers.Settings{"instance_url": "https://example.test"}, true, provision)
		return err
	})
	require.NoError(t, err)

	require.Equal(t, "secret-token", seenCreds["api_key"], "provision runs on the merged credentials, not the sparse payload")
	require.Equal(t, "https://example.test", seenSettings["instance_url"])

	cfg, err := store.LoadConfig(ctx, orgID, testProviderID)
	require.NoError(t, err)
	require.Equal(t, "provisioned", cfg.Settings["note"], "settings the provision step returns are persisted")
}
