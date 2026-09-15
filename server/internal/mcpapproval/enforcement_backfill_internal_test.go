package mcpapproval

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// The backfill refuses a policy outside the caller's organization scope. The
// ids come from the auth context today; this keeps that an assumption
// someone checks rather than one every future caller silently inherits.
func TestPolicyBelongsToOrganization(t *testing.T) {
	t.Parallel()

	policy := riskrepo.RiskPolicy{ID: uuid.New(), OrganizationID: "org_a"}

	require.NoError(t, policyBelongsToOrganization(policy, "org_a"))

	err := policyBelongsToOrganization(policy, "org_b")
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not belong to the caller's organization scope")
}

// An approved row with no stored principals is an everyone-approval written
// before RecordDecision normalized the empty set — replaying it literally
// would grant nobody. A denial's empty set stays empty: it grants nothing by
// design.
func TestStandingDecisionPrincipals_NormalizesLegacyEmptyApprovals(t *testing.T) {
	t.Parallel()

	approved, err := standingDecisionPrincipals(repo.ListStandingServerDecisionsForProjectRow{
		TargetKey:            "https://mcp.example.com/legacy",
		TargetRaw:            "https://mcp.example.com/legacy",
		Decision:             decisionApproved,
		GrantedPrincipalUrns: []string{},
	})
	require.NoError(t, err)
	require.Equal(t, authz.AllUsersPrincipal(), approved[0])
	require.Len(t, approved, 1)

	denied, err := standingDecisionPrincipals(repo.ListStandingServerDecisionsForProjectRow{
		TargetKey:            "https://mcp.example.com/denied",
		TargetRaw:            "https://mcp.example.com/denied",
		Decision:             decisionDenied,
		GrantedPrincipalUrns: []string{},
	})
	require.NoError(t, err)
	require.Empty(t, denied)
}
