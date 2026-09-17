package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const constraintsOrgID = "org_constraints"

func logsCheck() Check {
	return Check{Scope: ScopeLogsRead, ResourceKind: "", ResourceID: constraintsOrgID, Dimensions: nil, selectorMatch: selectorMatchNormal}
}

func TestScopeConstraintsFor_noGrantsIsNotGranted(t *testing.T) {
	t.Parallel()

	constraints, err := ScopeConstraintsFor(nil, logsCheck(), ActorSelectorKeys)
	require.NoError(t, err)
	require.False(t, constraints.Granted)
	require.False(t, constraints.Unrestricted)
	require.Empty(t, constraints.Narrowed)
}

func TestScopeConstraintsFor_wildcardGrantIsUnrestricted(t *testing.T) {
	t.Parallel()

	grants := []Grant{NewGrant(ScopeLogsRead, WildcardResource)}

	constraints, err := ScopeConstraintsFor(grants, logsCheck(), ActorSelectorKeys)
	require.NoError(t, err)
	require.True(t, constraints.Granted)
	require.True(t, constraints.Unrestricted)
	require.Empty(t, constraints.Narrowed)
}

func TestScopeConstraintsFor_rootGrantIsUnrestricted(t *testing.T) {
	t.Parallel()

	grants := []Grant{NewGrant(ScopeRoot, WildcardResource)}

	constraints, err := ScopeConstraintsFor(grants, logsCheck(), ActorSelectorKeys)
	require.NoError(t, err)
	require.True(t, constraints.Granted)
	require.True(t, constraints.Unrestricted)
}

func TestScopeConstraintsFor_departmentGrantNarrows(t *testing.T) {
	t.Parallel()

	grants := []Grant{NewGrantWithSelector(ScopeLogsRead, Selector{
		SelectorKeyResourceKind:    ResourceKindLogs,
		SelectorKeyResourceID:      constraintsOrgID,
		SelectorKeyActorDepartment: "Engineering",
	})}

	constraints, err := ScopeConstraintsFor(grants, logsCheck(), ActorSelectorKeys)
	require.NoError(t, err)
	require.True(t, constraints.Granted)
	require.False(t, constraints.Unrestricted)
	require.Equal(t, []Selector{{SelectorKeyActorDepartment: "Engineering"}}, constraints.Narrowed)
}

func TestScopeConstraintsFor_narrowingGrantsUnion(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		NewGrantWithSelector(ScopeLogsRead, Selector{
			SelectorKeyResourceKind:    ResourceKindLogs,
			SelectorKeyResourceID:      constraintsOrgID,
			SelectorKeyActorDepartment: "Engineering",
		}),
		NewGrantWithSelector(ScopeLogsRead, Selector{
			SelectorKeyResourceKind: ResourceKindLogs,
			SelectorKeyResourceID:   WildcardResource,
			SelectorKeyActorGroup:   "platform-leads",
		}),
	}

	constraints, err := ScopeConstraintsFor(grants, logsCheck(), ActorSelectorKeys)
	require.NoError(t, err)
	require.True(t, constraints.Granted)
	require.False(t, constraints.Unrestricted)
	require.Equal(t, []Selector{
		{SelectorKeyActorDepartment: "Engineering"},
		{SelectorKeyActorGroup: "platform-leads"},
	}, constraints.Narrowed)
}

func TestScopeConstraintsFor_multiDimensionGrantKeptTogether(t *testing.T) {
	t.Parallel()

	grants := []Grant{NewGrantWithSelector(ScopeLogsRead, Selector{
		SelectorKeyResourceKind:    ResourceKindLogs,
		SelectorKeyResourceID:      constraintsOrgID,
		SelectorKeyActorDepartment: "Engineering",
		SelectorKeyActorRole:       "member",
	})}

	constraints, err := ScopeConstraintsFor(grants, logsCheck(), ActorSelectorKeys)
	require.NoError(t, err)
	require.Len(t, constraints.Narrowed, 1)
	require.Equal(t, Selector{
		SelectorKeyActorDepartment: "Engineering",
		SelectorKeyActorRole:       "member",
	}, constraints.Narrowed[0])
}

func TestScopeConstraintsFor_wildcardDimensionIsUnrestricted(t *testing.T) {
	t.Parallel()

	grants := []Grant{NewGrantWithSelector(ScopeLogsRead, Selector{
		SelectorKeyResourceKind:    ResourceKindLogs,
		SelectorKeyResourceID:      constraintsOrgID,
		SelectorKeyActorDepartment: WildcardResource,
	})}

	constraints, err := ScopeConstraintsFor(grants, logsCheck(), ActorSelectorKeys)
	require.NoError(t, err)
	require.True(t, constraints.Unrestricted)
	require.Empty(t, constraints.Narrowed)
}

func TestScopeConstraintsFor_anyUnrestrictedGrantWins(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		NewGrantWithSelector(ScopeLogsRead, Selector{
			SelectorKeyResourceKind:    ResourceKindLogs,
			SelectorKeyResourceID:      constraintsOrgID,
			SelectorKeyActorDepartment: "Engineering",
		}),
		NewGrant(ScopeLogsRead, WildcardResource),
	}

	constraints, err := ScopeConstraintsFor(grants, logsCheck(), ActorSelectorKeys)
	require.NoError(t, err)
	require.True(t, constraints.Unrestricted)
	require.Empty(t, constraints.Narrowed)
}

func TestScopeConstraintsFor_otherOrgGrantIgnored(t *testing.T) {
	t.Parallel()

	grants := []Grant{NewGrantWithSelector(ScopeLogsRead, Selector{
		SelectorKeyResourceKind:    ResourceKindLogs,
		SelectorKeyResourceID:      "org_other",
		SelectorKeyActorDepartment: "Engineering",
	})}

	constraints, err := ScopeConstraintsFor(grants, logsCheck(), ActorSelectorKeys)
	require.NoError(t, err)
	require.False(t, constraints.Granted)
}

func TestScopeConstraintsFor_unrelatedScopeIgnored(t *testing.T) {
	t.Parallel()

	grants := []Grant{NewGrant(ScopeProjectRead, WildcardResource)}

	constraints, err := ScopeConstraintsFor(grants, logsCheck(), ActorSelectorKeys)
	require.NoError(t, err)
	require.False(t, constraints.Granted)
}

func TestScopeConstraintsFor_narrowedGrantStillSatisfiesRequire(t *testing.T) {
	t.Parallel()

	grants := []Grant{NewGrantWithSelector(ScopeLogsRead, Selector{
		SelectorKeyResourceKind:    ResourceKindLogs,
		SelectorKeyResourceID:      constraintsOrgID,
		SelectorKeyActorDepartment: "Engineering",
	})}

	// A dimensionless check matches a narrowed grant, which is what lets a
	// handler authorize the request and then scope the rows it returns.
	require.True(t, GrantsSatisfy(grants, logsCheck()))

	// Strict matching is how a handler that cannot scope its rows refuses a
	// narrowed grant instead of over-serving it.
	require.False(t, GrantsSatisfy(grants, logsCheck().WithStrictSelectorMatch()))
	require.True(t, GrantsSatisfy([]Grant{NewGrant(ScopeLogsRead, WildcardResource)}, logsCheck().WithStrictSelectorMatch()))
}

func TestScopeConstraints_multiplePoliciesCollapseWhenBothNarrow(t *testing.T) {
	t.Parallel()

	department := []Grant{NewGrantWithSelector(ScopeLogsRead, Selector{
		SelectorKeyResourceKind:    ResourceKindLogs,
		SelectorKeyResourceID:      WildcardResource,
		SelectorKeyActorDepartment: "Engineering",
	})}
	group := []Grant{NewGrantWithSelector(ScopeLogsRead, Selector{
		SelectorKeyResourceKind: ResourceKindLogs,
		SelectorKeyResourceID:   WildcardResource,
		SelectorKeyActorGroup:   "platform-leads",
	})}

	authorization := grantAuthorization{policies: [][]Grant{department, group}}
	constraints, err := authorization.constraints(logsCheck(), ActorSelectorKeys)
	require.NoError(t, err)
	require.True(t, constraints.Granted)
	require.False(t, constraints.Unrestricted)
	require.Empty(t, constraints.Narrowed, "intersecting two narrowed policies under-approximates rather than widening")
}

func TestScopeConstraints_unrestrictedPolicyKeepsNarrowedPolicy(t *testing.T) {
	t.Parallel()

	narrowed := []Grant{NewGrantWithSelector(ScopeLogsRead, Selector{
		SelectorKeyResourceKind:    ResourceKindLogs,
		SelectorKeyResourceID:      WildcardResource,
		SelectorKeyActorDepartment: "Engineering",
	})}
	unrestricted := []Grant{NewGrant(ScopeLogsRead, WildcardResource)}

	authorization := grantAuthorization{policies: [][]Grant{unrestricted, narrowed}}
	constraints, err := authorization.constraints(logsCheck(), ActorSelectorKeys)
	require.NoError(t, err)
	require.True(t, constraints.Granted)
	require.False(t, constraints.Unrestricted)
	require.Equal(t, []Selector{{SelectorKeyActorDepartment: "Engineering"}}, constraints.Narrowed)
}

func TestScopeConstraints_policyWithoutGrantDenies(t *testing.T) {
	t.Parallel()

	unrestricted := []Grant{NewGrant(ScopeLogsRead, WildcardResource)}

	authorization := grantAuthorization{policies: [][]Grant{unrestricted, nil}}
	constraints, err := authorization.constraints(logsCheck(), ActorSelectorKeys)
	require.NoError(t, err)
	require.False(t, constraints.Granted)
}

func TestValidateSelector_logsActorDimensions(t *testing.T) {
	t.Parallel()

	base := Selector{SelectorKeyResourceKind: ResourceKindLogs, SelectorKeyResourceID: constraintsOrgID}
	require.NoError(t, ValidateSelector(ScopeLogsRead, base))

	for _, key := range ActorSelectorKeys {
		selector := Selector{
			SelectorKeyResourceKind: ResourceKindLogs,
			SelectorKeyResourceID:   constraintsOrgID,
			key:                     "value",
		}
		require.NoError(t, ValidateSelector(ScopeLogsRead, selector), key)
	}

	withTool := Selector{
		SelectorKeyResourceKind: ResourceKindLogs,
		SelectorKeyResourceID:   constraintsOrgID,
		SelectorKeyTool:         "search_docs",
	}
	require.Error(t, ValidateSelector(ScopeLogsRead, withTool))

	withActorDimensionOnOtherFamily := Selector{
		SelectorKeyResourceKind:    ResourceKindProject,
		SelectorKeyResourceID:      "proj_123",
		SelectorKeyActorDepartment: "Engineering",
	}
	require.Error(t, ValidateSelector(ScopeProjectRead, withActorDimensionOnOtherFamily))
}
