package deviceintegrations

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// stateReport is the subset of a stored state report the coverage queries
// read.
type stateReport struct {
	email   string
	serial  string
	at      time.Time
	etag    string
	halted  bool
	blocked bool
	tools   string
}

func seedStateReport(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, r stateReport) {
	t.Helper()
	tools := r.tools
	if tools == "" {
		tools = "[]"
	}
	require.NoError(t, testrepo.New(conn).InsertDeviceAgentStateReportFixture(ctx, testrepo.InsertDeviceAgentStateReportFixtureParams{
		OrganizationID:    orgID,
		Email:             r.email,
		SerialNumber:      r.serial,
		ReportedAt:        conv.ToPGTimestamptz(r.at),
		AppliedEtag:       r.etag,
		EnforcementHalted: r.halted,
		CurrentBlocked:    r.blocked,
		SpeakeasydVersion: "0.42.0",
		Tools:             []byte(tools),
	}))
}

// seedServedEtag records the etag a user's last poll was served. The user's
// agent sync row must already exist.
func seedServedEtag(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, email string, etag string) {
	t.Helper()
	require.NoError(t, testrepo.New(conn).SetDeviceAgentServedEtagFixture(ctx, testrepo.SetDeviceAgentServedEtagFixtureParams{
		ServedEtag:     conv.ToPGText(etag),
		OrganizationID: orgID,
		Email:          email,
	}))
}

func coverageCounts(t *testing.T, ctx context.Context, store *Store, orgID string, deviceLevel bool, now time.Time) repo.GetCoverageCountsRow {
	t.Helper()
	counts, err := store.repo.GetCoverageCounts(ctx, repo.GetCoverageCountsParams{
		DeviceLevel:    deviceLevel,
		ActiveCutoff:   conv.ToPGTimestamptz(now.Add(-activeWindow)),
		OrganizationID: orgID,
		Provider:       conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	return counts
}

func listDevices(t *testing.T, ctx context.Context, store *Store, orgID string, deviceLevel bool, now time.Time, policyState string) map[string]repo.ListManagedDevicesRow {
	t.Helper()
	rows, err := store.repo.ListManagedDevices(ctx, repo.ListManagedDevicesParams{
		DeviceLevel:    deviceLevel,
		ActiveCutoff:   conv.ToPGTimestamptz(now.Add(-activeWindow)),
		OrganizationID: orgID,
		Provider:       conv.ToPGTextEmpty(""),
		CursorID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Bucket:         conv.ToPGTextEmpty(""),
		PolicyState:    conv.ToPGTextEmpty(policyState),
		UserIds:        nil,
		UserEmails:     nil,
		PageLimit:      200,
	})
	require.NoError(t, err)
	byExternalID := make(map[string]repo.ListManagedDevicesRow, len(rows))
	for _, row := range rows {
		byExternalID[row.ExternalID] = row
	}
	return byExternalID
}

// TestPolicyStateUserLevel covers each policy_state under user-level
// matching, and that the four counts partition agent_active.
func TestPolicyStateUserLevel(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	cfg := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	now := time.Now().UTC()

	seed := func(name string) string {
		email := name + "@example.test"
		userID := seedUser(t, ctx, conn, email)
		seedDevice(t, ctx, conn, cfg.Config.ID, orgID, name, email, &userID, false)
		return email
	}

	// Applied the etag it was last served.
	attested := seed("attested")
	seedAgentSync(t, ctx, conn, orgID, attested, now)
	seedServedEtag(t, ctx, conn, orgID, attested, "e1")
	seedStateReport(t, ctx, conn, orgID, stateReport{email: attested, at: now, etag: "e1"})

	// Polled a newer policy but still enforces the old one.
	outdated := seed("outdated")
	seedAgentSync(t, ctx, conn, orgID, outdated, now)
	seedServedEtag(t, ctx, conn, orgID, outdated, "e2")
	seedStateReport(t, ctx, conn, orgID, stateReport{email: outdated, at: now, etag: "e1"})

	// Polls, but runs a build that does not report.
	silent := seed("silent")
	seedAgentSync(t, ctx, conn, orgID, silent, now)

	// Its last report is older than the coverage window.
	staleReport := seed("stale-report")
	seedAgentSync(t, ctx, conn, orgID, staleReport, now)
	seedServedEtag(t, ctx, conn, orgID, staleReport, "e1")
	seedStateReport(t, ctx, conn, orgID, stateReport{email: staleReport, at: now.Add(-3 * time.Hour), etag: "e1"})

	// A blocked build stops polling but keeps reporting, so the report alone
	// keeps the device agent_active.
	halted := seed("halted")
	seedAgentSync(t, ctx, conn, orgID, halted, now.Add(-48*time.Hour))
	seedServedEtag(t, ctx, conn, orgID, halted, "e1")
	seedStateReport(t, ctx, conn, orgID, stateReport{email: halted, at: now, etag: "e1", halted: true, blocked: true})

	// Not agent_active, so it has no policy state.
	stale := seed("stale")
	seedAgentSync(t, ctx, conn, orgID, stale, now.Add(-48*time.Hour))

	counts := coverageCounts(t, ctx, store, orgID, false, now)
	require.EqualValues(t, 5, counts.AgentActive)
	require.EqualValues(t, 1, counts.AgentStale)
	require.EqualValues(t, 1, counts.PolicyAttested)
	require.EqualValues(t, 1, counts.PolicyOutdated)
	require.EqualValues(t, 2, counts.PolicyUnreported, "a silent agent and a stale report both read unreported")
	require.EqualValues(t, 1, counts.EnforcementHalted)
	require.Equal(t, counts.AgentActive,
		counts.PolicyAttested+counts.PolicyOutdated+counts.PolicyUnreported+counts.EnforcementHalted,
		"policy states partition agent_active")

	rows := listDevices(t, ctx, store, orgID, false, now, "")
	want := map[string]string{
		"attested":     "attested",
		"outdated":     "outdated",
		"silent":       "unreported",
		"stale-report": "unreported",
		"halted":       "halted",
		"stale":        "",
	}
	for name, state := range want {
		require.Equal(t, state, rows[name].PolicyState, "%s", name)
	}
	require.Equal(t, "agent_active", rows["halted"].CoverageBucket)
	require.WithinDuration(t, now, rows["halted"].AgentLastSeenAt.Time, time.Second,
		"agent_last_seen_at counts the report as liveness")
	require.False(t, rows["silent"].HasReport)
	require.True(t, rows["stale-report"].HasReport, "a stale report still shows the last known state")
}

// TestPolicyStateDeviceLevel covers serial matching: a machine attests by its
// own report and never borrows one sent from another machine.
func TestPolicyStateDeviceLevel(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	cfg := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	now := time.Now().UTC()

	const email = "dev@example.test"
	userID := seedUser(t, ctx, conn, email)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "laptop", email, &userID, "SERIAL-LAPTOP", false)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "desktop", email, &userID, "SERIAL-DESKTOP", false)

	seedAgentSync(t, ctx, conn, orgID, email, now)
	seedServedEtag(t, ctx, conn, orgID, email, "e1")
	seedDeviceAgentSync(t, ctx, conn, orgID, "SERIAL-LAPTOP", email, now)
	seedDeviceAgentSync(t, ctx, conn, orgID, "SERIAL-DESKTOP", email, now)
	// Only the laptop reports. Serials are matched case-insensitively.
	seedStateReport(t, ctx, conn, orgID, stateReport{email: email, serial: "serial-laptop", at: now, etag: "e1"})

	counts := coverageCounts(t, ctx, store, orgID, true, now)
	require.EqualValues(t, 2, counts.AgentActive)
	require.EqualValues(t, 1, counts.PolicyAttested)
	require.EqualValues(t, 1, counts.PolicyUnreported,
		"the desktop has its own heartbeat, so it must not borrow the laptop's report")

	rows := listDevices(t, ctx, store, orgID, true, now, "")
	require.Equal(t, "attested", rows["laptop"].PolicyState)
	require.Equal(t, "unreported", rows["desktop"].PolicyState)
	require.Equal(t, "device", deviceView(ctx, testenv.NewLogger(t), rows["laptop"]).AgentState.Attestation,
		"the laptop's own agent sent the report")
}

// TestPolicyStateDeviceLevelReportAloneIsLiveness covers a blocked build
// under device-level matching: its serial heartbeat went stale, but its
// report is fresh.
func TestPolicyStateDeviceLevelReportAloneIsLiveness(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	cfg := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	now := time.Now().UTC()

	const email = "halted@example.test"
	userID := seedUser(t, ctx, conn, email)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "laptop", email, &userID, "SERIAL-H", false)
	seedAgentSync(t, ctx, conn, orgID, email, now.Add(-48*time.Hour))
	seedDeviceAgentSync(t, ctx, conn, orgID, "SERIAL-H", email, now.Add(-48*time.Hour))
	seedStateReport(t, ctx, conn, orgID, stateReport{email: email, serial: "SERIAL-H", at: now, etag: "e1", halted: true})

	counts := coverageCounts(t, ctx, store, orgID, true, now)
	require.EqualValues(t, 1, counts.AgentActive)
	require.EqualValues(t, 1, counts.EnforcementHalted)
	require.EqualValues(t, 1, counts.AgentActiveDeviceAttested, "the report was matched by serial")
}

// TestPolicyStateSharedMachinePrefersAssignedUser covers a machine two people
// sign in to: the report from the MDM-assigned user is the one shown.
func TestPolicyStateSharedMachinePrefersAssignedUser(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	cfg := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	now := time.Now().UTC()

	const owner = "owner@example.test"
	const guest = "guest@example.test"
	ownerID := seedUser(t, ctx, conn, owner)
	seedUser(t, ctx, conn, guest)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "shared", owner, &ownerID, "SERIAL-SHARED", false)
	seedDeviceAgentSync(t, ctx, conn, orgID, "SERIAL-SHARED", owner, now)
	seedAgentSync(t, ctx, conn, orgID, owner, now)
	seedServedEtag(t, ctx, conn, orgID, owner, "owner-etag")
	seedAgentSync(t, ctx, conn, orgID, guest, now)
	seedServedEtag(t, ctx, conn, orgID, guest, "guest-etag")

	seedStateReport(t, ctx, conn, orgID, stateReport{email: owner, serial: "SERIAL-SHARED", at: now.Add(-10 * time.Minute), etag: "owner-etag"})
	// The guest reported more recently, under the guest's own policy.
	seedStateReport(t, ctx, conn, orgID, stateReport{email: guest, serial: "SERIAL-SHARED", at: now, etag: "guest-etag"})

	rows := listDevices(t, ctx, store, orgID, true, now, "")
	require.Equal(t, "attested", rows["shared"].PolicyState)
	require.Equal(t, "owner-etag", rows["shared"].ReportAppliedEtag.String)
}

// TestListManagedDevicesPolicyStateFilterAndAgentState covers the drilldown
// filter and the decoded per-tool state.
func TestListManagedDevicesPolicyStateFilterAndAgentState(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	cfg := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	now := time.Now().UTC()

	for _, name := range []string{"good", "behind"} {
		email := name + "@example.test"
		userID := seedUser(t, ctx, conn, email)
		seedDevice(t, ctx, conn, cfg.Config.ID, orgID, name, email, &userID, false)
		seedAgentSync(t, ctx, conn, orgID, email, now)
		seedServedEtag(t, ctx, conn, orgID, email, "e2")
	}
	seedStateReport(t, ctx, conn, orgID, stateReport{email: "good@example.test", at: now, etag: "e2"})
	seedStateReport(t, ctx, conn, orgID, stateReport{
		email: "behind@example.test", at: now, etag: "e1",
		tools: `[{"tool":"claude_code","layer":"managed","outcome":"error","error_kind":"permission_denied","action_required":true,"drift_corrections":0}]`,
	})

	outdated := listDevices(t, ctx, store, orgID, false, now, "outdated")
	require.Len(t, outdated, 1)
	row, ok := outdated["behind"]
	require.True(t, ok)

	view := deviceView(ctx, testenv.NewLogger(t), row)
	require.NotNil(t, view.PolicyState)
	require.Equal(t, "outdated", *view.PolicyState)
	require.NotNil(t, view.AgentState)
	require.Equal(t, "e1", *view.AgentState.AppliedEtag)
	require.Equal(t, "user", view.AgentState.Attestation, "user-level matching cannot tie a report to this machine")
	require.Len(t, view.AgentState.Tools, 1)
	tool := view.AgentState.Tools[0]
	require.Equal(t, "claude_code", tool.Tool)
	require.Equal(t, "permission_denied", *tool.ErrorKind)
	require.True(t, tool.ActionRequired)
	require.NotNil(t, tool.DriftCorrections, "a measured zero is kept")
	require.Equal(t, 0, *tool.DriftCorrections)
	require.Nil(t, view.AgentState.LastRollback, "no rollback reported")

	rolledBackAt := now.Add(-time.Hour).UTC().Truncate(time.Second)
	require.NoError(t, testrepo.New(conn).SetDeviceAgentStateReportUpdateFixture(ctx, testrepo.SetDeviceAgentStateReportUpdateFixtureParams{
		AutoUpdate:       conv.ToPGText("automatic"),
		UpdateChannel:    conv.ToPGText("stable"),
		PinnedTarget:     conv.ToPGText("0.41.3"),
		LastRollbackAt:   conv.ToPGTimestamptz(rolledBackAt),
		LastRollbackFrom: conv.ToPGText("0.42.0"),
		LastRollbackTo:   conv.ToPGText("0.41.3"),
		OrganizationID:   orgID,
		Email:            "behind@example.test",
		SerialNumber:     "",
	}))
	view = deviceView(ctx, testenv.NewLogger(t), listDevices(t, ctx, store, orgID, false, now, "outdated")["behind"])
	require.Equal(t, "automatic", *view.AgentState.AutoUpdate)
	require.Equal(t, "stable", *view.AgentState.UpdateChannel)
	require.Equal(t, "0.41.3", *view.AgentState.PinnedTarget)
	require.NotNil(t, view.AgentState.LastRollback)
	require.Equal(t, rolledBackAt.Format(time.RFC3339), view.AgentState.LastRollback.At)
	require.Equal(t, "0.42.0", *view.AgentState.LastRollback.From)
	require.Equal(t, "0.41.3", *view.AgentState.LastRollback.To)
}
