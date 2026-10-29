package deviceintegrations

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

type fleetFilter struct {
	provider    string
	inInventory *bool
	cursor      uuid.NullUUID
	limit       int32
}

func listFleet(t *testing.T, ctx context.Context, store *Store, orgID string, deviceLevel bool, now time.Time, filter fleetFilter) []repo.ListFleetDevicesRow {
	t.Helper()
	inInventory := pgtype.Bool{Bool: false, Valid: false}
	if filter.inInventory != nil {
		inInventory = pgtype.Bool{Bool: *filter.inInventory, Valid: true}
	}
	limit := filter.limit
	if limit == 0 {
		limit = 200
	}
	rows, err := store.repo.ListFleetDevices(ctx, repo.ListFleetDevicesParams{
		DeviceLevel:    deviceLevel,
		ActiveCutoff:   conv.ToPGTimestamptz(now.Add(-activeWindow)),
		OrganizationID: orgID,
		Provider:       conv.ToPGTextEmpty(filter.provider),
		InInventory:    inInventory,
		CursorID:       filter.cursor,
		Bucket:         conv.ToPGTextEmpty(""),
		PolicyState:    conv.ToPGTextEmpty(""),
		UserIds:        nil,
		UserEmails:     nil,
		PageLimit:      limit,
	})
	require.NoError(t, err)
	return rows
}

func fleetCounts(t *testing.T, ctx context.Context, store *Store, orgID string, deviceLevel bool, now time.Time, provider string) repo.GetFleetCoverageCountsRow {
	t.Helper()
	counts, err := store.repo.GetFleetCoverageCounts(ctx, repo.GetFleetCoverageCountsParams{
		DeviceLevel:    deviceLevel,
		ActiveCutoff:   conv.ToPGTimestamptz(now.Add(-activeWindow)),
		OrganizationID: orgID,
		Provider:       conv.ToPGTextEmpty(provider),
	})
	require.NoError(t, err)
	return counts
}

// fleetKey names a fleet row by what a person would: its serial, or its user
// when the agent reports none.
func fleetKey(row repo.ListFleetDevicesRow) string {
	if row.SerialNumber.Valid {
		return row.SerialNumber.String
	}
	return row.UserEmail.String
}

func byFleetKey(rows []repo.ListFleetDevicesRow) map[string]repo.ListFleetDevicesRow {
	out := make(map[string]repo.ListFleetDevicesRow, len(rows))
	for _, row := range rows {
		out[fleetKey(row)] = row
	}
	return out
}

// TestFleetWithoutMDMListsAgentMachines is the case the fleet exists for: an
// organization with no MDM connected still sees every machine its agents
// report, with the same buckets and policy states an MDM row would carry.
func TestFleetWithoutMDMListsAgentMachines(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	now := time.Now().UTC()

	// A: heartbeat and a report that applies the served policy.
	seedAgentSync(t, ctx, conn, orgID, "a@example.test", now)
	seedDeviceAgentSync(t, ctx, conn, orgID, "S-A", "a@example.test", now)
	seedServedEtag(t, ctx, conn, orgID, "a@example.test", "served")
	seedStateReport(t, ctx, conn, orgID, stateReport{email: "a@example.test", serial: "S-A", at: now, etag: "served"})
	// B: a report with a serial and no heartbeat, on an older policy.
	seedAgentSync(t, ctx, conn, orgID, "b@example.test", now)
	seedServedEtag(t, ctx, conn, orgID, "b@example.test", "served")
	seedStateReport(t, ctx, conn, orgID, stateReport{email: "b@example.test", serial: "S-B", at: now, etag: "older"})
	// C: a user heartbeat from an agent that predates serial reporting.
	seedAgentSync(t, ctx, conn, orgID, "c@example.test", now)
	// D: a serial-less report from a halted build.
	seedAgentSync(t, ctx, conn, orgID, "d@example.test", now)
	seedStateReport(t, ctx, conn, orgID, stateReport{email: "d@example.test", at: now, halted: true})
	// E: a machine that went quiet.
	seedAgentSync(t, ctx, conn, orgID, "e@example.test", now.Add(-25*time.Hour))
	seedDeviceAgentSync(t, ctx, conn, orgID, "S-E", "e@example.test", now.Add(-25*time.Hour))
	// F: a serial machine whose user heartbeat must not add a second row.
	seedAgentSync(t, ctx, conn, orgID, "f@example.test", now)
	seedDeviceAgentSync(t, ctx, conn, orgID, "S-F", "f@example.test", now)

	for _, deviceLevel := range []bool{false, true} {
		rows := listFleet(t, ctx, store, orgID, deviceLevel, now, fleetFilter{})
		require.Len(t, rows, 6, "device_level=%v", deviceLevel)
		got := byFleetKey(rows)

		want := map[string]struct {
			bucket, policy string
			bySerial       bool
		}{
			"S-A":            {"agent_active", "attested", true},
			"S-B":            {"agent_active", "outdated", true},
			"c@example.test": {"agent_active", "unreported", false},
			"d@example.test": {"agent_active", "halted", false},
			"S-E":            {"agent_stale", "", true},
			"S-F":            {"agent_active", "unreported", true},
		}
		for key, w := range want {
			row, ok := got[key]
			require.True(t, ok, "device_level=%v: %s is listed", deviceLevel, key)
			require.False(t, row.InInventory, key)
			require.Empty(t, row.Provider, key)
			require.Equal(t, w.bucket, row.CoverageBucket, key)
			require.Equal(t, w.policy, row.PolicyState, key)
			if row.HasReport {
				require.Equal(t, w.bySerial, row.ReportBySerial, key)
			}
			require.True(t, strings.HasPrefix(fleetDeviceID(row), "agent:"), key)
		}

		counts := fleetCounts(t, ctx, store, orgID, deviceLevel, now, "")
		require.False(t, counts.MdmConnected)
		require.EqualValues(t, 6, counts.Total)
		require.EqualValues(t, 6, counts.AgentOnly)
		require.EqualValues(t, 5, counts.AgentActive)
		require.EqualValues(t, 1, counts.AgentStale)
		require.EqualValues(t, 3, counts.AgentActiveDeviceAttested, "the serial machines answer for themselves")
		require.EqualValues(t, 1, counts.PolicyAttested)
		require.EqualValues(t, 1, counts.PolicyOutdated)
		require.EqualValues(t, 2, counts.PolicyUnreported)
		require.EqualValues(t, 1, counts.EnforcementHalted)
		require.Equal(t, "user", fleetAttestation(counts.AgentActive, counts.AgentActiveDeviceAttested))
	}
}

// TestFleetMergesAgentMachinesIntoInventory checks the dedupe: an agent
// machine an MDM holds is one row, the MDM's, and only machines no inventory
// holds are added beside it.
func TestFleetMergesAgentMachinesIntoInventory(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	cfg := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	now := time.Now().UTC()

	inEmail := "in@example.test"
	inUser := seedUser(t, ctx, conn, inEmail)
	require.NoError(t, testrepo.New(conn).CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{
		OrganizationID: orgID,
		UserID:         conv.ToPGText(inUser),
	}))
	// The MDM's serial casing differs from the agent's on purpose.
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "mdm-in", inEmail, &inUser, "s-in", false)
	seedAgentSync(t, ctx, conn, orgID, inEmail, now)
	seedDeviceAgentSync(t, ctx, conn, orgID, "S-IN", inEmail, now)
	// The same user's second machine, which no MDM holds.
	seedDeviceAgentSync(t, ctx, conn, orgID, "S-OUT", inEmail, now)

	// A retired device still running the agent keeps its one MDM row.
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "mdm-gone", inEmail, &inUser, "S-GONE", true)
	seedDeviceAgentSync(t, ctx, conn, orgID, "S-GONE", inEmail, now)

	// A serial-less agent the MDM row already counts through its email.
	legacyEmail := "legacy@example.test"
	legacyUser := seedUser(t, ctx, conn, legacyEmail)
	seedDevice(t, ctx, conn, cfg.Config.ID, orgID, "mdm-legacy", legacyEmail, &legacyUser, false)
	seedAgentSync(t, ctx, conn, orgID, legacyEmail, now)

	// A serial-less agent with no MDM device.
	seedAgentSync(t, ctx, conn, orgID, "loose@example.test", now)

	for _, deviceLevel := range []bool{false, true} {
		rows := listFleet(t, ctx, store, orgID, deviceLevel, now, fleetFilter{})
		require.Len(t, rows, 5, "device_level=%v: mdm-in, mdm-gone, mdm-legacy, S-OUT and loose", deviceLevel)
		got := byFleetKey(rows)

		require.True(t, got["s-in"].InInventory)
		require.Equal(t, "agent_active", got["s-in"].CoverageBucket)
		require.Equal(t, "missing", got["S-GONE"].CoverageBucket)
		require.True(t, got["S-GONE"].InInventory)
		require.True(t, got[legacyEmail].InInventory)
		require.Equal(t, "agent_active", got[legacyEmail].CoverageBucket)
		require.False(t, got["S-OUT"].InInventory)
		require.Equal(t, inUser, got["S-OUT"].UserID.String, "an agent machine resolves its user")
		require.False(t, got["loose@example.test"].InInventory)

		counts := fleetCounts(t, ctx, store, orgID, deviceLevel, now, "")
		require.True(t, counts.MdmConnected)
		require.EqualValues(t, 5, counts.Total)
		require.EqualValues(t, 2, counts.AgentOnly)
		require.EqualValues(t, 1, counts.Missing)

		// A provider filter is that inventory and nothing else.
		scoped := listFleet(t, ctx, store, orgID, deviceLevel, now, fleetFilter{provider: cfg.Config.Provider})
		require.Len(t, scoped, 3)
		scopedCounts := fleetCounts(t, ctx, store, orgID, deviceLevel, now, cfg.Config.Provider)
		require.EqualValues(t, 3, scopedCounts.Total)
		require.Zero(t, scopedCounts.AgentOnly)

		agentOnly := false
		outside := listFleet(t, ctx, store, orgID, deviceLevel, now, fleetFilter{inInventory: &agentOnly})
		require.Len(t, outside, 2)
	}

	// Another provider's name matches no row and reports no connection.
	other := fleetCounts(t, ctx, store, orgID, false, now, "intune")
	require.False(t, other.MdmConnected)
	require.Zero(t, other.Total)
}

// TestFleetSerialRowIDHoldsStill guards the device link: an agent machine's id
// must not change when its second source arrives.
func TestFleetSerialRowIDHoldsStill(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	now := time.Now().UTC()

	seedAgentSync(t, ctx, conn, orgID, "late@example.test", now)
	seedStateReport(t, ctx, conn, orgID, stateReport{email: "late@example.test", serial: "S-LATE", at: now})
	before := listFleet(t, ctx, store, orgID, true, now, fleetFilter{})
	require.Len(t, before, 1)

	seedDeviceAgentSync(t, ctx, conn, orgID, "S-LATE", "late@example.test", now)
	after := listFleet(t, ctx, store, orgID, true, now, fleetFilter{})
	require.Len(t, after, 1)
	require.Equal(t, before[0].RowID, after[0].RowID)
}

// TestFleetCountsAgreeWithList holds the aggregate and the list to the same
// classification over a mixed fleet, and pages it across both row kinds.
func TestFleetCountsAgreeWithList(t *testing.T) {
	t.Parallel()

	ctx, conn, store, orgID := newStoreTestDB(t)
	cfg := mustUpsert(t, ctx, conn, store, orgID, validCreds(), validSettings(), true)
	now := time.Now().UTC()

	activeEmail := "mix-active@example.test"
	activeUser := seedUser(t, ctx, conn, activeEmail)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "mix-active", activeEmail, &activeUser, "M-ACTIVE", false)
	seedAgentSync(t, ctx, conn, orgID, activeEmail, now)
	seedDeviceAgentSync(t, ctx, conn, orgID, "M-ACTIVE", activeEmail, now)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "mix-other", activeEmail, &activeUser, "M-OTHER", false)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "mix-noemail", "", nil, "M-NOEMAIL", false)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "mix-stranger", "who@example.test", nil, "M-STRANGER", false)
	noAgentEmail := "mix-none@example.test"
	noAgentUser := seedUser(t, ctx, conn, noAgentEmail)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "mix-none", noAgentEmail, &noAgentUser, "M-NONE", false)
	seedDeviceWithSerial(t, ctx, conn, cfg.Config.ID, orgID, "mix-missing", "", nil, "M-MISSING", true)

	seedServedEtag(t, ctx, conn, orgID, activeEmail, "served")
	seedDeviceAgentSync(t, ctx, conn, orgID, "A-ONE", activeEmail, now)
	seedStateReport(t, ctx, conn, orgID, stateReport{email: activeEmail, serial: "A-ONE", at: now, etag: "served"})
	seedAgentSync(t, ctx, conn, orgID, "mix-loose@example.test", now)
	seedStateReport(t, ctx, conn, orgID, stateReport{email: "mix-loose@example.test", at: now, etag: "older"})
	seedAgentSync(t, ctx, conn, orgID, "mix-old@example.test", now.Add(-25*time.Hour))

	for _, deviceLevel := range []bool{false, true} {
		counts := fleetCounts(t, ctx, store, orgID, deviceLevel, now, "")
		rows := listFleet(t, ctx, store, orgID, deviceLevel, now, fleetFilter{})

		listed := map[string]int64{}
		for _, row := range rows {
			listed[row.CoverageBucket]++
			if row.PolicyState != "" {
				listed["policy_"+row.PolicyState]++
			}
			if !row.InInventory {
				listed["agent_only"]++
			}
		}
		aggregate := map[string]int64{
			"agent_active":       counts.AgentActive,
			"agent_stale":        counts.AgentStale,
			"agent_other_device": counts.AgentOtherDevice,
			"no_agent":           counts.NoAgent,
			"no_email":           counts.NoEmail,
			"unresolved_email":   counts.UnresolvedEmail,
			"missing":            counts.Missing,
			"policy_attested":    counts.PolicyAttested,
			"policy_outdated":    counts.PolicyOutdated,
			"policy_unreported":  counts.PolicyUnreported,
			"policy_halted":      counts.EnforcementHalted,
			"agent_only":         counts.AgentOnly,
		}
		for bucket, want := range aggregate {
			require.Equal(t, want, listed[bucket], "device_level=%v: counts and list disagree on %s", deviceLevel, bucket)
		}
		require.EqualValues(t, len(rows), counts.Total)
		require.EqualValues(t, 3, counts.AgentOnly, "A-ONE, mix-loose and mix-old")

		// Both kinds page under one cursor, newest first, with no row twice.
		seen := map[uuid.UUID]bool{}
		cursor := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
		for {
			page := listFleet(t, ctx, store, orgID, deviceLevel, now, fleetFilter{cursor: cursor, limit: 2})
			for _, row := range page {
				require.False(t, seen[row.RowID], "row %s listed twice", row.RowID)
				seen[row.RowID] = true
			}
			if len(page) < 2 {
				break
			}
			cursor = uuid.NullUUID{UUID: page[len(page)-1].RowID, Valid: true}
		}
		require.Len(t, seen, len(rows))
	}
}

func TestParseFleetDeviceID(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	for _, kind := range []string{"mdm", "agent"} {
		parsed, err := parseFleetDeviceID(kind + ":" + id.String())
		require.NoError(t, err)
		require.Equal(t, id, parsed)
	}
	for _, bad := range []string{id.String(), "device:" + id.String(), "agent:nope", ""} {
		_, err := parseFleetDeviceID(bad)
		require.ErrorIs(t, err, ErrInvalidCursor, bad)
	}
}

// TestFleetQueriesShareTheirClassification reads queries.sql itself: the
// fleet CTE is pasted into both fleet queries, and its bucket and policy
// CASEs are pasted from GetCoverageCounts. sqlc has no way to share them, so
// this is what stops a fix to one copy from missing the others.
func TestFleetQueriesShareTheirClassification(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("queries.sql")
	require.NoError(t, err)
	queries := splitQueries(string(raw))

	list, counts, coverage := queries["ListFleetDevices"], queries["GetFleetCoverageCounts"], queries["GetCoverageCounts"]
	require.NotEmpty(t, list)
	require.NotEmpty(t, counts)
	require.NotEmpty(t, coverage)

	listFleetCTE := between(t, list, "-- fleet:begin", "-- fleet:end")
	require.Equal(t, listFleetCTE, between(t, counts, "-- fleet:begin", "-- fleet:end"), "the fleet CTE differs between ListFleetDevices and GetFleetCoverageCounts")

	normalizedCoverage := normalizeSQL(coverage)
	bucket := normalizeSQL(between(t, listFleetCTE, "-- bucket:begin", "-- bucket:end"))
	require.Contains(t, normalizedCoverage, bucket, "the fleet's bucket CASE differs from GetCoverageCounts")

	policies := strings.Split(listFleetCTE, "-- policy:begin")[1:]
	require.Len(t, policies, 3, "one policy CASE per fleet arm")
	for _, policy := range policies {
		body, _, ok := strings.Cut(policy, "-- policy:end")
		require.True(t, ok)
		require.Contains(t, normalizedCoverage, normalizeSQL(body), "a fleet arm's policy CASE differs from GetCoverageCounts")
	}
}

var queryName = regexp.MustCompile(`(?m)^-- name: (\w+) `)

func splitQueries(sql string) map[string]string {
	out := map[string]string{}
	matches := queryName.FindAllStringSubmatchIndex(sql, -1)
	for i, m := range matches {
		end := len(sql)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		out[sql[m[2]:m[3]]] = sql[m[1]:end]
	}
	return out
}

func between(t *testing.T, s, begin, end string) string {
	t.Helper()
	_, rest, ok := strings.Cut(s, begin)
	require.True(t, ok, "missing %q", begin)
	body, _, ok := strings.Cut(rest, end)
	require.True(t, ok, "missing %q", end)
	return body
}

func normalizeSQL(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
