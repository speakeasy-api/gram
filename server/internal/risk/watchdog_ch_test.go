package risk_test

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// Insert strings directly: the ordinary fixture writer binds time.Time at
// second precision and would hide this regression before the read executes.
func TestWatchdogClickHouseFractionalTimestamps(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	q := chrepo.New(ti.chConn)
	base := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Second)
	from, to := base.Add(100000001*time.Nanosecond), base.Add(900000009*time.Nanosecond)
	policy := uuid.NewString()
	events := []time.Time{from.Add(-time.Nanosecond), from, base.Add(500000005 * time.Nanosecond), to}
	ids := make([]uuid.UUID, len(events))
	for i, event := range events {
		ids[i] = uuid.New()
		require.NoError(t, ti.chConn.Exec(ctx, `INSERT INTO risk_findings
   (id, organization_id, project_id, risk_policy_id, created_at, message_created_at, category)
   VALUES (?, ?, ?, ?, toDateTime64(?, 9, 'UTC'), toDateTime64(?, 9, 'UTC'), 'test')`,
			ids[i], auth.ActiveOrganizationID, auth.ProjectID.String(), policy,
			to.Add(time.Hour).Format("2006-01-02 15:04:05.000000000"), event.Format("2006-01-02 15:04:05.000000000")))
	}
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)
	p := chrepo.ListRiskFindingsParams{OrganizationID: auth.ActiveOrganizationID, ProjectID: auth.ProjectID.String(), PolicyIDs: []string{policy}, From: &from, To: &to, Limit: 10}
	t.Run("nanosecond window bounds", func(t *testing.T) {
		t.Parallel()
		count, err := q.CountWatchdogFindings(ctx, p)
		require.NoError(t, err)
		require.Equal(t, uint64(2), count)
		groups, err := q.GroupWatchdogFindings(ctx, p, "data_type")
		require.NoError(t, err)
		require.Equal(t, []chrepo.WatchdogGroup{{Value: "test", Count: 2}}, groups)
		rows, err := q.ListWatchdogFindings(ctx, p)
		require.NoError(t, err)
		require.Len(t, rows, 2)
		require.Equal(t, ids[2], rows[0].ID)
		require.Equal(t, ids[1], rows[1].ID)
		require.Equal(t, from, rows[1].MessageCreatedAt.UTC())
	})
	t.Run("nanosecond cursor", func(t *testing.T) {
		t.Parallel()
		end := base.Add(time.Second)
		pageParams := p
		pageParams.From, pageParams.To, pageParams.Limit = &base, &end, 1
		for i, event := range slices.Backward(events) {
			rows, err := q.ListWatchdogFindings(ctx, pageParams)
			require.NoError(t, err)
			require.Len(t, rows, 1, "event index %d", i)
			require.Equal(t, ids[i], rows[0].ID)
			require.Equal(t, event, rows[0].MessageCreatedAt.UTC())
			pageParams.CursorTime = &rows[0].MessageCreatedAt
			pageParams.CursorID = uuid.NullUUID{UUID: rows[0].ID, Valid: true}
		}
		rows, err := q.ListWatchdogFindings(ctx, pageParams)
		require.NoError(t, err)
		require.Empty(t, rows)
	})
}

func TestWatchdogClickHouse(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	require.NotNil(t, ti.chConn)
	q := chrepo.New(ti.chConn)
	from := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Hour)
	to := from.Add(4 * time.Hour)
	policy := uuid.NewString()
	makeFinding := func(event time.Time) chrepo.RiskFindingRow {
		row := chListFinding(t, *auth.ProjectID, auth.ActiveOrganizationID, uuid.New(), uuid.New(), policy, to.Add(time.Hour), event, "gitleaks", "secret.test", "external-test", "redacted-test", "fingerprint-test", "")
		row.EventKind = chrepo.EventKindFinding
		row.Category = "secrets"
		row.Team = "engineering"
		row.ChatSource = "assistant"
		row.UserID = "internal-test"
		return row
	}
	oldest := makeFinding(from) // inclusive lower bound
	middle := makeFinding(from.Add(time.Hour))
	middle.ExternalUserID = "" // internal identity fallback
	newest := makeFinding(from.Add(2 * time.Hour))
	newest.Category, newest.Team, newest.ChatSource = "", "", ""
	newest.ExternalUserID, newest.UserID = "", "" // unknown remains empty
	tied := makeFinding(newest.MessageCreatedAt)
	// Fix IDs to explicitly test the event-time tie breaker across page boundaries.
	newest.ID = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	tied.ID = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	// IDs must remain tenant-unique because ClickHouse dedups within tenant scope.
	before := makeFinding(from.Add(-time.Second))
	upper := makeFinding(to) // exclusive upper bound, despite later created_at
	foreignOrg := makeFinding(from.Add(time.Hour))
	foreignOrg.OrganizationID = uuid.NewString()
	foreignProject := makeFinding(from.Add(time.Hour))
	foreignProject.ProjectID = uuid.NewString()
	otherPolicy := makeFinding(from.Add(time.Hour))
	otherPolicy.RiskPolicyID = uuid.NewString()
	deadLetter := makeFinding(from.Add(time.Hour))
	deadLetter.DeadLetterReason = "test sentinel"
	suppressed := makeFinding(from.Add(3 * time.Hour))
	suppression := suppressed
	suppression.EventKind = chrepo.EventKindSuppression
	suppression.ExcludedAt = &to
	falsePositive := makeFinding(from.Add(3 * time.Hour))
	fpCopy := falsePositive
	fpCopy.FalsePositiveAt = &to
	fpCopy.EventKind = chrepo.EventKindSuppression
	// Insert order makes original redeliveries newer than suppression copies.
	rows := []chrepo.RiskFindingRow{oldest, middle, newest, tied, tied, before, upper, foreignOrg, foreignProject, otherPolicy, deadLetter, suppressed, suppression, suppressed, falsePositive, fpCopy, falsePositive}
	require.NoError(t, q.InsertRiskFindings(ctx, rows))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)
	p := chrepo.ListRiskFindingsParams{OrganizationID: auth.ActiveOrganizationID, ProjectID: auth.ProjectID.String(), PolicyIDs: []string{policy}, From: &from, To: &to, Limit: 1}

	t.Run("pagination and safe metadata", func(t *testing.T) {
		t.Parallel()
		pageParams := p
		var got []chrepo.WatchdogFinding
		for range 5 {
			page, err := q.ListWatchdogFindings(ctx, pageParams)
			require.NoError(t, err)
			if len(page) == 0 {
				break
			}
			require.Len(t, page, 1)
			got = append(got, page[0])
			pageParams.CursorTime = &page[0].MessageCreatedAt
			pageParams.CursorID = uuid.NullUUID{UUID: page[0].ID, Valid: true}
		}
		require.Len(t, got, 4)
		require.Equal(t, []uuid.UUID{tied.ID, newest.ID, middle.ID, oldest.ID}, []uuid.UUID{got[0].ID, got[1].ID, got[2].ID, got[3].ID})
		require.Equal(t, "external-test", got[0].User)
		require.Empty(t, got[1].User)
		require.Equal(t, "internal-test", got[2].User)
		require.Equal(t, from, got[3].MessageCreatedAt.UTC())
		require.Equal(t, policy, got[0].PolicyID)
		require.Equal(t, "secret.test", got[0].RuleID)
		require.Equal(t, "assistant", got[0].App)
		// Even a cursor past the last live finding must not narrow counts/groups.
		count, err := q.CountWatchdogFindings(ctx, pageParams)
		require.NoError(t, err)
		require.Equal(t, uint64(4), count)
		for dimension, expected := range map[string][]chrepo.WatchdogGroup{
			"severity":  {{Value: policy, Count: 4}},
			"data_type": {{Value: "secrets", Count: 3}, {Value: "", Count: 1}},
			"team":      {{Value: "engineering", Count: 3}, {Value: "", Count: 1}},
			"app":       {{Value: "assistant", Count: 3}, {Value: "", Count: 1}},
			"user":      {{Value: "external-test", Count: 2}, {Value: "", Count: 1}, {Value: "internal-test", Count: 1}},
		} {
			groups, err := q.GroupWatchdogFindings(ctx, pageParams, dimension)
			require.NoError(t, err, dimension)
			require.Equal(t, expected, groups, dimension)
		}
	})

	t.Run("explicit policy scope", func(t *testing.T) {
		t.Parallel()
		scope := p
		scope.PolicyIDs = []string{otherPolicy.RiskPolicyID}
		page, err := q.ListWatchdogFindings(ctx, scope)
		require.NoError(t, err)
		require.Len(t, page, 1)
		require.Equal(t, otherPolicy.ID, page[0].ID)
		count, err := q.CountWatchdogFindings(ctx, scope)
		require.NoError(t, err)
		require.Equal(t, uint64(1), count)
		scope.PolicyIDs = []string{uuid.NewString()}
		count, err = q.CountWatchdogFindings(ctx, scope)
		require.NoError(t, err)
		require.Zero(t, count)
	})
}
