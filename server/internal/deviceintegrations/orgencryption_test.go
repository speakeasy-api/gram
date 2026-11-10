package deviceintegrations

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/providers"
	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/integrationcredentials"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// newOrgEncryptedSyncTestEnv is newSyncTestEnv for an organization enrolled
// in organization encryption.
func newOrgEncryptedSyncTestEnv(t *testing.T) (context.Context, *pgxpool.Pool, *Store, *Syncer, string, *orgencryptiontest.Fixture) {
	t.Helper()
	ctx, conn, _, orgID := newStoreTestDB(t)
	fixture := orgencryptiontest.New(t, conn)
	features := &feature.InMemory{}
	features.SetFlag(feature.FlagOrgEncryptedIntegrationCredentials, orgID, true)
	credentials := integrationcredentials.New(testenv.NewLogger(t), testenv.NewEncryptionClient(t), fixture.Service, fixture.Provisioner, features)
	store := NewStore(testenv.NewLogger(t), conn, credentials)
	syncer := NewSyncer(testenv.NewLogger(t), testenv.NewMeterProvider(t), conn, credentials, guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)), nil, nil)
	return ctx, conn, store, syncer, orgID, fixture
}

func TestOrgEncryptedCredentialsAreReadFromTheOrganizationCopy(t *testing.T) {
	t.Parallel()

	ctx, conn, store, syncer, orgID, _ := newOrgEncryptedSyncTestEnv(t)
	created := mustUpsert(t, ctx, conn, store, orgID, validCreds(), providers.Settings{
		"instance_url": "https://example.test",
		"devices":      "dev-1",
	}, true)

	row, err := store.repo.GetConfigByOrgAndProvider(ctx, repo.GetConfigByOrgAndProviderParams{OrganizationID: orgID, Provider: testProviderID})
	require.NoError(t, err)
	require.NotEmpty(t, row.CredentialsEncrypted, "the application-key copy is kept for rollback")
	require.True(t, row.CredentialsOrgEncrypted.Valid)
	require.NotContains(t, row.CredentialsOrgEncrypted.String, "secret-token")

	// With the application-key copy unreadable, reads can only succeed
	// through the organization copy.
	require.NoError(t, testrepo.New(conn).CorruptDeviceIntegrationCredentialsFixture(ctx, created.Config.ID))

	_, creds, err := store.LoadConfigWithCredentials(ctx, orgID, testProviderID)
	require.NoError(t, err)
	require.Equal(t, validCreds(), creds)

	require.NoError(t, syncer.RunSync(ctx, findSyncID(t, ctx, store, orgID, testProviderID)))
	state := scheduleStateFor(t, ctx, store, created.Config.ID, "testmdm_inventory")
	require.True(t, state.LastPollSuccessAt.Valid)
	require.False(t, state.LastPollFailedAt.Valid)
}

func TestRunSyncRetriesWhileTheOrganizationKeyIsUnavailable(t *testing.T) {
	t.Parallel()

	ctx, conn, store, syncer, orgID, fixture := newOrgEncryptedSyncTestEnv(t)
	created := mustUpsert(t, ctx, conn, store, orgID, validCreds(), providers.Settings{
		"instance_url": "https://example.test",
		"devices":      "dev-1",
	}, true)
	require.NoError(t, fixture.Provisioner.Suspend(ctx, orgID, "customer disabled the key", orgencryption.SystemActor()))

	err := syncer.RunSync(ctx, findSyncID(t, ctx, store, orgID, testProviderID))
	require.ErrorIs(t, err, orgencryption.ErrSuspended, "an unavailable key is retried as infrastructure")

	state := scheduleStateFor(t, ctx, store, created.Config.ID, "testmdm_inventory")
	require.False(t, state.LastPollFailedAt.Valid, "no credential failure is recorded, so the schedule never auto-pauses")
}
