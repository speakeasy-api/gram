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
	seedDeviceWithSerial(t, ctx, conn, configID, orgID, externalID, email, userID, "", missing)
}

// seedDeviceWithSerial is seedDevice plus the hardware serial the device-level
// coverage join matches on.
func seedDeviceWithSerial(t *testing.T, ctx context.Context, conn *pgxpool.Pool, configID uuid.UUID, orgID string, externalID string, email string, userID *string, serial string, missing bool) {
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
		SerialNumber:              serial,
		MissingSince:              missingSince,
	}))
}

// seedDeviceAgentSync records a per-DEVICE heartbeat (keyed on serial), the
// signal device-level coverage matches on.
func seedDeviceAgentSync(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, serial string, email string, lastSeen time.Time) {
	t.Helper()
	require.NoError(t, testrepo.New(conn).InsertDeviceAgentDeviceSyncFixture(ctx, testrepo.InsertDeviceAgentDeviceSyncFixtureParams{
		OrganizationID: orgID,
		SerialNumber:   serial,
		Email:          email,
		Hostname:       "",
		SeenAt:         conv.ToPGTimestamptz(lastSeen),
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
		DeviceLevel:    false,
		ActiveCutoff:   cutoff,
		OrganizationID: orgID,
		Provider:       conv.PtrToPGTextEmpty(nil),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), counts.AgentActive)
	require.Equal(t, int64(1), counts.AgentStale)
	require.Equal(t, int64(1), counts.NoAgent)
	require.Equal(t, int64(1), counts.NoEmail)
	require.Equal(t, int64(1), counts.UnresolvedEmail)
	require.Equal(t, int64(1), counts.Missing)
	require.Equal(t, int64(6), counts.Total)

	unmanaged, err := store.repo.CountUnmanagedAgentUsers(ctx, repo.CountUnmanagedAgentUsersParams{
		DeviceLevel:    false,
		OrganizationID: orgID,
		Provider:       conv.PtrToPGTextEmpty(nil),
	})
	require.NoError(t, err)
	// shadow@ has no device; stale@ and active@ have devices; gone@ has only
	// a missing device, which does not count as managed.
	require.Equal(t, int64(1), unmanaged)

	// Provider scoping: counts collapse to the named provider's devices, and
	// "unmanaged" means unmanaged BY THAT provider — users covered only by a
	// different MDM count as unmanaged for it.
	scoped, err := store.repo.GetCoverageCounts(ctx, repo.GetCoverageCountsParams{
		DeviceLevel:    false,
		ActiveCutoff:   cutoff,
		OrganizationID: orgID,
		Provider:       conv.PtrToPGTextEmpty(conv.PtrEmpty(testSinkProviderID)),
	})
	require.NoError(t, err)
	require.Zero(t, scoped.Total, "no devices belong to the sink provider")

	scopedUnmanaged, err := store.repo.CountUnmanagedAgentUsers(ctx, repo.CountUnmanagedAgentUsersParams{
		DeviceLevel:    false,
		OrganizationID: orgID,
		Provider:       conv.PtrToPGTextEmpty(conv.PtrEmpty(testSinkProviderID)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(3), scopedUnmanaged, "all agent users are unmanaged from the sink provider's view")
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
		DeviceLevel:    false,
		ActiveCutoff:   conv.ToPGTimestamptz(time.Now().UTC().Add(-activeWindow)),
		OrganizationID: orgID,
		Provider:       conv.PtrToPGTextEmpty(nil),
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
		DeviceLevel:    false,
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
		DeviceLevel:    false,
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
		DeviceLevel:    false,
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

// TestDeviceLevelCoverageDistinguishesMachines is the core of DNO-643: one
// user with two enrolled machines and the agent on only one. Under user-level
// matching both read covered; under device-level only the machine that
// actually reported does.
func TestDeviceLevelCoverageDistinguishesMachines(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	cfg := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	now := time.Now().UTC()

	const email = "dev@example.test"
	userID := seedUser(t, ctx, conn, email)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "laptop", email, &userID, "SERIAL-LAPTOP", false)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "desktop", email, &userID, "SERIAL-DESKTOP", false)

	// The user's agent runs on the laptop only. Both signals exist, because
	// the same poll writes both rows.
	seedAgentSync(t, ctx, conn, orgID, email, now)
	seedDeviceAgentSync(t, ctx, conn, orgID, "SERIAL-LAPTOP", email, now)

	cutoff := conv.ToPGTimestamptz(now.Add(-time.Hour))

	userLevel, err := store.repo.GetCoverageCounts(ctx, repo.GetCoverageCountsParams{
		DeviceLevel:    false,
		ActiveCutoff:   cutoff,
		OrganizationID: orgID,
		Provider:       conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, userLevel.AgentActive,
		"user-level matching cannot tell the machines apart: both read covered")
	require.EqualValues(t, 0, userLevel.AgentOtherDevice)

	deviceLevel, err := store.repo.GetCoverageCounts(ctx, repo.GetCoverageCountsParams{
		DeviceLevel:    true,
		ActiveCutoff:   cutoff,
		OrganizationID: orgID,
		Provider:       conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, deviceLevel.AgentActive, "only the laptop reported in")
	require.EqualValues(t, 1, deviceLevel.AgentOtherDevice,
		"the desktop's user runs the agent, just not there — distinct from never having installed it")
	require.EqualValues(t, 0, deviceLevel.NoAgent,
		"agent_other_device must not be double-counted as no_agent")
}

// TestDeviceLevelCoverageRescuesEmaillessDevice covers the larger practical
// win: a device the MDM records with no assigned user is unmatchable by email
// forever, but its own agent heartbeat makes it legible.
func TestDeviceLevelCoverageRescuesEmaillessDevice(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	cfg := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	now := time.Now().UTC()

	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "orphan", "", nil, "SERIAL-ORPHAN", false)
	seedDeviceAgentSync(t, ctx, conn, orgID, "SERIAL-ORPHAN", "whoever@example.test", now)

	cutoff := conv.ToPGTimestamptz(now.Add(-time.Hour))

	userLevel, err := store.repo.GetCoverageCounts(ctx, repo.GetCoverageCountsParams{
		DeviceLevel:    false,
		ActiveCutoff:   cutoff,
		OrganizationID: orgID,
		Provider:       conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, userLevel.NoEmail, "no email, no possible answer")
	require.EqualValues(t, 0, userLevel.AgentActive)

	deviceLevel, err := store.repo.GetCoverageCounts(ctx, repo.GetCoverageCountsParams{
		DeviceLevel:    true,
		ActiveCutoff:   cutoff,
		OrganizationID: orgID,
		Provider:       conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, deviceLevel.AgentActive,
		"a serial match outranks no_email: the machine answered for itself")
	require.EqualValues(t, 0, deviceLevel.NoEmail)
}

// TestDeviceLevelCoverageFallsBackToEmail pins graceful degradation: a device
// whose agent predates hardware reporting has no serial heartbeat, so
// device-level matching must fall back rather than report it uncovered.
func TestDeviceLevelCoverageFallsBackToEmail(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	cfg := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	now := time.Now().UTC()

	const email = "legacy@example.test"
	userID := seedUser(t, ctx, conn, email)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "legacy", email, &userID, "SERIAL-LEGACY", false)
	// Old agent: user-level heartbeat only, no device row.
	seedAgentSync(t, ctx, conn, orgID, email, now)

	counts, err := store.repo.GetCoverageCounts(ctx, repo.GetCoverageCountsParams{
		DeviceLevel:    true,
		ActiveCutoff:   conv.ToPGTimestamptz(now.Add(-time.Hour)),
		OrganizationID: orgID,
		Provider:       conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, counts.AgentOtherDevice,
		"with no serial heartbeat the honest answer is the weaker one, not uncovered")
	require.EqualValues(t, 0, counts.NoAgent)
}

// TestDeviceLevelCoverageMatchesSerialCaseInsensitively guards the join key:
// MDM vendors and the agent disagree on serial casing, and a case-sensitive
// match would silently report every device as uncovered.
func TestDeviceLevelCoverageMatchesSerialCaseInsensitively(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	cfg := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	now := time.Now().UTC()

	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "mixed", "", nil, "Serial-MiXeD", false)
	seedDeviceAgentSync(t, ctx, conn, orgID, "serial-mixed", "dev@example.test", now)

	counts, err := store.repo.GetCoverageCounts(ctx, repo.GetCoverageCountsParams{
		DeviceLevel:    true,
		ActiveCutoff:   conv.ToPGTimestamptz(now.Add(-time.Hour)),
		OrganizationID: orgID,
		Provider:       conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, counts.AgentActive, "serial matching is case-insensitive on both sides")
}

// TestDeviceLevelCoverageDualEnrollmentCountsBoth documents the collision
// rule: mdm_devices.serial_number is deliberately not unique, so a machine
// enrolled in two MDMs yields two rows and both truthfully report covered.
// The agent table's (org, serial) uniqueness is what keeps this from fanning
// out the device count.
func TestDeviceLevelCoverageDualEnrollmentCountsBoth(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	cfg := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	now := time.Now().UTC()

	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "jamf-copy", "", nil, "SERIAL-DUAL", false)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "intune-copy", "", nil, "SERIAL-DUAL", false)
	seedDeviceAgentSync(t, ctx, conn, orgID, "SERIAL-DUAL", "dev@example.test", now)

	counts, err := store.repo.GetCoverageCounts(ctx, repo.GetCoverageCountsParams{
		DeviceLevel:    true,
		ActiveCutoff:   conv.ToPGTimestamptz(now.Add(-time.Hour)),
		OrganizationID: orgID,
		Provider:       conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, counts.AgentActive, "both inventory rows describe the same covered machine")
	require.EqualValues(t, 2, counts.Total, "the heartbeat row must not fan out the device count")
}
