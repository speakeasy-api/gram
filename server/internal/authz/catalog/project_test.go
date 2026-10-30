package catalog

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/stretchr/testify/require"
)

// TestProject_definitionKeepsTodaysRules runs project grant rows through both
// entry points. Project permissions keep the engine's current behaviour: write
// lets a caller view, a grant may cover the whole organization, and a direct
// user grant outranks a block inherited from a role.
func TestProject_definitionKeepsTodaysRules(t *testing.T) {
	t.Parallel()
	client := newClientTestClient(t)

	project := ProjectTarget(clientOrg, clientProject)
	other := ProjectTarget(clientOrg, "project-2")
	writer := []authz.Grant{heldBy(clientUser, authz.NewGrant(authz.ScopeProjectWrite, clientProject))}
	orgWide := []authz.Grant{heldBy(clientRole, authz.NewGrant(authz.ScopeProjectRead, authz.WildcardResource))}
	directOverBlock := []authz.Grant{
		heldBy(clientUser, authz.NewGrant(authz.ScopeProjectWrite, clientProject)),
		heldBy(clientRole, authz.NewGrant(authz.ScopeProjectBlockedWrite, clientProject)),
	}
	roleUnderBlock := []authz.Grant{
		heldBy(clientRole, authz.NewGrant(authz.ScopeProjectWrite, clientProject)),
		heldBy(clientRole, authz.NewGrant(authz.ScopeProjectBlockedWrite, clientProject)),
	}

	for _, tc := range []struct {
		name    string
		grants  []authz.Grant
		request Request
		reason  Reason
	}{
		{"writer changes its project", writer, ProjectWrite(project), ReasonAllowed},
		{"writer views its project", writer, ProjectRead(project), ReasonAllowed},
		{"writer cannot change another project", writer, ProjectWrite(other), ReasonNoGrant},
		{"org-wide read covers every project", orgWide, ProjectRead(other), ReasonAllowed},
		{"org-wide read does not change", orgWide, ProjectWrite(project), ReasonNoGrant},
		{"direct user grant outranks a role block", directOverBlock, ProjectWrite(project), ReasonAllowed},
		{"role grant yields to a role block", roleUnderBlock, ProjectWrite(project), ReasonBlocked},
		{"another organization's project", writer, ProjectWrite(ProjectTarget("org_other", clientProject)), ReasonCrossTenant},
	} {
		ctx := authz.GrantsToContext(sessionCtx(t), tc.grants)
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
