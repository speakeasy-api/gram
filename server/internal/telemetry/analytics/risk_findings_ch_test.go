package analytics

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	riskchrepo "github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	riskRepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

func riskFindingFixture(orgID, projectID, category, ruleID, policyID, email string, at time.Time) riskchrepo.RiskFindingRow {
	return riskchrepo.RiskFindingRow{
		ID:               uuid.Must(uuid.NewV7()),
		CreatedAt:        at,
		OrganizationID:   orgID,
		ProjectID:        projectID,
		RiskPolicyID:     policyID,
		RuleID:           ruleID,
		Source:           "gitleaks",
		Category:         category,
		ChatID:           uuid.NewString(),
		UserEmail:        email,
		ChatSource:       "claude-code",
		MessageCreatedAt: at,
		EventKind:        riskchrepo.EventKindFinding,
	}
}

// TestRiskFindingsCountsLiveFindingsOnce runs the dataset against ClickHouse:
// a redelivered finding counts once, and a dismissed, shadow, dead-letter or
// foreign finding not at all, as on the Watchdog page.
func TestRiskFindingsCountsLiveFindingsOnce(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := newTestClickhouse(t)

	orgID, projectID := "org_"+uuid.NewString(), uuid.NewString()
	at := time.Now().UTC().Add(-time.Hour)
	criticalPolicyID := uuid.New()
	criticalPolicy := criticalPolicyID.String()

	secret := riskFindingFixture(orgID, projectID, "secrets", "secret.github_pat", criticalPolicy, "alice@example.com", at)
	email := riskFindingFixture(orgID, projectID, "pii", "pii.email_address", "", "bob@example.com", at)

	dismissed := riskFindingFixture(orgID, projectID, "secrets", "secret.github_pat", criticalPolicy, "carol@example.com", at)
	dismissal := dismissed
	dismissedAt := at.Add(time.Minute)
	dismissal.ExcludedAt = &dismissedAt
	dismissal.ExcludedReason = "manual"
	dismissal.EventKind = riskchrepo.EventKindSuppression

	shadow := riskFindingFixture(orgID, projectID, "secrets", "secret.github_pat", criticalPolicy, "dave@example.com", at)
	shadow.Shadow = true
	deadLetter := riskFindingFixture(orgID, projectID, "", "", criticalPolicy, "erin@example.com", at)
	deadLetter.DeadLetterReason = "could-not-analyze"
	foreign := riskFindingFixture("org_"+uuid.NewString(), uuid.NewString(), "secrets", "secret.github_pat", criticalPolicy, "frank@example.com", at)

	require.NoError(t, riskchrepo.New(conn).InsertRiskFindings(ctx, []riskchrepo.RiskFindingRow{
		secret, secret, email, dismissed, dismissal, shadow, deadLetter, foreign,
	}))

	tenant := Tenant{OrganizationID: orgID, ProjectID: projectID}
	lookups := LookupMaps{RiskPolicySeveritiesLookup: severitiesByKey([]riskRepo.ListRiskPolicyAnalyticsLabelsRow{
		{ID: criticalPolicyID, Name: "Secrets", Score: 9.5, Deleted: false},
	})}
	query := func(t *testing.T, dimensions []string, measures []Measure) []Row {
		t.Helper()
		plan, err := Compile(Default, tenant, lookups, Request{
			Dataset:      "risk_findings",
			FromUnixNano: at.Add(-time.Hour).UnixNano(),
			ToUnixNano:   at.Add(time.Hour).UnixNano(),
			Grain:        TimeGrainNone,
			Dimensions:   dimensions,
			Measures:     measures,
			Filters:      nil,
			OrderBy:      nil,
			Limit:        0,
			Ungrouped:    false,
		})
		require.NoError(t, err)
		rows, err := plan.Run(ctx, conn)
		require.NoError(t, err)
		return rows
	}

	// InsertRiskFindings returns once the rows are committed, so they read
	// back straight away.
	rows := query(t, []string{"category"}, []Measure{{Op: AggregationCount, Field: "", Alias: "findings"}})
	require.ElementsMatch(t, []Row{
		{"category": "secrets", "findings": int64(1)},
		{"category": "pii", "findings": int64(1)},
	}, rows)

	rows = query(t, []string{"severity"}, []Measure{{Op: AggregationCount, Field: "", Alias: "findings"}})
	require.ElementsMatch(t, []Row{
		{"severity": "critical", "findings": int64(1)},
		{"severity": "medium", "findings": int64(1)},
	}, rows, "a scored policy sets the band; a finding without one takes its category's")

	rows = query(t, nil, []Measure{{Op: string(AggregationCountDistinct), Field: "user", Alias: "people"}})
	require.Equal(t, []Row{{"people": int64(2)}}, rows)
}
