package risk_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

func TestRiskPolicyTargets_CreateWithNoTargetsStoresNoRows(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	q := riskrepo.New(ti.conn)

	policy, err := q.CreateRiskPolicyWithTargets(ctx, riskrepo.CreateRiskPolicyWithTargetsParams{
		Policy: newRepoRiskPolicyParams(t, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "Everyone Policy"),
	})
	require.NoError(t, err)

	targets, err := q.ListRiskPolicyTargetsByPolicy(ctx, policy.ID)
	require.NoError(t, err)
	require.Empty(t, targets)
}

func TestRiskPolicyTargets_CreateWithTargetsStoresRows(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	q := riskrepo.New(ti.conn)
	roleID := uuid.NewString()

	policy, err := q.CreateRiskPolicyWithTargets(ctx, riskrepo.CreateRiskPolicyWithTargetsParams{
		Policy: newRepoRiskPolicyParams(t, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "Targeted Policy"),
		Targets: []riskrepo.RiskPolicyTargetInput{
			{TargetType: "user", TargetID: authCtx.UserID},
			{TargetType: "role", TargetID: roleID},
		},
	})
	require.NoError(t, err)

	targets, err := q.ListRiskPolicyTargetsByPolicy(ctx, policy.ID)
	require.NoError(t, err)
	require.Len(t, targets, 2)
	require.ElementsMatch(t, []string{
		"user:" + authCtx.UserID,
		"role:" + roleID,
	}, targetKeys(targets))
	for _, target := range targets {
		require.Equal(t, policy.ID, target.RiskPolicyID)
		require.Equal(t, authCtx.ActiveOrganizationID, target.OrganizationID)
	}
}

func TestRiskPolicyTargets_UpdateWithTargetsReplacesRows(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	q := riskrepo.New(ti.conn)
	initialRoleID := uuid.NewString()
	replacementRoleID := uuid.NewString()

	policy, err := q.CreateRiskPolicyWithTargets(ctx, riskrepo.CreateRiskPolicyWithTargetsParams{
		Policy: newRepoRiskPolicyParams(t, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "Original Targeted Policy"),
		Targets: []riskrepo.RiskPolicyTargetInput{
			{TargetType: "role", TargetID: initialRoleID},
		},
	})
	require.NoError(t, err)

	updated, err := q.UpdateRiskPolicyWithTargets(ctx, riskrepo.UpdateRiskPolicyWithTargetsParams{
		Policy: riskrepo.UpdateRiskPolicyParams{
			ID:               policy.ID,
			ProjectID:        policy.ProjectID,
			Name:             "Updated Targeted Policy",
			Sources:          policy.Sources,
			PresidioEntities: policy.PresidioEntities,
			Enabled:          policy.Enabled,
			Action:           policy.Action,
			AutoName:         policy.AutoName,
		},
		Targets: []riskrepo.RiskPolicyTargetInput{
			{TargetType: "user", TargetID: authCtx.UserID},
			{TargetType: "role", TargetID: replacementRoleID},
		},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), updated.Version, "name and target changes should not bump policy version")

	targets, err := q.ListRiskPolicyTargetsByPolicy(ctx, policy.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{
		"user:" + authCtx.UserID,
		"role:" + replacementRoleID,
	}, targetKeys(targets))
	require.NotContains(t, targetKeys(targets), "role:"+initialRoleID)
}

func TestRiskPolicyTargets_ReplaceOnlyDoesNotBumpVersion(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	q := riskrepo.New(ti.conn)

	policy, err := q.CreateRiskPolicyWithTargets(ctx, riskrepo.CreateRiskPolicyWithTargetsParams{
		Policy: newRepoRiskPolicyParams(t, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "Target Only Policy"),
	})
	require.NoError(t, err)

	err = q.ReplaceRiskPolicyTargets(ctx, riskrepo.ReplaceRiskPolicyTargetsParams{
		RiskPolicyID:   policy.ID,
		OrganizationID: policy.OrganizationID,
		Targets: []riskrepo.RiskPolicyTargetInput{
			{TargetType: "user", TargetID: authCtx.UserID},
		},
	})
	require.NoError(t, err)

	after, err := q.GetRiskPolicy(ctx, riskrepo.GetRiskPolicyParams{
		ID:        policy.ID,
		ProjectID: policy.ProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, policy.Version, after.Version)
}

func newRepoRiskPolicyParams(t *testing.T, projectID uuid.UUID, organizationID, name string) riskrepo.CreateRiskPolicyParams {
	t.Helper()

	id, err := uuid.NewV7()
	require.NoError(t, err)

	return riskrepo.CreateRiskPolicyParams{
		ID:               id,
		ProjectID:        projectID,
		OrganizationID:   organizationID,
		Name:             name,
		Sources:          []string{"gitleaks"},
		PresidioEntities: nil,
		Enabled:          true,
		Action:           "flag",
		AutoName:         false,
	}
}

func targetKeys(targets []riskrepo.RiskPolicyTarget) []string {
	keys := make([]string, 0, len(targets))
	for _, target := range targets {
		keys = append(keys, fmt.Sprintf("%s:%s", target.TargetType, target.TargetID))
	}
	return keys
}
