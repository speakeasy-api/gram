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
	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/providers"
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

// TestDeviceLevelCoverageFallsBackToEmail is the regression test for the
// demotion bug: a device whose agent predates hardware reporting has no serial
// heartbeat, and enabling device-level matching must leave it agent_active on
// the weaker attestation — NOT move it into a warning bucket. Demoting it made
// a fully covered fleet read "0% are running the agent" while the evidence
// push simultaneously reported it active.
func TestDeviceLevelCoverageFallsBackToEmail(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	cfg := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	now := time.Now().UTC()

	const email = "legacy@example.test"
	userID := seedUser(t, ctx, conn, email)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "legacy", email, &userID, "SERIAL-LEGACY", false)
	// Old agent: user-level heartbeat only, no device row anywhere in the org.
	seedAgentSync(t, ctx, conn, orgID, email, now)

	counts, err := store.repo.GetCoverageCounts(ctx, repo.GetCoverageCountsParams{
		DeviceLevel:    true,
		ActiveCutoff:   conv.ToPGTimestamptz(now.Add(-time.Hour)),
		OrganizationID: orgID,
		Provider:       conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, counts.AgentActive,
		"the email fallback keeps the device covered; only the attestation strength is weaker")
	require.EqualValues(t, 0, counts.AgentOtherDevice,
		"agent_other_device needs positive evidence of the agent on a DIFFERENT identified machine")
	require.EqualValues(t, 0, counts.NoAgent)
}

// TestDeviceLevelCoverageModesAgreeWithoutSerials pins the property that makes
// the rollout safe: for an org whose agents report no serials at all (every org
// until the daemon ships), flipping the flag must not move a single device.
func TestDeviceLevelCoverageModesAgreeWithoutSerials(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	cfg := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	now := time.Now().UTC()

	// A fleet spanning every bucket the email path can produce.
	activeEmail := "active@example.test"
	activeUser := seedUser(t, ctx, conn, activeEmail)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "active", activeEmail, &activeUser, "S-ACTIVE", false)
	seedAgentSync(t, ctx, conn, orgID, activeEmail, now)

	staleEmail := "stale@example.test"
	staleUser := seedUser(t, ctx, conn, staleEmail)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "stale", staleEmail, &staleUser, "S-STALE", false)
	seedAgentSync(t, ctx, conn, orgID, staleEmail, now.Add(-25*time.Hour))

	noAgentEmail := "noagent@example.test"
	noAgentUser := seedUser(t, ctx, conn, noAgentEmail)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "noagent", noAgentEmail, &noAgentUser, "S-NOAGENT", false)

	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "noemail", "", nil, "S-NOEMAIL", false)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "stranger", "stranger@example.test", nil, "S-STRANGER", false)

	cutoff := conv.ToPGTimestamptz(now.Add(-time.Hour))
	params := func(deviceLevel bool) repo.GetCoverageCountsParams {
		return repo.GetCoverageCountsParams{
			DeviceLevel:    deviceLevel,
			ActiveCutoff:   cutoff,
			OrganizationID: orgID,
			Provider:       conv.ToPGTextEmpty(""),
		}
	}

	off, err := store.repo.GetCoverageCounts(ctx, params(false))
	require.NoError(t, err)
	on, err := store.repo.GetCoverageCounts(ctx, params(true))
	require.NoError(t, err)
	require.Equal(t, off, on,
		"with no serial heartbeats in the org, enabling device-level matching must be a no-op")
}

// TestCoverageBucketDefinitionsAgreeAcrossQueries guards the one risk of the
// bucket CASE being duplicated in GetCoverageCounts and ListManagedDevices:
// a divergence would leave devices reachable from no bucket, with the tile
// count and the drill-down list silently disagreeing.
func TestCoverageBucketDefinitionsAgreeAcrossQueries(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	cfg := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	now := time.Now().UTC()

	// One device per reachable bucket, under device-level matching.
	activeEmail := "dl-active@example.test"
	activeUser := seedUser(t, ctx, conn, activeEmail)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "dl-active", activeEmail, &activeUser, "D-ACTIVE", false)
	seedDeviceAgentSync(t, ctx, conn, orgID, "D-ACTIVE", activeEmail, now)
	// Same user, second machine with no heartbeat -> agent_other_device.
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "dl-other", activeEmail, &activeUser, "D-OTHER", false)

	staleEmail := "dl-stale@example.test"
	staleUser := seedUser(t, ctx, conn, staleEmail)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "dl-stale", staleEmail, &staleUser, "D-STALE", false)
	seedDeviceAgentSync(t, ctx, conn, orgID, "D-STALE", staleEmail, now.Add(-25*time.Hour))

	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "dl-noemail", "", nil, "D-NOEMAIL", false)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "dl-stranger", "who@example.test", nil, "D-STRANGER", false)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "dl-missing", "", nil, "D-MISSING", true)

	cutoff := conv.ToPGTimestamptz(now.Add(-time.Hour))

	for _, deviceLevel := range []bool{false, true} {
		counts, err := store.repo.GetCoverageCounts(ctx, repo.GetCoverageCountsParams{
			DeviceLevel:    deviceLevel,
			ActiveCutoff:   cutoff,
			OrganizationID: orgID,
			Provider:       conv.ToPGTextEmpty(""),
		})
		require.NoError(t, err)

		rows, err := store.repo.ListManagedDevices(ctx, repo.ListManagedDevicesParams{
			DeviceLevel:    deviceLevel,
			ActiveCutoff:   cutoff,
			OrganizationID: orgID,
			Provider:       conv.ToPGTextEmpty(""),
			CursorID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			Bucket:         conv.ToPGTextEmpty(""),
			PageLimit:      200,
		})
		require.NoError(t, err)

		listed := map[string]int64{}
		for _, row := range rows {
			listed[row.CoverageBucket]++
		}
		aggregate := map[string]int64{
			"agent_active":       counts.AgentActive,
			"agent_stale":        counts.AgentStale,
			"agent_other_device": counts.AgentOtherDevice,
			"no_agent":           counts.NoAgent,
			"no_email":           counts.NoEmail,
			"unresolved_email":   counts.UnresolvedEmail,
			"missing":            counts.Missing,
		}
		for bucket, want := range aggregate {
			require.Equal(t, want, listed[bucket],
				"device_level=%v: GetCoverageCounts and ListManagedDevices disagree on %s", deviceLevel, bucket)
		}
		require.EqualValues(t, len(rows), counts.Total, "device_level=%v: every device lands in exactly one bucket", deviceLevel)
	}
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

// TestCoverageAttestationDowngradesOnMixedEvidence pins the honesty rule for
// the headline claim: one email-matched active device downgrades the whole
// response to user attestation, because the strong sentence ("N devices are
// running the agent") would be false for that device.
func TestCoverageAttestationDowngradesOnMixedEvidence(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	cfg := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	now := time.Now().UTC()

	// One machine reports its own serial; a second is covered only through its
	// assigned user's heartbeat.
	attestedEmail := "attested@example.test"
	attestedUser := seedUser(t, ctx, conn, attestedEmail)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "attested", attestedEmail, &attestedUser, "S-ATTESTED", false)
	seedDeviceAgentSync(t, ctx, conn, orgID, "S-ATTESTED", attestedEmail, now)
	seedAgentSync(t, ctx, conn, orgID, attestedEmail, now)

	legacyEmail := "legacy@example.test"
	legacyUser := seedUser(t, ctx, conn, legacyEmail)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "legacy", legacyEmail, &legacyUser, "S-LEGACY", false)
	seedAgentSync(t, ctx, conn, orgID, legacyEmail, now)

	counts, err := store.repo.GetCoverageCounts(ctx, repo.GetCoverageCountsParams{
		DeviceLevel:    true,
		ActiveCutoff:   conv.ToPGTimestamptz(now.Add(-time.Hour)),
		OrganizationID: orgID,
		Provider:       conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, counts.AgentActive, "both devices are covered")
	require.EqualValues(t, 1, counts.AgentActiveDeviceAttested, "only one is backed by its own heartbeat")

	require.Equal(t, string(providers.AttestationUser),
		coverageAttestation(true, counts.AgentActive, counts.AgentActiveDeviceAttested),
		"a single email-matched active device downgrades the response's claim")
	require.Equal(t, string(providers.AttestationDevice),
		coverageAttestation(true, 2, 2),
		"all-device-attested earns the strong claim")
	require.Equal(t, string(providers.AttestationUser),
		coverageAttestation(false, 2, 0),
		"user-level matching never claims device attestation")
	require.Equal(t, string(providers.AttestationUser),
		coverageAttestation(true, 0, 0),
		"an empty active set cannot support the strong claim")
}

// TestCoverageAttestationIgnoresMissingDevices is the regression test for a
// counting bug in the attestation comparison: a device retired from MDM keeps
// its bucket 'missing', but its agent may still be installed and polling. If
// the device-attested count included it, that count could EXCEED agent_active
// and the equality check would never match again — permanently printing the
// weaker claim for an org whose every active device is serial-attested.
func TestCoverageAttestationIgnoresMissingDevices(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	cfg := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	now := time.Now().UTC()

	// Two present machines, both reporting their own serials.
	for _, d := range []struct{ ext, email, serial string }{
		{"present-a", "a@example.test", "S-A"},
		{"present-b", "b@example.test", "S-B"},
	} {
		userID := seedUser(t, ctx, conn, d.email)
		seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, d.ext, d.email, &userID, d.serial, false)
		seedDeviceAgentSync(t, ctx, conn, orgID, d.serial, d.email, now)
	}

	// A third machine was retired from MDM inventory, but the agent is still
	// installed and still reporting its serial.
	retiredEmail := "retired@example.test"
	retiredUser := seedUser(t, ctx, conn, retiredEmail)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "retired", retiredEmail, &retiredUser, "S-RETIRED", true)
	seedDeviceAgentSync(t, ctx, conn, orgID, "S-RETIRED", retiredEmail, now)

	counts, err := store.repo.GetCoverageCounts(ctx, repo.GetCoverageCountsParams{
		DeviceLevel:    true,
		ActiveCutoff:   conv.ToPGTimestamptz(now.Add(-time.Hour)),
		OrganizationID: orgID,
		Provider:       conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, counts.Missing)
	require.EqualValues(t, 2, counts.AgentActive)
	require.EqualValues(t, 2, counts.AgentActiveDeviceAttested,
		"the missing device's live heartbeat must not be counted among active ones")
	require.LessOrEqual(t, counts.AgentActiveDeviceAttested, counts.AgentActive,
		"device-attested can never exceed active, or the equality check below is unreachable")
	require.Equal(t, string(providers.AttestationDevice),
		coverageAttestation(true, counts.AgentActive, counts.AgentActiveDeviceAttested),
		"every ACTIVE device is serial-attested, so the strong claim holds")
}

// TestUnmanagedAgentUsersSerialEscapeHatch covers the device-level branch of
// CountUnmanagedAgentUsers, which had no test: every existing call passes
// DeviceLevel:false. The tile answers "agent users we know no device for", and
// under device-level matching the email link is no longer the only way a user
// can be managed — one of their machines may be in inventory under a different
// email. Without the escape hatch the tile calls a user unmanaged while the
// coverage list beside it shows that very device as active.
func TestUnmanagedAgentUsersSerialEscapeHatch(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	cfg := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	now := time.Now().UTC()

	// The agent reports under the SSO email; the MDM recorded a different one
	// for the same machine (a stale or personal address). The email join cannot
	// connect them, but the serial can.
	const agentEmail = "sso@example.test"
	seedAgentSync(t, ctx, conn, orgID, agentEmail, now)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "mismatched", "old-address@example.test", nil, "S-MISMATCH", false)
	seedDeviceAgentSync(t, ctx, conn, orgID, "S-MISMATCH", agentEmail, now)

	params := func(deviceLevel bool) repo.CountUnmanagedAgentUsersParams {
		return repo.CountUnmanagedAgentUsersParams{
			DeviceLevel:    deviceLevel,
			OrganizationID: orgID,
			Provider:       conv.ToPGTextEmpty(""),
		}
	}

	userLevel, err := store.repo.CountUnmanagedAgentUsers(ctx, params(false))
	require.NoError(t, err)
	require.EqualValues(t, 1, userLevel,
		"the email join cannot see the machine, so user-level matching reports the user unmanaged")

	deviceLevel, err := store.repo.CountUnmanagedAgentUsers(ctx, params(true))
	require.NoError(t, err)
	require.EqualValues(t, 0, deviceLevel,
		"the serial links the user to a managed device the email never could")
}

// TestUnmanagedAgentUsersIgnoresMissingAndOtherProviders keeps the escape hatch
// from being over-broad: a serial-matched device that has left MDM inventory, or
// belongs to a different provider than the one being scoped, must not count as
// managed.
func TestUnmanagedAgentUsersIgnoresMissingAndOtherProviders(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	cfg := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	now := time.Now().UTC()

	const agentEmail = "retired-hw@example.test"
	seedAgentSync(t, ctx, conn, orgID, agentEmail, now)
	// Serial matches, but the device is gone from inventory.
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "gone", "someone@example.test", nil, "S-GONE", true)
	seedDeviceAgentSync(t, ctx, conn, orgID, "S-GONE", agentEmail, now)

	count, err := store.repo.CountUnmanagedAgentUsers(ctx, repo.CountUnmanagedAgentUsersParams{
		DeviceLevel:    true,
		OrganizationID: orgID,
		Provider:       conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, count,
		"a device absent from inventory cannot make its user managed, serial match or not")
}

// TestDeviceLevelCoverageNullSerialMatchesNothing pins the SQL NULL trap: a
// device the MDM reports with no serial joins on LOWER(NULL), which is NULL and
// therefore matches no heartbeat row. Worth pinning because a well-meaning
// coalesce(d.serial_number, ”) on either side of that join would make every
// serial-less device collide with every other one.
func TestDeviceLevelCoverageNullSerialMatchesNothing(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	cfg := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	now := time.Now().UTC()

	// Two devices with NO serial in inventory, and an unrelated heartbeat row.
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "no-serial-a", "", nil, "", false)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "no-serial-b", "", nil, "", false)
	seedDeviceAgentSync(t, ctx, conn, orgID, "S-SOMEONE-ELSE", "other@example.test", now)

	counts, err := store.repo.GetCoverageCounts(ctx, repo.GetCoverageCountsParams{
		DeviceLevel:    true,
		ActiveCutoff:   conv.ToPGTimestamptz(now.Add(-time.Hour)),
		OrganizationID: orgID,
		Provider:       conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, counts.Total, "no fan-out between two serial-less devices")
	require.EqualValues(t, 2, counts.NoEmail, "both fall through to the email path, which has nothing either")
	require.EqualValues(t, 0, counts.AgentActive)
	require.EqualValues(t, 0, counts.AgentActiveDeviceAttested)
}

// TestDeviceLevelCoverageStaleOtherMachineIsNotOtherDevice pins the freshness
// requirement on agent_other_device. The bucket tells an admin "install it
// here", so it must mean the user is demonstrably running the agent NOW
// somewhere else — a heartbeat from a machine that went quiet months ago is not
// evidence of that.
func TestDeviceLevelCoverageStaleOtherMachineIsNotOtherDevice(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	cfg := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	now := time.Now().UTC()

	const email = "twomachines@example.test"
	userID := seedUser(t, ctx, conn, email)
	// Machine A ran the agent, but went quiet. Machine B never reported.
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "machine-a", email, &userID, "S-A", false)
	seedDeviceAgentSync(t, ctx, conn, orgID, "S-A", email, now.Add(-25*time.Hour))
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "machine-b", email, &userID, "S-B", false)
	seedAgentSync(t, ctx, conn, orgID, email, now.Add(-25*time.Hour))

	counts, err := store.repo.GetCoverageCounts(ctx, repo.GetCoverageCountsParams{
		DeviceLevel:    true,
		ActiveCutoff:   conv.ToPGTimestamptz(now.Add(-time.Hour)),
		OrganizationID: orgID,
		Provider:       conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	require.EqualValues(t, 0, counts.AgentOtherDevice,
		"a quiet machine elsewhere is not evidence the user runs the agent now")
	// Both land in agent_stale, and that is the intended unified fallback:
	// machine A's OWN heartbeat went quiet, and machine B has none of its own
	// while its assigned user's also went quiet. Identical to what flag-off
	// reports for the same fleet, which is the property that makes enabling
	// device-level matching safe.
	require.EqualValues(t, 2, counts.AgentStale)
	require.EqualValues(t, 2, counts.Total)

	off, err := store.repo.GetCoverageCounts(ctx, repo.GetCoverageCountsParams{
		DeviceLevel:    false,
		ActiveCutoff:   conv.ToPGTimestamptz(now.Add(-time.Hour)),
		OrganizationID: orgID,
		Provider:       conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	require.Equal(t, off.AgentStale, counts.AgentStale, "the stale classification is mode-independent")
}

// TestListManagedDevicesFiltersNewBucket exercises the drill-down for
// agent_other_device. The tile is only useful if clicking it returns exactly the
// devices it counted, and the filter reads the same lateral the projection does.
func TestListManagedDevicesFiltersNewBucket(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	cfg := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	now := time.Now().UTC()

	const email = "filter@example.test"
	userID := seedUser(t, ctx, conn, email)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "has-agent", email, &userID, "S-HAS", false)
	seedDeviceAgentSync(t, ctx, conn, orgID, "S-HAS", email, now)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "needs-agent", email, &userID, "S-NEEDS", false)

	rows, err := store.repo.ListManagedDevices(ctx, repo.ListManagedDevicesParams{
		DeviceLevel:    true,
		ActiveCutoff:   conv.ToPGTimestamptz(now.Add(-time.Hour)),
		OrganizationID: orgID,
		Provider:       conv.ToPGTextEmpty(""),
		CursorID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Bucket:         conv.ToPGTextEmpty("agent_other_device"),
		PageLimit:      50,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1, "the drill-down must return exactly what the tile counted")
	require.Equal(t, "needs-agent", rows[0].ExternalID)
	require.Equal(t, "agent_other_device", rows[0].CoverageBucket)
	// No timestamp, and that is correct rather than a gap in the fixture: the
	// evidence for this bucket lives on ANOTHER machine's heartbeat row, and
	// agent_last_seen_at reports only this device's own heartbeat or its
	// assigned user's aggregate one — neither exists here. Surfacing "last seen
	// on another machine at X" would need a new field, not this one.
	require.False(t, rows[0].AgentLastSeenAt.Valid,
		"agent_other_device has no heartbeat of its own to report")

	// The counted total and the drill-down must agree for this bucket too.
	counts, err := store.repo.GetCoverageCounts(ctx, repo.GetCoverageCountsParams{
		DeviceLevel:    true,
		ActiveCutoff:   conv.ToPGTimestamptz(now.Add(-time.Hour)),
		OrganizationID: orgID,
		Provider:       conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, counts.AgentOtherDevice)
}
