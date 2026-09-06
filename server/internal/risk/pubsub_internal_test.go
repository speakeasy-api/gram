package risk

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
)

func TestFilterPresidioFindingsAppliesEntityBlocklist(t *testing.T) {
	t.Parallel()

	person := scanners.Finding{RuleID: "pii.person", Confidence: 0.9, Source: ra.SourcePresidio}
	license := scanners.Finding{RuleID: "pii.us_driver_license", Confidence: 0.9, Source: ra.SourcePresidio}
	email := scanners.Finding{RuleID: "pii.email_address", Confidence: 0.9, Source: ra.SourcePresidio}
	findings := []scanners.Finding{person, license, email}

	// A pin containing PERSON is trimmed like the local scanner's pinned list.
	pinned := filterPresidioFindings(findings, repo.RiskPolicy{PresidioEntities: []string{"PERSON", "EMAIL_ADDRESS"}})
	require.Len(t, pinned, 1)
	require.Equal(t, "pii.email_address", pinned[0].RuleID)

	// A pin that trims to nothing scans nothing.
	require.Empty(t, filterPresidioFindings(findings, repo.RiskPolicy{PresidioEntities: []string{"PERSON"}}))

	// Unpinned: PERSON is allowed, US_DRIVER_LICENSE never surfaces.
	unpinned := filterPresidioFindings(findings, repo.RiskPolicy{})
	require.Len(t, unpinned, 2)
	require.Equal(t, "pii.person", unpinned[0].RuleID)
	require.Equal(t, "pii.email_address", unpinned[1].RuleID)
}

func TestEnforcementFindingsScoreAndDescription(t *testing.T) {
	t.Parallel()

	_, err := enforcementFindings("secret", ra.SourceGitleaks, []*riskv1.EnforcementFinding{
		riskv1.EnforcementFinding_builder{RuleId: new("secret.x"), Score: new(math.NaN()), StartPos: new(int32(0)), EndPos: new(int32(6))}.Build(),
	})
	require.ErrorContains(t, err, "invalid score")

	converted, err := enforcementFindings("secret", ra.SourceGitleaks, []*riskv1.EnforcementFinding{
		riskv1.EnforcementFinding_builder{RuleId: new(gitleaks.AccessKeyIDRuleID), Score: new(0.9), StartPos: new(int32(0)), EndPos: new(int32(6))}.Build(),
	})
	require.NoError(t, err)
	require.Len(t, converted, 1)
	wantDescription, ok := gitleaks.DescribeRule(gitleaks.AccessKeyIDRuleID)
	require.True(t, ok)
	require.Equal(t, wantDescription, converted[0].Description)
	require.NotEqual(t, gitleaks.AccessKeyIDRuleID, converted[0].Description)
}
