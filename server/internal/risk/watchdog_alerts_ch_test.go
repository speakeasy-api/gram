package risk_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestWatchdogAlertsClickHouse(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	q := chrepo.New(ti.chConn)
	base := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Second)
	from, to := base.Add(123456789*time.Nanosecond), base.Add(987654321*time.Nanosecond)
	p := chrepo.RiskSignalWindowParams{
		OrganizationID: auth.ActiveOrganizationID,
		ProjectID:      auth.ProjectID.String(),
		MCPServerID:    "",
		WideFrom:       base.Add(-time.Hour),
		From:           from,
		To:             to,
	}
	const layout = "2006-01-02 15:04:05.000000000"
	// Bind timestamp strings so ingestion cannot erase the boundary regression.
	insert := func(id uuid.UUID, org, project, rule, policy, external, user, app, sample string, detected, message time.Time, state string) {
		t.Helper()
		require.NoError(t, ti.chConn.Exec(ctx, `INSERT INTO risk_findings
   (id,organization_id,project_id,rule_id,risk_policy_id,external_user_id,user_id,chat_source,match_redacted,created_at,message_created_at,category,team,excluded_at,false_positive_at,dead_letter_reason)
   VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, toDateTime64(?,9,'UTC'), toDateTime64(?,9,'UTC'), 'pii', 'engineering',
   if(? = 'excluded', now64(9), NULL), if(? = 'false_positive', now64(9), NULL), ?)`,
			id, org, project, rule, policy, external, user, app, sample, detected.Format(layout), message.Format(layout), state, state,
			map[bool]string{true: "failed", false: ""}[state == "dead_letter"]))
	}
	add := func(id uuid.UUID, policy, external, user, app, sample string, detected, message time.Time, state string) {
		insert(id, p.OrganizationID, p.ProjectID, "rule-a", policy, external, user, app, sample, detected, message, state)
	}
	first, last := base.Add(-48*time.Hour), base.Add(-24*time.Hour)
	id := uuid.New()
	add(id, "policy-a", "external-a", "internal-a", "cursor", "", from, first, "")
	// Redelivery must not multiply finding counts.
	add(id, "policy-a", "external-a", "internal-a", "cursor", "", from, first, "")
	add(uuid.New(), "policy-b", "external-a", "different-internal", "codex", "older-display", from.Add(time.Nanosecond), first.Add(time.Hour), "")
	add(uuid.New(), "policy-a", "", "internal-b", "cursor", "latest-display", to.Add(-time.Nanosecond), last.Add(-time.Hour), "")
	add(uuid.New(), "", "", "", "", "", to.Add(-time.Nanosecond), last, "")
	// Detection time, not message time, defines membership.
	add(uuid.New(), "policy-a", "outside", "", "", "", from.Add(-time.Nanosecond), from, "")
	add(uuid.New(), "policy-a", "outside", "", "", "", to, from, "")
	for _, state := range []string{"excluded", "false_positive", "dead_letter"} {
		suppressed := uuid.New()
		add(suppressed, "policy-a", "suppressed", "", "", "must-not-sample", from, last.Add(time.Hour), "")
		add(suppressed, "policy-a", "suppressed", "", "", "must-not-sample", from, last.Add(time.Hour), state)
	}
	insert(uuid.New(), "foreign-org", p.ProjectID, "rule-a", "", "foreign", "", "", "", from, from, "")
	insert(uuid.New(), p.OrganizationID, uuid.NewString(), "rule-a", "", "foreign", "", "", "", from, from, "")
	insert(uuid.New(), p.OrganizationID, p.ProjectID, "rule-b", "", "", "", "", "", from, from, "")
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)
	alerts, err := q.ListWatchdogAlerts(ctx, p, chrepo.WatchdogAlertLimit)
	require.NoError(t, err)
	require.Len(t, alerts, 2)
	require.Equal(t, "rule-a", alerts[0].RuleID)
	require.Equal(t, "pii", alerts[0].Category)
	require.Equal(t, uint64(4), alerts[0].FindingCount)
	require.Equal(t, uint64(2), alerts[0].UsersAffected)
	require.Equal(t, []string{"policy-a", "policy-b"}, alerts[0].PolicyIDs)
	require.Equal(t, []string{"codex", "cursor"}, alerts[0].Clients)
	require.Equal(t, first, alerts[0].FirstSeen.UTC())
	require.Equal(t, last, alerts[0].LastSeen.UTC())
	require.Equal(t, "latest-display", alerts[0].SampleEvidence)
	require.Empty(t, alerts[1].SampleEvidence)
	groups, err := q.GroupWatchdogAlerts(ctx, p, []string{"rule-a", "rule-b"}, "user")
	require.NoError(t, err)
	require.Equal(t, []chrepo.WatchdogAlertGroup{{RuleID: "rule-a", Value: "external-a", Count: 2}, {RuleID: "rule-a", Value: "internal-b", Count: 1}}, groups)
	groups, err = q.GroupWatchdogAlerts(ctx, p, []string{"rule-a", "rule-b"}, "data_type")
	require.NoError(t, err)
	require.Equal(t, []chrepo.WatchdogAlertGroup{{RuleID: "rule-a", Value: "pii", Count: 4}, {RuleID: "rule-b", Value: "pii", Count: 1}}, groups)
}

func TestWatchdogAlertsClickHouseArrayOverflow(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	q := chrepo.New(ti.chConn)
	from := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	for _, dimension := range []string{"policy", "client"} {
		t.Run(dimension, func(t *testing.T) {
			t.Parallel()
			// Separate rule population/window per subtest, with 201 distinct values.
			window := from
			if dimension == "client" {
				window = window.Add(time.Minute)
			}
			require.NoError(t, ti.chConn.Exec(ctx, `INSERT INTO risk_findings
    (id,organization_id,project_id,rule_id,risk_policy_id,chat_source,created_at,message_created_at)
    SELECT generateUUIDv4(), ?, ?, 'overflow', if(? = 'policy', toString(number), ''), if(? = 'client', toString(number), ''), ?, ? FROM numbers(201)`,
				auth.ActiveOrganizationID, auth.ProjectID.String(), dimension, dimension, window, window))
			testenv.FlushClickHouseAsyncInserts(t, ti.chConn)
			rows, err := q.ListWatchdogAlerts(ctx, chrepo.RiskSignalWindowParams{
				OrganizationID: auth.ActiveOrganizationID,
				ProjectID:      auth.ProjectID.String(),
				MCPServerID:    "",
				WideFrom:       time.Time{},
				From:           window,
				To:             window.Add(time.Second),
			}, chrepo.WatchdogAlertLimit)
			require.ErrorContains(t, err, "distinct client or policy limit")
			require.Nil(t, rows)
		})
	}
}

func TestWatchdogAlertGroupsClickHousePerRuleLimit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	from := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	require.NoError(t, ti.chConn.Exec(ctx, `INSERT INTO risk_findings
  (id,organization_id,project_id,rule_id,team,created_at,message_created_at)
  SELECT generateUUIDv4(), ?, ?, concat('rule-', toString(intDiv(number, 210))), leftPad(toString(number % 210), 3, '0'), ?, ? FROM numbers(630)`,
		auth.ActiveOrganizationID, auth.ProjectID.String(), from, from))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)
	p := chrepo.RiskSignalWindowParams{
		OrganizationID: auth.ActiveOrganizationID,
		ProjectID:      auth.ProjectID.String(),
		MCPServerID:    "",
		WideFrom:       time.Time{},
		From:           from,
		To:             from.Add(time.Second),
	}
	groups, err := chrepo.New(ti.chConn).GroupWatchdogAlerts(ctx, p, []string{"rule-0", "rule-1"}, "team")
	require.NoError(t, err)
	require.Len(t, groups, 402)
	require.Equal(t, chrepo.WatchdogAlertGroup{RuleID: "rule-0", Value: "000", Count: 1}, groups[0])
	require.Equal(t, chrepo.WatchdogAlertGroup{RuleID: "rule-0", Value: "200", Count: 1}, groups[200])
	require.Equal(t, chrepo.WatchdogAlertGroup{RuleID: "rule-1", Value: "000", Count: 1}, groups[201])
	require.Equal(t, chrepo.WatchdogAlertGroup{RuleID: "rule-1", Value: "200", Count: 1}, groups[401])
}
