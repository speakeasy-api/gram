package deviceintegrations

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// seedDevice inserts one mdm_devices row via the shared test fixture; the
// sync pipeline that normally writes these lands in a later ticket.
func seedDevice(t *testing.T, ctx context.Context, conn *pgxpool.Pool, configID uuid.UUID, orgID string, externalID string, email string, userID *string, missing bool) {
	t.Helper()
	missingSince := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	if missing {
		missingSince = conv.ToPGTimestamptz(time.Now().UTC())
	}
	require.NoError(t, testrepo.New(conn).InsertMdmDeviceFixture(ctx, testrepo.InsertMdmDeviceFixtureParams{
		DeviceIntegrationConfigID: configID,
		OrganizationID:            orgID,
		ExternalID:                externalID,
		UserEmail:                 email,
		UserID:                    conv.PtrToPGTextEmpty(userID),
		MissingSince:              missingSince,
	}))
}

func seedAgentSync(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, email string, lastSeen time.Time) {
	t.Helper()
	require.NoError(t, testrepo.New(conn).InsertDeviceAgentSyncFixture(ctx, testrepo.InsertDeviceAgentSyncFixtureParams{
		OrganizationID: orgID,
		Email:          email,
		SeenAt:         conv.ToPGTimestamptz(lastSeen),
	}))
}

func seedUser(t *testing.T, ctx context.Context, conn *pgxpool.Pool, email string) string {
	t.Helper()
	id := "user_" + uuid.NewString()
	require.NoError(t, testrepo.New(conn).InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{
		ID:          id,
		Email:       email,
		DisplayName: "Coverage Test User",
	}))
	return id
}

func TestCoverageBucketsAndUnmanagedAgents(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	created := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	configID := created.Config.ID

	now := time.Now().UTC()
	memberID := seedUser(t, ctx, conn, "member@example.test")
	staleUserID := seedUser(t, ctx, conn, "stale@example.test")

	// One device per bucket.
	seedAgentSync(t, ctx, conn, orgID, "Active@Example.Test", now) // case-insensitive match
	seedDevice(t, ctx, conn, configID, orgID, "d-active", "active@example.test", nil, false)

	seedAgentSync(t, ctx, conn, orgID, "stale@example.test", now.Add(-48*time.Hour))
	seedDevice(t, ctx, conn, configID, orgID, "d-stale", "stale@example.test", &staleUserID, false)

	seedDevice(t, ctx, conn, configID, orgID, "d-noagent", "member@example.test", &memberID, false)
	seedDevice(t, ctx, conn, configID, orgID, "d-noemail", "", nil, false)
	seedDevice(t, ctx, conn, configID, orgID, "d-unresolved", "ghost@example.test", nil, false)
	seedDevice(t, ctx, conn, configID, orgID, "d-missing", "gone@example.test", nil, true)

	// An agent user with no managed device at all.
	seedAgentSync(t, ctx, conn, orgID, "shadow@example.test", now)

	cutoff := conv.ToPGTimestamptz(now.Add(-activeWindow))
	counts, err := store.repo.GetCoverageCounts(ctx, repo.GetCoverageCountsParams{
		ActiveCutoff:   cutoff,
		OrganizationID: orgID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), counts.AgentActive)
	require.Equal(t, int64(1), counts.AgentStale)
	require.Equal(t, int64(1), counts.NoAgent)
	require.Equal(t, int64(1), counts.NoEmail)
	require.Equal(t, int64(1), counts.UnresolvedEmail)
	require.Equal(t, int64(1), counts.Missing)
	require.Equal(t, int64(6), counts.Total)

	unmanaged, err := store.repo.CountUnmanagedAgentUsers(ctx, orgID)
	require.NoError(t, err)
	// shadow@ has no device; stale@ and active@ have devices; gone@ has only
	// a missing device, which does not count as managed.
	require.Equal(t, int64(1), unmanaged)
}

func TestCoverageExcludesSoftDeletedConfigs(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	created := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	seedDevice(t, ctx, conn, created.Config.ID, orgID, "d-1", "someone@example.test", nil, false)

	require.NoError(t, pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		return store.softDeleteWithTx(ctx, tx, orgID, testProviderID)
	}))

	counts, err := store.repo.GetCoverageCounts(ctx, repo.GetCoverageCountsParams{
		ActiveCutoff:   conv.ToPGTimestamptz(time.Now().UTC().Add(-activeWindow)),
		OrganizationID: orgID,
	})
	require.NoError(t, err)
	require.Zero(t, counts.Total, "disconnected integrations disappear from coverage")
}

func TestListManagedDevicesBucketFilterAndPagination(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	created := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	configID := created.Config.ID

	now := time.Now().UTC()
	seedAgentSync(t, ctx, conn, orgID, "active@example.test", now)
	seedDevice(t, ctx, conn, configID, orgID, "d-active-1", "active@example.test", nil, false)
	seedDevice(t, ctx, conn, configID, orgID, "d-active-2", "active@example.test", nil, false)
	seedDevice(t, ctx, conn, configID, orgID, "d-noemail", "", nil, false)

	cutoff := conv.ToPGTimestamptz(now.Add(-activeWindow))

	// Bucket filter.
	rows, err := store.repo.ListManagedDevices(ctx, repo.ListManagedDevicesParams{
		ActiveCutoff:   cutoff,
		OrganizationID: orgID,
		Provider:       conv.PtrToPGTextEmpty(nil),
		CursorID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Bucket:         conv.PtrToPGTextEmpty(conv.PtrEmpty("agent_active")),
		PageLimit:      10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Equal(t, "agent_active", row.CoverageBucket)
	}

	// Pagination: page size 2 over 3 devices, newest first.
	page1, err := store.repo.ListManagedDevices(ctx, repo.ListManagedDevicesParams{
		ActiveCutoff:   cutoff,
		OrganizationID: orgID,
		Provider:       conv.PtrToPGTextEmpty(nil),
		CursorID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Bucket:         conv.PtrToPGTextEmpty(nil),
		PageLimit:      2,
	})
	require.NoError(t, err)
	require.Len(t, page1, 2)

	page2, err := store.repo.ListManagedDevices(ctx, repo.ListManagedDevicesParams{
		ActiveCutoff:   cutoff,
		OrganizationID: orgID,
		Provider:       conv.PtrToPGTextEmpty(nil),
		CursorID:       uuid.NullUUID{UUID: page1[1].ID, Valid: true},
		Bucket:         conv.PtrToPGTextEmpty(nil),
		PageLimit:      2,
	})
	require.NoError(t, err)
	require.Len(t, page2, 1)
	require.NotEqual(t, page1[0].ID, page2[0].ID)
	require.NotEqual(t, page1[1].ID, page2[0].ID)
}
