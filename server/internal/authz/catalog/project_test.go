package catalog

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/stretchr/testify/require"
)

// These tests back the project scenarios in GUIDE.md, step 3. Every row is
// decided through both Authorize and Require, which must agree.

type projectCase struct {
	name    string
	request Request
	reason  Reason
}

func requireProjectCases(t *testing.T, grants []authz.Grant, cases []projectCase) {
	t.Helper()
	client := newClientTestClient(t)
	ctx := authz.GrantsToContext(sessionCtx(t), grants)
	for _, tc := range cases {
		decision, err := client.Authorize(ctx, tc.request)
		require.NoError(t, err, tc.name)
		require.Equal(t, tc.reason, decision.Reason, tc.name)
		require.Equal(t, BasisNone, decision.Basis, tc.name)

		_, requireErr := client.Require(ctx, tc.request)
		if decision.Allowed() {
			require.NoError(t, requireErr, tc.name)
		} else {
			requireForbidden(t, requireErr)
		}
	}
}

var (
	projectA = ProjectTarget(clientOrg, "proj-a")
	projectB = ProjectTarget(clientOrg, "proj-b")
	projectC = ProjectTarget(clientOrg, "proj-c")
)

// Scenario 1: role A may read every project in the organization.
func TestProjectScenario1_readEveryProject(t *testing.T) {
	t.Parallel()
	grants := []authz.Grant{heldBy(scenarioRoleA, authz.NewGrant(authz.ScopeProjectRead, authz.WildcardResource))}

	requireProjectCases(t, grants, []projectCase{
		{"read proj-a", ProjectRead(projectA), ReasonAllowed},
		{"read proj-b", ProjectRead(projectB), ReasonAllowed},
		{"write proj-a", ProjectWrite(projectA), ReasonNoGrant},
		{"read a project in another organization", ProjectRead(ProjectTarget("org_other", "proj-a")), ReasonCrossTenant},
	})
}

// Scenario 2: role A may write one project, which also lets it read it.
func TestProjectScenario2_writeOneProjectImpliesRead(t *testing.T) {
	t.Parallel()
	grants := []authz.Grant{heldBy(scenarioRoleA, authz.NewGrant(authz.ScopeProjectWrite, "proj-a"))}

	requireProjectCases(t, grants, []projectCase{
		{"write proj-a", ProjectWrite(projectA), ReasonAllowed},
		{"read proj-a", ProjectRead(projectA), ReasonAllowed},
		{"write proj-b", ProjectWrite(projectB), ReasonNoGrant},
		{"read proj-b", ProjectRead(projectB), ReasonNoGrant},
	})
}

// Scenario 3: role B blocks writing project C. A user in roles A and B writes
// every project except C, and can still read C. Unlike MCP servers, a grant
// made directly to the user outranks the role's block.
func TestProjectScenario3_blockOneProjectForUserInBothRoles(t *testing.T) {
	t.Parallel()
	roles := []authz.Grant{
		heldBy(scenarioRoleA, authz.NewGrant(authz.ScopeProjectWrite, authz.WildcardResource)),
		heldBy(scenarioRoleB, authz.NewGrant(authz.ScopeProjectBlockedWrite, "proj-c")),
	}
	requireProjectCases(t, roles, []projectCase{
		{"write proj-a", ProjectWrite(projectA), ReasonAllowed},
		{"write proj-c", ProjectWrite(projectC), ReasonBlocked},
		{"read proj-c", ProjectRead(projectC), ReasonAllowed},
	})

	withDirect := []authz.Grant{roles[0], roles[1], heldBy(clientUser, authz.NewGrant(authz.ScopeProjectWrite, "proj-c"))}
	requireProjectCases(t, withDirect, []projectCase{
		{"direct user grant outranks the role block", ProjectWrite(projectC), ReasonAllowed},
	})

	readBlocked := []authz.Grant{
		heldBy(scenarioRoleA, authz.NewGrant(authz.ScopeProjectWrite, authz.WildcardResource)),
		heldBy(scenarioRoleB, authz.NewGrant(authz.ScopeProjectBlockedRead, "proj-c")),
	}
	requireProjectCases(t, readBlocked, []projectCase{
		{"a read block withdraws read", ProjectRead(projectC), ReasonBlocked},
		{"a read block withdraws write too", ProjectWrite(projectC), ReasonBlocked},
		{"other projects are untouched", ProjectWrite(projectA), ReasonAllowed},
	})
}
