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
