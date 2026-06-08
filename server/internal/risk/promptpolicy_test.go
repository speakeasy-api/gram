package risk_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestPromptPolicies_FixturesInList(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	result, err := ti.service.ListRiskPolicies(ctx, &gen.ListRiskPoliciesPayload{})
	require.NoError(t, err)

	promptPolicies := promptOnlyPolicies(result.Policies)
	require.Len(t, promptPolicies, 2)
	require.Equal(t, "prompt", promptPolicies[0].Kind)
	require.NotEmpty(t, promptPolicies[0].PromptInstruction)
}

func TestPromptPolicy_CRUD(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	name := "Prompt Guard"
	autoName := false
	created, err := ti.service.CreatePromptPolicy(ctx, &gen.CreatePromptPolicyPayload{
		Name:              &name,
		PromptInstruction: "Tool calls that delete production records.",
		MessageTypes:      []string{"tool_request"},
		Action:            "block",
		AutoName:          &autoName,
	})
	require.NoError(t, err)
	require.Equal(t, "prompt", created.Kind)
	require.Equal(t, name, created.Name)
	require.Equal(t, "block", created.Action)
	require.Equal(t, []string{"tool_request"}, created.MessageTypes)

	got, err := ti.service.GetRiskPolicy(ctx, &gen.GetRiskPolicyPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "prompt", got.Kind)

	enabled := false
	updated, err := ti.service.UpdatePromptPolicy(ctx, &gen.UpdatePromptPolicyPayload{
		ID:      created.ID,
		Enabled: &enabled,
	})
	require.NoError(t, err)
	require.Equal(t, name, updated.Name)
	require.False(t, updated.Enabled)
	require.Equal(t, int64(2), updated.Version)

	err = ti.service.DeleteRiskPolicy(ctx, &gen.DeleteRiskPolicyPayload{ID: created.ID})
	require.NoError(t, err)

	result, err := ti.service.ListRiskPolicies(ctx, &gen.ListRiskPoliciesPayload{})
	require.NoError(t, err)
	for _, policy := range promptOnlyPolicies(result.Policies) {
		require.NotEqual(t, created.ID, policy.ID)
	}
}

func TestPromptPolicy_ToolResponsesRejected(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	_, err := ti.service.CreatePromptPolicy(ctx, &gen.CreatePromptPolicyPayload{
		PromptInstruction: "Tool calls that delete production records.",
		MessageTypes:      []string{"tool_response"},
		Action:            "block",
	})
	require.Error(t, err)
}
