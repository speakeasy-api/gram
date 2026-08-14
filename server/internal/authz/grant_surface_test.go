package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateGrantSurface(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateGrantSurface(GrantSurfaceAccess, []*RoleGrant{
		{Scope: string(ScopeOrgAdmin)},
		{Scope: string(ScopeProjectRead)},
		{Scope: string(ScopeSkillWrite)},
		{Scope: string(ScopeSkillBlockedRead)},
		// Chat scopes are assignable only through custom roles, so the access
		// surface has to own them or non-owner session access could never be
		// granted at all.
		{Scope: string(ScopeChatRead)},
		{Scope: string(ScopeChatWrite)},
		{Scope: string(ScopeMCPApprovalRead)},
		{Scope: string(ScopeMCPApprovalBlockedRead)},
		{Scope: string(ScopeMCPApprovalDecide)},
		{Scope: string(ScopeMCPApprovalBlockedDecide)},
	}))
	require.NoError(t, ValidateGrantSurface(GrantSurfaceRiskPolicy, []*RoleGrant{
		{Scope: string(ScopeRiskPolicyEvaluate)},
		{Scope: string(ScopeRiskPolicyBypass)},
	}))

	require.ErrorContains(t, ValidateGrantSurface(GrantSurfaceAccess, []*RoleGrant{
		{Scope: string(ScopeRiskPolicyEvaluate)},
	}), `managed by "risk_policy" grants`)
	require.ErrorContains(t, ValidateGrantSurface(GrantSurfaceRiskPolicy, []*RoleGrant{
		{Scope: string(ScopeProjectRead)},
	}), `managed by "access" grants`)
}

func TestValidateGrantSurfaceRejectsUnknownScope(t *testing.T) {
	t.Parallel()

	err := ValidateGrantSurface(GrantSurfaceAccess, []*RoleGrant{{Scope: "unknown:scope"}})
	require.ErrorContains(t, err, `scope "unknown:scope" is not managed by a grant surface`)
}
