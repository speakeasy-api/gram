package admission

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/directory"
)

func TestEvaluatePermitsEmptyAudienceWithoutApproval(t *testing.T) {
	t.Parallel()

	verdict, err := Evaluate(nil, Decision{})
	require.NoError(t, err)
	require.Equal(t, StateEmptyAudience, verdict.State)
}

func TestEvaluateTreatsLegacyEmptyApprovedAudienceAsEveryone(t *testing.T) {
	t.Parallel()

	verdict, err := Evaluate([]string{"role:developers", "user:user_123"}, Decision{Decision: "approved", GrantedPrincipalURNs: nil})
	require.NoError(t, err)
	require.Equal(t, StateCovered, verdict.State)
}

func TestEvaluateNormalizesWildcardApprovalToEveryone(t *testing.T) {
	t.Parallel()

	verdict, err := Evaluate([]string{"role:developers"}, Decision{Decision: "approved", GrantedPrincipalURNs: []string{"*"}})
	require.NoError(t, err)
	require.Equal(t, StateCovered, verdict.State)
}

func TestEvaluateRequiresEveryExactAudiencePrincipal(t *testing.T) {
	t.Parallel()

	decision := Decision{Decision: "approved", GrantedPrincipalURNs: []string{"role:developers", "user:user_123"}}

	covered, err := Evaluate([]string{"user:user_123", "role:developers"}, decision)
	require.NoError(t, err)
	require.Equal(t, StateCovered, covered.State)

	narrow, err := Evaluate([]string{"role:developers", "role:operators"}, decision)
	require.NoError(t, err)
	require.Equal(t, StateApprovalRequired, narrow.State)
}

func TestEvaluateSupportsDirectoryPrincipals(t *testing.T) {
	t.Parallel()

	group := directory.GroupPrincipal(uuid.New())
	attribute := directory.AttributePrincipal("department", "engineering")
	verdict, err := Evaluate([]string{group, attribute}, Decision{Decision: "approved", GrantedPrincipalURNs: []string{group, attribute}})

	require.NoError(t, err)
	require.Equal(t, StateCovered, verdict.State)
}

func TestEvaluateReportsOnlyMissingPrincipalKinds(t *testing.T) {
	t.Parallel()

	verdict, err := Evaluate([]string{
		"*",
		"role:developers",
		directory.GroupPrincipal(uuid.New()),
		directory.AttributePrincipal("department", "engineering"),
		"user:user_123",
	}, Decision{Decision: "approved", GrantedPrincipalURNs: []string{"role:developers"}})

	require.NoError(t, err)
	require.Equal(t, StateApprovalRequired, verdict.State)
	require.Equal(t, MissingAudienceCounts{Everyone: 1, Roles: 0, Groups: 1, Attributes: 1, Users: 1}, verdict.MissingAudienceCounts)
}

func TestCountMissingAudienceDeduplicatesCanonicalAliases(t *testing.T) {
	t.Parallel()

	counts := CountMissingAudience([]string{"*", authz.AllUsersPrincipal().String(), "role:developers", "role:developers"})

	require.Equal(t, MissingAudienceCounts{Everyone: 1, Roles: 1, Groups: 0, Attributes: 0, Users: 0}, counts)
}

func TestEvaluateRequiresApprovalAfterDenial(t *testing.T) {
	t.Parallel()

	verdict, err := Evaluate([]string{"role:developers"}, Decision{Decision: "denied", GrantedPrincipalURNs: nil})
	require.NoError(t, err)
	require.Equal(t, StateApprovalRequired, verdict.State)
	require.Equal(t, MissingAudienceCounts{Roles: 1}, verdict.MissingAudienceCounts)
}

func TestEvaluateRejectsUnknownDecisionState(t *testing.T) {
	t.Parallel()

	_, err := Evaluate([]string{"role:developers"}, Decision{Decision: "pending", GrantedPrincipalURNs: nil})
	require.Error(t, err)
}

func TestEvaluateRejectsMalformedApprovedPrincipal(t *testing.T) {
	t.Parallel()

	_, err := Evaluate([]string{"role:developers"}, Decision{Decision: "approved", GrantedPrincipalURNs: []string{"*", "not-a-principal"}})
	require.Error(t, err)
}

func TestEvaluateRejectsMalformedDesiredPrincipal(t *testing.T) {
	t.Parallel()

	_, err := Evaluate([]string{"not-a-principal"}, Decision{Decision: "approved", GrantedPrincipalURNs: []string{"*"}})
	require.Error(t, err)
}
