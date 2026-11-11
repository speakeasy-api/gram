package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateGrantSource(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateGrantSource(GrantSourceRoles, []*RoleGrant{
		{Scope: string(ScopeOrgAdmin)},
		{Scope: string(ScopeProjectRead)},
		{Scope: string(ScopeSkillWrite)},
		{Scope: string(ScopeSkillBlockedRead)},
		// Chat scopes are assignable only through custom roles, so roles have
		// to be their source or non-owner session access could never be
		// granted at all.
		{Scope: string(ScopeChatRead)},
		{Scope: string(ScopeChatWrite)},
		{Scope: string(ScopeLogRead)},
		{Scope: string(ScopeAgentRead)},
		{Scope: string(ScopeAgentWrite)},
		{Scope: string(ScopeAgentAuthorize)},
		{Scope: string(ScopeAgentTransfer)},
		{Scope: string(ScopeAssistantRead)},
		{Scope: string(ScopeAssistantBlockedRead)},
		{Scope: string(ScopeAssistantWrite)},
		{Scope: string(ScopeAssistantBlockedWrite)},
	}))
	require.NoError(t, ValidateGrantSource(GrantSourceRiskPolicy, []*RoleGrant{
		{Scope: string(ScopeRiskPolicyEvaluate)},
		{Scope: string(ScopeRiskPolicyBypass)},
	}))

	require.ErrorContains(t, ValidateGrantSource(GrantSourceRoles, []*RoleGrant{
		{Scope: string(ScopeRiskPolicyEvaluate)},
	}), `managed by "risk_policy" grants`)
	require.ErrorContains(t, ValidateGrantSource(GrantSourceRiskPolicy, []*RoleGrant{
		{Scope: string(ScopeProjectRead)},
	}), `managed by "access" grants`)
}

func TestValidateGrantSourceRejectsUnknownScope(t *testing.T) {
	t.Parallel()

	err := ValidateGrantSource(GrantSourceRoles, []*RoleGrant{{Scope: "unknown:scope"}})
	require.ErrorContains(t, err, `scope "unknown:scope" is not managed by a grant surface`)
}
